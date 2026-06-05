package blocklist

import (
	"os"
	"strings"
	"testing"
)

func TestParseList(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   []string // domains expected to be present
		absent []string // domains expected to be absent
	}{
		{
			name:  "hosts format 0.0.0.0",
			input: "0.0.0.0 ads.example.com\n",
			want:  []string{"ads.example.com"},
		},
		{
			name:  "hosts format 127.0.0.1",
			input: "127.0.0.1 tracker.net\n",
			want:  []string{"tracker.net"},
		},
		{
			name:  "abp format",
			input: "||abp-domain.example.com^\n",
			want:  []string{"abp-domain.example.com"},
		},
		{
			name:  "plain domain",
			input: "plain.example.com\n",
			want:  []string{"plain.example.com"},
		},
		{
			name:   "skips hash comments",
			input:  "# this is a comment\n0.0.0.0 ads.example.com\n",
			want:   []string{"ads.example.com"},
			absent: []string{"this"},
		},
		{
			name:   "skips abp comments",
			input:  "! abp comment\n||ads.example.com^\n",
			want:   []string{"ads.example.com"},
			absent: []string{"abp"},
		},
		{
			name:   "skips localhost and broadcasthost",
			input:  "0.0.0.0 localhost\n0.0.0.0 broadcasthost\n",
			absent: []string{"localhost", "broadcasthost"},
		},
		{
			name:  "lowercases domains",
			input: "0.0.0.0 ADS.EXAMPLE.COM\n",
			want:  []string{"ads.example.com"},
		},
		{
			name:   "skips blank lines",
			input:  "\n\n0.0.0.0 ads.example.com\n\n",
			want:   []string{"ads.example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseList(strings.NewReader(tc.input))
			for _, d := range tc.want {
				if _, ok := got[d]; !ok {
					t.Errorf("expected %q in result, not found", d)
				}
			}
			for _, d := range tc.absent {
				if _, ok := got[d]; ok {
					t.Errorf("expected %q absent from result, but found it", d)
				}
			}
		})
	}
}

func blocklist_with(domains ...string) *Blocklist {
	bl := &Blocklist{
		domains:   make(map[string]struct{}),
		whitelist: make(map[string]struct{}),
	}
	for _, d := range domains {
		bl.domains[d] = struct{}{}
	}
	return bl
}

func TestIsBlocked(t *testing.T) {
	bl := blocklist_with("ads.example.com", "tracker.net")

	tests := []struct {
		domain  string
		blocked bool
	}{
		{"ads.example.com", true},
		{"sub.ads.example.com", true},    // subdomain of blocked parent
		{"tracker.net", true},
		{"safe.com", false},
		{"ADS.EXAMPLE.COM", true},        // case insensitive
		{"ads.example.com.", true},       // trailing dot stripped
		{"notads.example.com", false},    // different subdomain
		{"com", false},                   // TLD only
	}

	for _, tc := range tests {
		t.Run(tc.domain, func(t *testing.T) {
			if got := bl.IsBlocked(tc.domain); got != tc.blocked {
				t.Errorf("IsBlocked(%q) = %v, want %v", tc.domain, got, tc.blocked)
			}
		})
	}
}

func TestIsBlocked_WhitelistOverridesBlock(t *testing.T) {
	bl := blocklist_with("ads.example.com")
	bl.whitelist["ads.example.com"] = struct{}{}

	if bl.IsBlocked("ads.example.com") {
		t.Error("whitelisted domain should not be blocked")
	}
}

func TestIsBlocked_SubdomainWhitelisted(t *testing.T) {
	bl := blocklist_with("example.com")
	bl.whitelist["sub.example.com"] = struct{}{}

	if !bl.IsBlocked("other.example.com") {
		t.Error("non-whitelisted subdomain should be blocked")
	}
}

func TestWhitelist_AddListRemove(t *testing.T) {
	dir := t.TempDir()
	bl := New(dir, []string{}, 0)

	if err := bl.WhitelistAdd("safe.example.com"); err != nil {
		t.Fatalf("WhitelistAdd: %v", err)
	}
	if err := bl.WhitelistAdd("other.example.com"); err != nil {
		t.Fatalf("WhitelistAdd: %v", err)
	}

	list := bl.WhitelistList()
	if len(list) != 2 {
		t.Fatalf("WhitelistList len = %d, want 2", len(list))
	}

	if err := bl.WhitelistRemove("safe.example.com"); err != nil {
		t.Fatalf("WhitelistRemove: %v", err)
	}

	list = bl.WhitelistList()
	if len(list) != 1 || list[0] != "other.example.com" {
		t.Errorf("after remove, WhitelistList = %v, want [other.example.com]", list)
	}
}

func TestWhitelist_InMemoryUpdated(t *testing.T) {
	dir := t.TempDir()
	bl := New(dir, []string{}, 0)
	bl.domains["ads.example.com"] = struct{}{}

	if err := bl.WhitelistAdd("ads.example.com"); err != nil {
		t.Fatalf("WhitelistAdd: %v", err)
	}
	if bl.IsBlocked("ads.example.com") {
		t.Error("domain should not be blocked after being whitelisted")
	}

	if err := bl.WhitelistRemove("ads.example.com"); err != nil {
		t.Fatalf("WhitelistRemove: %v", err)
	}
	if !bl.IsBlocked("ads.example.com") {
		t.Error("domain should be blocked after whitelist removal")
	}
}

func TestLoadFromCache(t *testing.T) {
	dir := t.TempDir()
	bl := New(dir, []string{}, 0)

	cacheContent := "ads.example.com\ntracker.net\n\n"
	if err := os.WriteFile(bl.cacheFile(), []byte(cacheContent), 0644); err != nil {
		t.Fatal(err)
	}

	n, err := bl.loadFromCache()
	if err != nil {
		t.Fatalf("loadFromCache: %v", err)
	}
	if n != 2 {
		t.Errorf("loaded %d domains, want 2", n)
	}
	if !bl.IsBlocked("ads.example.com") {
		t.Error("ads.example.com should be blocked after load")
	}
}

func TestCount(t *testing.T) {
	bl := blocklist_with("a.com", "b.com", "c.com")
	if got := bl.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
}
