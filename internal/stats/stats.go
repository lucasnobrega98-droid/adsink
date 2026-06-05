// Package stats tracks per-domain DNS query metrics, safe for concurrent use.
package stats

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	recentCap   = 50
	topNDomains = 20
	maxAllowed  = 10_000 // cap on distinct allowed domains tracked
)

// DomainCount is one row in a top-N table.
type DomainCount struct {
	Domain string `json:"domain"`
	Count  uint64 `json:"count"`
}

// Snapshot is the immutable, JSON-serialisable view of current stats.
type Snapshot struct {
	Uptime        string        `json:"uptime"`
	UptimeSec     float64       `json:"uptime_sec"`
	TotalBlocked  uint64        `json:"total_blocked"`
	TotalAllowed  uint64        `json:"total_allowed"`
	TotalErrors   uint64        `json:"total_errors"`
	BlockPct      float64       `json:"block_pct"`
	QueriesPerSec float64       `json:"queries_per_sec"`
	TopBlocked    []DomainCount `json:"top_blocked"`
	TopAllowed    []DomainCount `json:"top_allowed"`
	RecentBlocked []string      `json:"recent_blocked"`
	BlocklistSize int           `json:"blocklist_size"`
	StartTimeUnix int64         `json:"start_time_unix"`
}

// ring is a fixed-capacity circular buffer of strings.
type ring struct {
	buf  [recentCap]string
	head int
	n    int
}

func (rb *ring) push(s string) {
	rb.buf[rb.head] = s
	rb.head = (rb.head + 1) % recentCap
	if rb.n < recentCap {
		rb.n++
	}
}

// slice returns up to recentCap entries newest-first, deduplicated.
func (rb *ring) slice() []string {
	out := make([]string, 0, rb.n)
	seen := make(map[string]struct{}, rb.n)
	for i := 0; i < rb.n; i++ {
		idx := ((rb.head - 1 - i) % recentCap + recentCap) % recentCap
		d := rb.buf[idx]
		if d != "" {
			if _, ok := seen[d]; !ok {
				seen[d] = struct{}{}
				out = append(out, d)
			}
		}
	}
	return out
}

// Recorder accumulates query statistics. All methods are safe for concurrent use.
type Recorder struct {
	mu            sync.RWMutex
	blockedMap    map[string]uint64
	allowedMap    map[string]uint64
	recent        ring
	totalBlocked  uint64
	totalAllowed  uint64
	totalErrors   uint64
	startTime     time.Time
	blocklistSize int
}

func New(blocklistSize int) *Recorder {
	return &Recorder{
		blockedMap:    make(map[string]uint64),
		allowedMap:    make(map[string]uint64),
		startTime:     time.Now(),
		blocklistSize: blocklistSize,
	}
}

func (r *Recorder) RecordBlocked(domain string) {
	domain = strings.TrimSuffix(domain, ".")
	r.mu.Lock()
	r.blockedMap[domain]++
	r.totalBlocked++
	r.recent.push(domain)
	r.mu.Unlock()
}

func (r *Recorder) RecordAllowed(domain string) {
	domain = strings.TrimSuffix(domain, ".")
	r.mu.Lock()
	// Only track new allowed domains up to the cap; always update existing ones.
	if _, exists := r.allowedMap[domain]; exists || len(r.allowedMap) < maxAllowed {
		r.allowedMap[domain]++
	}
	r.totalAllowed++
	r.mu.Unlock()
}

func (r *Recorder) RecordError() {
	r.mu.Lock()
	r.totalErrors++
	r.mu.Unlock()
}

// Snapshot returns a point-in-time copy of all metrics.
func (r *Recorder) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	uptime := time.Since(r.startTime)
	uptimeSec := uptime.Seconds()

	total := r.totalBlocked + r.totalAllowed
	var blockPct, qps float64
	if total > 0 {
		blockPct = float64(r.totalBlocked) / float64(total) * 100
	}
	if uptimeSec > 0 {
		qps = float64(r.totalBlocked+r.totalAllowed+r.totalErrors) / uptimeSec
	}

	return Snapshot{
		Uptime:        formatDuration(uptime),
		UptimeSec:     uptimeSec,
		TotalBlocked:  r.totalBlocked,
		TotalAllowed:  r.totalAllowed,
		TotalErrors:   r.totalErrors,
		BlockPct:      blockPct,
		QueriesPerSec: qps,
		TopBlocked:    topN(r.blockedMap, topNDomains),
		TopAllowed:    topN(r.allowedMap, topNDomains),
		RecentBlocked: r.recent.slice(),
		BlocklistSize: r.blocklistSize,
		StartTimeUnix: r.startTime.Unix(),
	}
}

func topN(m map[string]uint64, n int) []DomainCount {
	out := make([]DomainCount, 0, len(m))
	for d, c := range m {
		out = append(out, DomainCount{Domain: d, Count: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
