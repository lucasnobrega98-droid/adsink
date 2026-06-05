package stats

import (
	"testing"
	"time"
)

func TestRecorder_Counts(t *testing.T) {
	r := New(242636)
	r.RecordBlocked("ads.example.com")
	r.RecordBlocked("ads.example.com")
	r.RecordAllowed("safe.com")
	r.RecordError()

	s := r.Snapshot()
	if s.TotalBlocked != 2 {
		t.Errorf("TotalBlocked = %d, want 2", s.TotalBlocked)
	}
	if s.TotalAllowed != 1 {
		t.Errorf("TotalAllowed = %d, want 1", s.TotalAllowed)
	}
	if s.TotalErrors != 1 {
		t.Errorf("TotalErrors = %d, want 1", s.TotalErrors)
	}
	if s.BlocklistSize != 242636 {
		t.Errorf("BlocklistSize = %d, want 242636", s.BlocklistSize)
	}
}

func TestRecorder_BlockPct(t *testing.T) {
	r := New(0)
	r.RecordBlocked("a.com")
	r.RecordAllowed("b.com")

	s := r.Snapshot()
	if s.BlockPct != 50.0 {
		t.Errorf("BlockPct = %f, want 50.0", s.BlockPct)
	}
}

func TestRecorder_BlockPct_ZeroTotal(t *testing.T) {
	r := New(0)
	s := r.Snapshot()
	if s.BlockPct != 0 {
		t.Errorf("BlockPct with no queries = %f, want 0", s.BlockPct)
	}
}

func TestRecorder_TopBlocked_Ordered(t *testing.T) {
	r := New(0)
	r.RecordBlocked("b.com")
	r.RecordBlocked("a.com")
	r.RecordBlocked("a.com")

	s := r.Snapshot()
	if len(s.TopBlocked) == 0 {
		t.Fatal("TopBlocked is empty")
	}
	if s.TopBlocked[0].Domain != "a.com" {
		t.Errorf("TopBlocked[0] = %q, want a.com", s.TopBlocked[0].Domain)
	}
	if s.TopBlocked[0].Count != 2 {
		t.Errorf("TopBlocked[0].Count = %d, want 2", s.TopBlocked[0].Count)
	}
}

func TestRecorder_RecentBlocked_NewestFirst(t *testing.T) {
	r := New(0)
	r.RecordBlocked("first.com")
	r.RecordBlocked("second.com")

	s := r.Snapshot()
	if len(s.RecentBlocked) != 2 {
		t.Fatalf("RecentBlocked len = %d, want 2", len(s.RecentBlocked))
	}
	if s.RecentBlocked[0] != "second.com" {
		t.Errorf("RecentBlocked[0] = %q, want second.com", s.RecentBlocked[0])
	}
}

func TestRecorder_RecentBlocked_Deduplicated(t *testing.T) {
	r := New(0)
	r.RecordBlocked("a.com")
	r.RecordBlocked("b.com")
	r.RecordBlocked("a.com")

	s := r.Snapshot()
	if len(s.RecentBlocked) != 2 {
		t.Errorf("RecentBlocked len = %d, want 2 (deduped)", len(s.RecentBlocked))
	}
}

func TestRecorder_TrailingDotStripped(t *testing.T) {
	r := New(0)
	r.RecordBlocked("ads.example.com.")
	r.RecordAllowed("safe.com.")

	s := r.Snapshot()
	if s.TopBlocked[0].Domain != "ads.example.com" {
		t.Errorf("TopBlocked domain has trailing dot: %q", s.TopBlocked[0].Domain)
	}
	if s.TopAllowed[0].Domain != "safe.com" {
		t.Errorf("TopAllowed domain has trailing dot: %q", s.TopAllowed[0].Domain)
	}
}

func TestRecorder_AllowedCap(t *testing.T) {
	r := New(0)
	// Fill to cap
	for i := 0; i < maxAllowed; i++ {
		r.RecordAllowed("existing.com")
	}
	// Updating an existing domain still works beyond cap
	r.RecordAllowed("existing.com")

	s := r.Snapshot()
	if s.TotalAllowed != uint64(maxAllowed)+1 {
		t.Errorf("TotalAllowed = %d, want %d", s.TotalAllowed, maxAllowed+1)
	}
}

func TestRing_CapacityNotExceeded(t *testing.T) {
	var rb ring
	for i := 0; i < recentCap+10; i++ {
		rb.push("x.com")
	}
	if rb.n != recentCap {
		t.Errorf("ring.n = %d, want %d", rb.n, recentCap)
	}
}

func TestRing_Order(t *testing.T) {
	var rb ring
	rb.push("first")
	rb.push("second")
	rb.push("third")

	got := rb.slice()
	if got[0] != "third" || got[1] != "second" || got[2] != "first" {
		t.Errorf("ring.slice() = %v, want [third second first]", got)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{3661 * time.Second, "1h 1m 1s"},
		{3600 * time.Second, "1h 0m 0s"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := formatDuration(tc.d); got != tc.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
