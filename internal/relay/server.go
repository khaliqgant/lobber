// internal/relay/server.go
package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lobber-dev/lobber/internal/db"
	"github.com/lobber-dev/lobber/internal/tunnel"
)

// TokenValidator validates a token and returns (userID, valid)
type TokenValidator func(token string) (string, bool)

// TunnelState represents the lifecycle state of a tunnel connection
type TunnelState int

const (
	TunnelStateConnected  TunnelState = iota // Connection established, waiting for ready
	TunnelStateReady                         // Ready frame received, can process requests
	TunnelStateClosed                        // Connection closed
)

// ServerConfig holds configurable parameters for the relay server
type ServerConfig struct {
	MaxPendingQueue int           // Max requests to queue before tunnel ready (default 100)
	PendingQueueTTL time.Duration // Max time a request can wait in queue (default 5s)
}

// DefaultServerConfig returns sensible defaults
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		MaxPendingQueue: 100,
		PendingQueueTTL: 5 * time.Second,
	}
}

type Server struct {
	db             *db.DB
	mu             sync.RWMutex
	tunnels        map[string]*Tunnel // hostname -> tunnel
	mux            *http.ServeMux
	tokenValidator TokenValidator
	config         *ServerConfig
}

// pendingRequest holds a request waiting for tunnel to become ready
type pendingRequest struct {
	req      *tunnel.Request
	respCh   chan *tunnel.Response
	queuedAt time.Time
}

