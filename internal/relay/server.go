// internal/relay/server.go
package relay

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/lobber-dev/lobber/internal/db"
)

type Server struct {
	db       *db.DB
	mu       sync.RWMutex
	tunnels  map[string]*Tunnel // hostname -> tunnel
	mux      *http.ServeMux
}

type Tunnel struct {
	Domain   string
	UserID   string
	// TODO: HTTP/2 connection to client
}

func NewServer(database *db.DB) *Server {
	s := &Server{
		db:      database,
		tunnels: make(map[string]*Tunnel),
		mux:     http.NewServeMux(),
	}

	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/_lobber/connect", s.handleConnect)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is an internal route
	if r.URL.Path == "/health" || r.URL.Path == "/_lobber/connect" {
		s.mux.ServeHTTP(w, r)
		return
	}

	// Otherwise, try to route to a tunnel based on Host header
	s.handleProxy(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	// TODO: Upgrade to HTTP/2 tunnel
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	hostname := r.Host

	s.mu.RLock()
	tunnel, ok := s.tunnels[hostname]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "tunnel not found", http.StatusBadGateway)
		return
	}

	_ = tunnel
	// TODO: Forward request through tunnel
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) RegisterTunnel(t *Tunnel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnels[t.Domain] = t
}

func (s *Server) UnregisterTunnel(domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tunnels, domain)
}
