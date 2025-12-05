// integration_test.go
package integration_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lobber-dev/lobber/internal/client"
	"github.com/lobber-dev/lobber/internal/relay"
)

func TestEndToEndTunnel(t *testing.T) {
	// 1. Start a local HTTP server that we want to expose
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Local-Server", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from local"))
	}))
	defer localServer.Close()

	// 2. Start relay server (in-memory, no real TLS)
	relayServer := relay.NewServer(nil)
	relayHTTP := httptest.NewServer(relayServer)
	defer relayHTTP.Close()

	// 3. Create and connect client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tunnelClient := client.New(localServer.URL, relayHTTP.URL, "test-token", "test.example.com")

	// Set up ready channel for synchronization
	readyCh := make(chan struct{})
	tunnelClient.SetOnReady(func() {
		close(readyCh)
	})

	// Start client in background
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- tunnelClient.Run(ctx)
	}()

	// 4. Wait for tunnel to be ready (replaces time.Sleep)
	select {
	case <-readyCh:
		// Client is ready
	case err := <-clientErr:
		t.Fatalf("client error before ready: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tunnel to become ready")
	}

	// Also wait for server to receive and process the ready frame
	tun := relayServer.GetTunnel("test.example.com")
	if tun == nil {
		t.Fatal("tunnel not registered")
	}
	select {
	case <-tun.GetReadyChannel():
		// Tunnel is ready on server side
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to mark tunnel ready")
	}

	// 5. Make request to relay with Host header matching the tunnel domain
	req, err := http.NewRequest("GET", relayHTTP.URL+"/api/test", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Host = "test.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request through tunnel: %v", err)
	}
	defer resp.Body.Close()

	// 6. Verify response comes from local server
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, []byte("hello from local")) {
		t.Errorf("body = %q, want %q", string(body), "hello from local")
	}

	if resp.Header.Get("X-Local-Server") != "true" {
		t.Error("missing X-Local-Server header from local server")
	}

	// Cancel to stop the client
	cancel()
}

func TestTunnelHandshake(t *testing.T) {
	// Test that client can connect and register with relay
	relayServer := relay.NewServer(nil)
	relayHTTP := httptest.NewServer(relayServer)
	defer relayHTTP.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tunnelClient := client.New("http://localhost:3000", relayHTTP.URL, "test-token", "myapp.example.com")

	// Connect should succeed (not return "not implemented")
	err := tunnelClient.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Verify tunnel is registered
	if !relayServer.HasTunnel("myapp.example.com") {
		t.Error("tunnel not registered after Connect()")
	}
}