type Tunnel struct {
	Domain string
	UserID string
	conn   net.Conn
	bufrw  *bufio.ReadWriter

	// State machine
	state   TunnelState
	stateMu sync.RWMutex

	// Request/response channels for dedicated I/O goroutines
	reqCh  chan *pendingRequest
	respCh chan *tunnel.Response
	done   chan struct{}

	// Pre-ready queue
	pendingQueue []*pendingRequest
	queueMu      sync.Mutex
	config       *ServerConfig

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

func NewServer(database *db.DB) *Server {
	return NewServerWithConfig(database, DefaultServerConfig())
}

func NewServerWithConfig(database *db.DB, config *ServerConfig) *Server {
	if config == nil {
		config = DefaultServerConfig()
	}
	s := &Server{
		db:      database,
		tunnels: make(map[string]*Tunnel),
		mux:     http.NewServeMux(),
		config:  config,
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
	// Get domain from header
	domain := r.Header.Get("X-Lobber-Domain")
	if domain == "" {
		http.Error(w, "missing X-Lobber-Domain header", http.StatusBadRequest)
		return
	}

	// Validate auth token
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" || token == authHeader {
		http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	userID := "anonymous"
	if s.tokenValidator != nil {
		var valid bool
		userID, valid = s.tokenValidator(token)
		if !valid {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
	}

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "hijack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Send HTTP 200 OK response to indicate successful connection
	bufrw.WriteString("HTTP/1.1 200 OK\r\n")
	bufrw.WriteString("Content-Type: application/octet-stream\r\n")
	bufrw.WriteString("\r\n")
	bufrw.Flush()

	// Create context for tunnel lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	// Create the tunnel in Connected state
	t := &Tunnel{
		Domain:       domain,
		UserID:       userID,
		conn:         conn,
		bufrw:        bufrw,
		state:        TunnelStateConnected,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       s.config,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Register tunnel (even before ready, so requests can queue)
	s.RegisterTunnel(t)

	// Handle the tunnel lifecycle in a goroutine
	go func() {
		// First wait for ready frame
		if err := t.waitForReady(); err != nil {
			t.Close()
			s.UnregisterTunnel(domain)
			return
		}

		// Once ready, start I/O goroutines
		go t.writeLoop()
		t.readLoop() // Block on read loop
	}()
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	hostname := r.Host

	s.mu.RLock()
	tun, ok := s.tunnels[hostname]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "tunnel not found", http.StatusBadGateway)
		return
	}

	// Check tunnel state
	tun.stateMu.RLock()
	state := tun.state
	tun.stateMu.RUnlock()

	if state == TunnelStateClosed {
		http.Error(w, "tunnel closed", http.StatusBadGateway)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Generate request ID if not provided
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = generateRequestID()
	}

	// Create tunnel request
	tunnelReq := &tunnel.Request{
		ID:      reqID,
		Method:  r.Method,
		Path:    r.URL.RequestURI(),
		Headers: r.Header,
		Body:    body,
	}

	// Create pending request with response channel
	pr := &pendingRequest{
		req:      tunnelReq,
		respCh:   make(chan *tunnel.Response, 1),
		queuedAt: time.Now(),
	}

	// If tunnel not ready, queue the request
	if state == TunnelStateConnected {
		tun.queueMu.Lock()
		if len(tun.pendingQueue) >= tun.config.MaxPendingQueue {
			tun.queueMu.Unlock()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "tunnel not ready, queue full", http.StatusServiceUnavailable)
			return
		}
		tun.pendingQueue = append(tun.pendingQueue, pr)
		tun.queueMu.Unlock()
	} else {
		// Tunnel is ready, send directly
		select {
		case tun.reqCh <- pr:
		case <-tun.done:
			http.Error(w, "tunnel closed", http.StatusBadGateway)
			return
		}
	}

	// Wait for response with TTL
	select {
	case resp := <-pr.respCh:
		if resp == nil {
			http.Error(w, "tunnel error", http.StatusBadGateway)
			return
		}
		// Write response headers
		for k, vals := range resp.Headers {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(resp.Body)
	case <-time.After(tun.config.PendingQueueTTL + 5*time.Second):
		http.Error(w, "tunnel response timeout", http.StatusGatewayTimeout)
	case <-tun.done:
		http.Error(w, "tunnel closed", http.StatusBadGateway)
	}
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

// HasTunnel checks if a tunnel is registered for the given domain
func (s *Server) HasTunnel(domain string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.tunnels[domain]
	return ok
}

// SetTokenValidator sets the function used to validate auth tokens
func (s *Server) SetTokenValidator(v TokenValidator) {
	s.tokenValidator = v
}

// Tunnel methods

// waitForReady waits for the client to send a ready frame
func (t *Tunnel) waitForReady() error {
	if err := tunnel.DecodeReady(t.bufrw); err != nil {
		return err
	}

	// Transition to Ready state
	t.stateMu.Lock()
	t.state = TunnelStateReady
	t.stateMu.Unlock()

	// Flush pending queue
	t.flushPendingQueue()

	return nil
}

// flushPendingQueue sends all queued requests to reqCh
func (t *Tunnel) flushPendingQueue() {
	t.queueMu.Lock()
	defer t.queueMu.Unlock()

	now := time.Now()
	for _, pr := range t.pendingQueue {
		// Check TTL - discard expired requests
		if now.Sub(pr.queuedAt) > t.config.PendingQueueTTL {
			pr.respCh <- &tunnel.Response{
				ID:         pr.req.ID,
				StatusCode: 503,
				Headers:    map[string][]string{"Content-Type": {"text/plain"}},
				Body:       []byte("request timeout in queue"),
			}
			close(pr.respCh)
			continue
		}

		select {
		case t.reqCh <- pr:
		default:
			// Channel full, fail the request
			pr.respCh <- &tunnel.Response{
				ID:         pr.req.ID,
				StatusCode: 503,
				Headers:    map[string][]string{"Content-Type": {"text/plain"}},
				Body:       []byte("tunnel overloaded"),
			}
			close(pr.respCh)
		}
	}
	t.pendingQueue = nil
}

// readLoop handles all reads from the tunnel connection
func (t *Tunnel) readLoop() {
	defer t.Close()

	// Map to track pending requests by ID
	pending := make(map[string]*pendingRequest)
	var pendingMu sync.Mutex

	// Goroutine to track outgoing requests
	go func() {
		for {
			select {
			case pr := <-t.reqCh:
				pendingMu.Lock()
				pending[pr.req.ID] = pr
				pendingMu.Unlock()

				// Send to write loop
				select {
				case t.respCh <- nil: // Signal to write
				default:
				}

				// Actually write the request
				if err := tunnel.EncodeRequest(t.bufrw, pr.req); err != nil {
					pendingMu.Lock()
					delete(pending, pr.req.ID)
					pendingMu.Unlock()
					pr.respCh <- nil
					close(pr.respCh)
					return
				}
				t.bufrw.Flush()

			case <-t.done:
				return
			}
		}
	}()

	// Read responses from client
	for {
		select {
		case <-t.done:
			return
		default:
		}

		resp, err := tunnel.DecodeResponse(t.bufrw)
		if err != nil {
			return
		}

		pendingMu.Lock()
		pr, ok := pending[resp.ID]
		if ok {
			delete(pending, resp.ID)
		}
		pendingMu.Unlock()

		if ok && pr.respCh != nil {
			pr.respCh <- resp
			close(pr.respCh)
		}
	}
}

// writeLoop is now integrated into readLoop for simplicity
func (t *Tunnel) writeLoop() {
	// Requests are written in readLoop's goroutine
	// This is kept for potential future use
	<-t.done
}

// Close shuts down the tunnel and cleans up pending requests
func (t *Tunnel) Close() {
	t.stateMu.Lock()
	if t.state == TunnelStateClosed {
		t.stateMu.Unlock()
		return
	}
	t.state = TunnelStateClosed
	t.stateMu.Unlock()

	// Cancel context and signal done
	t.cancel()
	close(t.done)

	// Close connection
	if t.conn != nil {
		t.conn.Close()
	}

	// Fail all pending queue requests
	t.queueMu.Lock()
	for _, pr := range t.pendingQueue {
		pr.respCh <- &tunnel.Response{
			ID:         pr.req.ID,
			StatusCode: 503,
			Headers:    map[string][]string{"Content-Type": {"text/plain"}},
			Body:       []byte("tunnel closed"),
		}
		close(pr.respCh)
	}
	t.pendingQueue = nil
	t.queueMu.Unlock()
}

// GetState returns the current tunnel state
func (t *Tunnel) GetState() TunnelState {
	t.stateMu.RLock()
	defer t.stateMu.RUnlock()
	return t.state
}

// generateRequestID creates a unique request ID
func generateRequestID() string {
	return time.Now().Format("20060102150405.000000000")
}

// GetReadyChannel returns a channel that closes when tunnel is ready (for testing)
func (t *Tunnel) GetReadyChannel() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		for {
			t.stateMu.RLock()
			state := t.state
			t.stateMu.RUnlock()
			if state == TunnelStateReady {
				close(ch)
				return
			}
			if state == TunnelStateClosed {
				close(ch)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return ch
}

// GetTunnel returns a tunnel by domain (for testing)
func (s *Server) GetTunnel(domain string) *Tunnel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tunnels[domain]
}
