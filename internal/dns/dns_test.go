package dns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

// mockChecker implements Checker.
type mockChecker struct{ blocked bool }

func (m *mockChecker) IsBlocked(string) bool { return m.blocked }

// mockRecorder implements Recorder.
type mockRecorder struct {
	blocked, allowed, errors int
}

func (m *mockRecorder) RecordBlocked(string) { m.blocked++ }
func (m *mockRecorder) RecordAllowed(string) { m.allowed++ }
func (m *mockRecorder) RecordError()         { m.errors++ }

// mockWriter implements dns.ResponseWriter; captures the last written message.
type mockWriter struct{ msg *dns.Msg }

func (m *mockWriter) LocalAddr() net.Addr         { return &net.UDPAddr{} }
func (m *mockWriter) RemoteAddr() net.Addr        { return &net.UDPAddr{} }
func (m *mockWriter) WriteMsg(msg *dns.Msg) error { m.msg = msg; return nil }
func (m *mockWriter) Write([]byte) (int, error)   { return 0, nil }
func (m *mockWriter) Close() error                { return nil }
func (m *mockWriter) TsigStatus() error           { return nil }
func (m *mockWriter) TsigTimersOnly(bool)         {}
func (m *mockWriter) Hijack()                     {}

func query(name string, qtype uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	return m
}

func newServer(blocked bool) (*Server, *mockRecorder) {
	rec := &mockRecorder{}
	s := New(&mockChecker{blocked: blocked}, rec, "127.0.0.1:0", "127.0.0.1:53")
	return s, rec
}

func TestHandle_BlockedA(t *testing.T) {
	s, rec := newServer(true)
	w := &mockWriter{}

	s.handle(w, query("ads.example.com", dns.TypeA))

	if rec.blocked != 1 {
		t.Errorf("blocked count = %d, want 1", rec.blocked)
	}
	if w.msg == nil || len(w.msg.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %v", w.msg)
	}
	a, ok := w.msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer is not an A record: %T", w.msg.Answer[0])
	}
	if a.A.String() != "0.0.0.0" {
		t.Errorf("A record = %s, want 0.0.0.0", a.A)
	}
}

func TestHandle_BlockedAAAA(t *testing.T) {
	s, _ := newServer(true)
	w := &mockWriter{}

	s.handle(w, query("ads.example.com", dns.TypeAAAA))

	if w.msg == nil || len(w.msg.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %v", w.msg)
	}
	aaaa, ok := w.msg.Answer[0].(*dns.AAAA)
	if !ok {
		t.Fatalf("answer is not a AAAA record: %T", w.msg.Answer[0])
	}
	if !aaaa.AAAA.Equal(net.IPv6zero) {
		t.Errorf("AAAA record = %v, want IPv6 zero", aaaa.AAAA)
	}
}

func TestHandle_BlockedOtherType_NXDOMAIN(t *testing.T) {
	s, _ := newServer(true)
	w := &mockWriter{}

	s.handle(w, query("ads.example.com", dns.TypeMX))

	if w.msg == nil {
		t.Fatal("no response written")
	}
	if w.msg.Rcode != dns.RcodeNameError {
		t.Errorf("rcode = %d, want NXDOMAIN (%d)", w.msg.Rcode, dns.RcodeNameError)
	}
}

func TestHandle_EmptyQuestion_RecordsError(t *testing.T) {
	s, rec := newServer(false)
	w := &mockWriter{}

	s.handle(w, new(dns.Msg)) // no questions

	if rec.errors != 1 {
		t.Errorf("error count = %d, want 1", rec.errors)
	}
}

func TestHandle_BlockedSetsAuthoritative(t *testing.T) {
	s, _ := newServer(true)
	w := &mockWriter{}

	s.handle(w, query("ads.example.com", dns.TypeA))

	if !w.msg.Authoritative {
		t.Error("blocked response should be authoritative")
	}
}
