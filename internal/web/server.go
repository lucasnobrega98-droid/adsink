// Package web serves the statistics dashboard over HTTP.
package web

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"

	"github.com/lucasnobrega98/adblocker/internal/stats"
)

//go:embed dashboard.html
var dashboardHTML []byte

// Server is the HTTP stats dashboard.
type Server struct {
	rec *stats.Recorder
	srv *http.Server
}

func New(rec *stats.Recorder, addr string) *Server {
	s := &Server{rec: rec}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/stats", s.handleStats)
	s.srv = &http.Server{Addr: addr, Handler: mux}
	return s
}

// Start launches the HTTP server in the background.
func (s *Server) Start() {
	go func() {
		log.Printf("Web dashboard: http://%s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()
}

func (s *Server) Stop() { _ = s.srv.Close() }

func (s *Server) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	snap := s.rec.Snapshot()
	_ = json.NewEncoder(w).Encode(snap)
}
