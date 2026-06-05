// Package blocklist manages ad-blocking domain lists.
package blocklist

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var DefaultSources = []string{
	// StevenBlack unified hosts (ads + malware)
	"https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
	// AdGuard DNS filter
	"https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt",
	// OISD basic
	"https://abp.oisd.nl/basic/",
}

var (
	// "0.0.0.0 ads.example.com" or "127.0.0.1 ads.example.com"
	hostsRe = regexp.MustCompile(`^(?:0\.0\.0\.0|127\.0\.0\.1)\s+([a-zA-Z0-9._-]+)`)
	// "||ads.example.com^"
	abpRe = regexp.MustCompile(`^\|\|([a-zA-Z0-9._-]+)\^`)
	// plain domain
	plainRe = regexp.MustCompile(`^([a-zA-Z0-9._-]+\.[a-zA-Z]{2,})$`)
)

// Blocklist holds a set of blocked domains and a whitelist, safe for concurrent use.
type Blocklist struct {
	mu        sync.RWMutex
	domains   map[string]struct{}
	whitelist map[string]struct{}
	dataDir   string
	sources   []string
	ttl       time.Duration
}

func New(dataDir string, sources []string, ttl time.Duration) *Blocklist {
	if sources == nil {
		sources = DefaultSources
	}
	return &Blocklist{
		domains:   make(map[string]struct{}),
		whitelist: make(map[string]struct{}),
		dataDir:   dataDir,
		sources:   sources,
		ttl:       ttl,
	}
}

func (b *Blocklist) cacheFile() string    { return filepath.Join(b.dataDir, "blocklist.cache") }
func (b *Blocklist) whitelistFile() string { return filepath.Join(b.dataDir, "whitelist.txt") }

func (b *Blocklist) cacheIsFresh() bool {
	info, err := os.Stat(b.cacheFile())
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < b.ttl
}

// Load loads the blocklist from cache, updating first if the cache is stale.
func (b *Blocklist) Load() (int, error) {
	if err := os.MkdirAll(b.dataDir, 0755); err != nil {
		return 0, err
	}
	if !b.cacheIsFresh() {
		return b.Update()
	}
	return b.loadFromCache()
}

func (b *Blocklist) loadFromCache() (int, error) {
	f, err := os.Open(b.cacheFile())
	if err != nil {
		return 0, err
	}
	defer f.Close()

	domains := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		d := strings.TrimSpace(scanner.Text())
		if d != "" {
			domains[d] = struct{}{}
		}
	}

	wl, _ := b.loadWhitelistFromDisk()
	b.mu.Lock()
	b.domains = domains
	b.whitelist = wl
	b.mu.Unlock()
	return len(domains), scanner.Err()
}

// Update fetches all sources and rebuilds the cache.
func (b *Blocklist) Update() (int, error) {
	if err := os.MkdirAll(b.dataDir, 0755); err != nil {
		return 0, err
	}
	all := make(map[string]struct{})
	client := &http.Client{Timeout: 30 * time.Second}

	for _, url := range b.sources {
		fmt.Printf("  Fetching %s\n", url)
		domains, err := fetchAndParse(client, url)
		if err != nil {
			fmt.Printf("  Warning: %s: %v\n", url, err)
			continue
		}
		for d := range domains {
			all[d] = struct{}{}
		}
		fmt.Printf("  -> %d domains\n", len(domains))
	}

	// Write cache
	f, err := os.Create(b.cacheFile())
	if err != nil {
		return 0, err
	}
	w := bufio.NewWriter(f)
	for d := range all {
		fmt.Fprintln(w, d)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return 0, err
	}
	f.Close()

	wl, _ := b.loadWhitelistFromDisk()
	b.mu.Lock()
	b.domains = all
	b.whitelist = wl
	b.mu.Unlock()
	return len(all), nil
}

// IsBlocked reports whether domain (or any parent) is blocked.
func (b *Blocklist) IsBlocked(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.whitelist[domain]; ok {
		return false
	}
	parts := strings.Split(domain, ".")
	for i := range parts {
		candidate := strings.Join(parts[i:], ".")
		if _, ok := b.domains[candidate]; ok {
			// make sure it's not whitelisted at a parent level
			if _, wl := b.whitelist[candidate]; !wl {
				return true
			}
		}
	}
	return false
}

// Count returns the number of blocked domains.
func (b *Blocklist) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.domains)
}

// WhitelistAdd adds domain to the persistent whitelist.
func (b *Blocklist) WhitelistAdd(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	existing := b.WhitelistList()
	set := make(map[string]struct{}, len(existing)+1)
	for _, d := range existing {
		set[d] = struct{}{}
	}
	set[domain] = struct{}{}
	if err := b.saveWhitelist(set); err != nil {
		return err
	}
	b.mu.Lock()
	b.whitelist[domain] = struct{}{}
	b.mu.Unlock()
	return nil
}

// WhitelistRemove removes domain from the persistent whitelist.
func (b *Blocklist) WhitelistRemove(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	existing := b.WhitelistList()
	set := make(map[string]struct{}, len(existing))
	for _, d := range existing {
		if d != domain {
			set[d] = struct{}{}
		}
	}
	if err := b.saveWhitelist(set); err != nil {
		return err
	}
	b.mu.Lock()
	delete(b.whitelist, domain)
	b.mu.Unlock()
	return nil
}

// WhitelistList returns all whitelisted domains.
func (b *Blocklist) WhitelistList() []string {
	f, err := os.Open(b.whitelistFile())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		d := strings.TrimSpace(scanner.Text())
		if d != "" && !strings.HasPrefix(d, "#") {
			out = append(out, d)
		}
	}
	return out
}

func (b *Blocklist) loadWhitelistFromDisk() (map[string]struct{}, error) {
	out := make(map[string]struct{})
	for _, d := range b.WhitelistList() {
		out[d] = struct{}{}
	}
	return out, nil
}

func (b *Blocklist) saveWhitelist(set map[string]struct{}) error {
	if err := os.MkdirAll(b.dataDir, 0755); err != nil {
		return err
	}
	f, err := os.Create(b.whitelistFile())
	if err != nil {
		return err
	}
	defer f.Close()
	for d := range set {
		fmt.Fprintln(f, d)
	}
	return nil
}

func fetchAndParse(client *http.Client, url string) (map[string]struct{}, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "adblocker/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseList(resp.Body), nil
}

func parseList(r io.Reader) map[string]struct{} {
	domains := make(map[string]struct{})
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		var domain string
		switch {
		case hostsRe.MatchString(line):
			m := hostsRe.FindStringSubmatch(line)
			domain = m[1]
		case abpRe.MatchString(line):
			m := abpRe.FindStringSubmatch(line)
			domain = m[1]
		case plainRe.MatchString(line):
			domain = line
		}
		domain = strings.ToLower(domain)
		if domain != "" && strings.Contains(domain, ".") &&
			domain != "localhost" && domain != "broadcasthost" {
			domains[domain] = struct{}{}
		}
	}
	return domains
}
