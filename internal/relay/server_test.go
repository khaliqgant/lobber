// internal/relay/server_test.go
package relay

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lobber-dev/lobber/internal/tunnel"
)

func TestServerHealthCheck(t *testing.T) {
	s := NewServer(nil) // nil db for now

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestServerRejectUnknownDomain(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "unknown.example.com"
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)

	// Unknown domains should go to proxy logic and return 502 if tunnel not found.
	// They should NOT fall back to landing page.
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestQueueOverflow(t *testing.T) {
	// Create server with small queue for testing
	config := &ServerConfig{
		MaxPendingQueue: 2,
		PendingQueueTTL: 5 * time.Second,
	}
	s := NewServerWithConfig(nil, config)

	// Create a tunnel in Connected state (not Ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun := &Tunnel{
		Domain:       "overflow.example.com",
		UserID:       "test-user",
		state:        TunnelStateConnected,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}
	s.RegisterTunnel(tun)

	// Fill the queue
	for i := 0; i < config.MaxPendingQueue; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Host = "overflow.example.com"
		rec := httptest.NewRecorder()

		// Run in goroutine since it will block waiting for response
		go s.ServeHTTP(rec, req)
	}

	// Wait a bit for requests to queue
	time.Sleep(50 * time.Millisecond)

	// Next request should get 503 with Retry-After
	req := httptest.NewRequest("GET", "/overflow", nil)
	req.Host = "overflow.example.com"
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (503)", rec.Code, http.StatusServiceUnavailable)
	}

	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q, want %q", rec.Header().Get("Retry-After"), "1")
	}
}

func TestQueueTTLExpiry(t *testing.T) {
	// Create server with very short TTL for testing
	config := &ServerConfig{
		MaxPendingQueue: 100,
		PendingQueueTTL: 50 * time.Millisecond,
	}
	s := NewServerWithConfig(nil, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun := &Tunnel{
		Domain:       "ttl.example.com",
		UserID:       "test-user",
		state:        TunnelStateConnected,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}
	s.RegisterTunnel(tun)

	// Queue a request
	pr := &pendingRequest{
		req: &tunnel.Request{
			ID:     "test-req-1",
			Method: "GET",
			Path:   "/test",
		},
		respCh:   make(chan *tunnel.Response, 1),
		queuedAt: time.Now().Add(-100 * time.Millisecond), // Already expired
	}
	tun.queueMu.Lock()
	tun.pendingQueue = append(tun.pendingQueue, pr)
	tun.queueMu.Unlock()

	// Transition to Ready state, which flushes the queue
	tun.stateMu.Lock()
	tun.state = TunnelStateReady
	tun.stateMu.Unlock()
	tun.flushPendingQueue()

	// The expired request should receive 503
	select {
	case resp := <-pr.respCh:
		if resp.StatusCode != 503 {
			t.Errorf("expired request status = %d, want 503", resp.StatusCode)
		}
		if !strings.Contains(string(resp.Body), "timeout") {
			t.Errorf("expected timeout message in body, got %q", string(resp.Body))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for expired request response")
	}
}

func TestDisconnectCleanup(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServerWithConfig(nil, config)

	ctx, cancel := context.WithCancel(context.Background())

	tun := &Tunnel{
		Domain:       "cleanup.example.com",
		UserID:       "test-user",
		state:        TunnelStateConnected,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}
	tun.onClose = func() {
		s.UnregisterTunnel("cleanup.example.com")
	}
	s.RegisterTunnel(tun)

	// Queue some pending requests
	var responses []chan *tunnel.Response
	for i := 0; i < 3; i++ {
		pr := &pendingRequest{
			req: &tunnel.Request{
				ID:     "req-" + string(rune('A'+i)),
				Method: "GET",
				Path:   "/test",
			},
			respCh:   make(chan *tunnel.Response, 1),
			queuedAt: time.Now(),
		}
		tun.queueMu.Lock()
		tun.pendingQueue = append(tun.pendingQueue, pr)
		tun.queueMu.Unlock()
		responses = append(responses, pr.respCh)
	}

	// Close the tunnel (simulating disconnect)
	tun.Close()

	// All pending requests should receive 503
	for i, respCh := range responses {
		select {
		case resp := <-respCh:
			if resp.StatusCode != 503 {
				t.Errorf("request %d: status = %d, want 503", i, resp.StatusCode)
			}
			if !strings.Contains(string(resp.Body), "closed") {
				t.Errorf("request %d: expected 'closed' in body, got %q", i, string(resp.Body))
			}
		case <-time.After(time.Second):
			t.Fatalf("request %d: timed out waiting for response", i)
		}
	}

	// Tunnel should be unregistered
	if s.HasTunnel("cleanup.example.com") {
		t.Error("tunnel should be unregistered after Close()")
	}

	// State should be Closed
	if tun.GetState() != TunnelStateClosed {
		t.Errorf("state = %v, want TunnelStateClosed", tun.GetState())
	}
}

func TestCloseIdempotent(t *testing.T) {
	config := DefaultServerConfig()
	ctx, cancel := context.WithCancel(context.Background())

	closeCount := 0
	var mu sync.Mutex

	tun := &Tunnel{
		Domain:       "idempotent.example.com",
		UserID:       "test-user",
		state:        TunnelStateReady,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		onClose: func() {
			mu.Lock()
			closeCount++
			mu.Unlock()
		},
	}

	// Call Close() multiple times concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tun.Close()
		}()
	}
	wg.Wait()

	// onClose should only be called once
	mu.Lock()
	count := closeCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("onClose called %d times, want 1", count)
	}
}

func TestTunnelStateTransitions(t *testing.T) {
	config := DefaultServerConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun := &Tunnel{
		Domain:       "state.example.com",
		UserID:       "test-user",
		state:        TunnelStateConnected,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Initial state
	if tun.GetState() != TunnelStateConnected {
		t.Errorf("initial state = %v, want TunnelStateConnected", tun.GetState())
	}

	// Transition to Ready
	tun.stateMu.Lock()
	tun.state = TunnelStateReady
	tun.stateMu.Unlock()

	if tun.GetState() != TunnelStateReady {
		t.Errorf("after ready: state = %v, want TunnelStateReady", tun.GetState())
	}

	// Transition to Closed
	tun.Close()

	if tun.GetState() != TunnelStateClosed {
		t.Errorf("after close: state = %v, want TunnelStateClosed", tun.GetState())
	}
}

// mockConn implements net.Conn for testing
type mockConn struct {
	net.Conn
	closed bool
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestTunnelClosesConnection(t *testing.T) {
	config := DefaultServerConfig()
	ctx, cancel := context.WithCancel(context.Background())

	conn := &mockConn{}
	bufrw := bufio.NewReadWriter(
		bufio.NewReader(strings.NewReader("")),
		bufio.NewWriter(conn),
	)

	tun := &Tunnel{
		Domain:       "conn.example.com",
		UserID:       "test-user",
		conn:         conn,
		bufrw:        bufrw,
		state:        TunnelStateReady,
		reqCh:        make(chan *pendingRequest, 100),
		respCh:       make(chan *tunnel.Response, 100),
		done:         make(chan struct{}),
		pendingQueue: make([]*pendingRequest, 0),
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}

	tun.Close()

	if !conn.closed {
		t.Error("connection should be closed after tunnel.Close()")
	}
}
