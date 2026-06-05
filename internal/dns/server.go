// Package dns provides the local DNS server that blocks ad domains.
package dns

import (
	"fmt"
	"log"
	"net"

	"github.com/miekg/dns"
)

// Checker is implemented by blocklist.Blocklist.
type Checker interface {
	IsBlocked(domain string) bool
}

// Recorder is implemented by stats.Recorder.
type Recorder interface {
	RecordBlocked(domain string)
	RecordAllowed(domain string)
	RecordError()
}

// Server is a local DNS server that forwards allowed queries to an upstream resolver.
type Server struct {
	blocklist Checker
	recorder  Recorder
	upstream  string
	udpServer *dns.Server
	tcpServer *dns.Server
}

func New(bl Checker, rec Recorder, listenAddr, upstream string) *Server {
	s := &Server{
		blocklist: bl,
		recorder:  rec,
		upstream:  upstream,
	}
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handle)
	s.udpServer = &dns.Server{Addr: listenAddr, Net: "udp", Handler: mux}
	s.tcpServer = &dns.Server{Addr: listenAddr, Net: "tcp", Handler: mux}
	return s
}

// Start begins serving DNS on both UDP and TCP. It blocks until both sockets are bound.
func (s *Server) Start() error {
	udpReady := make(chan error, 1)
	tcpReady := make(chan error, 1)

	s.udpServer.NotifyStartedFunc = func() { udpReady <- nil }
	s.tcpServer.NotifyStartedFunc = func() { tcpReady <- nil }

	go func() {
		if err := s.udpServer.ListenAndServe(); err != nil {
			udpReady <- err
		}
	}()
	go func() {
		if err := s.tcpServer.ListenAndServe(); err != nil {
			tcpReady <- err
		}
	}()

	if err := <-udpReady; err != nil {
		return fmt.Errorf("UDP: %w", err)
	}
	if err := <-tcpReady; err != nil {
		return fmt.Errorf("TCP: %w", err)
	}
	return nil
}

// Stop shuts down both servers.
func (s *Server) Stop() {
	_ = s.udpServer.Shutdown()
	_ = s.tcpServer.Shutdown()
}

func (s *Server) handle(w dns.ResponseWriter, r *dns.Msg) {
	if len(r.Question) == 0 {
		s.recorder.RecordError()
		return
	}

	q := r.Question[0]
	domain := dns.Fqdn(q.Name)

	if s.blocklist.IsBlocked(domain) {
		s.recorder.RecordBlocked(domain)
		log.Printf("[BLOCK] %s", domain)
		s.writeBlocked(w, r, q)
		return
	}

	resp, err := dns.Exchange(r, s.upstream)
	if err != nil || resp == nil {
		s.recorder.RecordError()
		log.Printf("[ERROR] forward %s: %v", domain, err)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(m)
		return
	}

	s.recorder.RecordAllowed(domain)
	_ = w.WriteMsg(resp)
}

func (s *Server) writeBlocked(w dns.ResponseWriter, r *dns.Msg, q dns.Question) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.RecursionAvailable = false

	switch q.Qtype {
	case dns.TypeA:
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.IPv4(0, 0, 0, 0),
		})
	case dns.TypeAAAA:
		m.Answer = append(m.Answer, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
			AAAA: net.IPv6zero,
		})
	default:
		m.SetRcode(r, dns.RcodeNameError)
	}

	_ = w.WriteMsg(m)
}
