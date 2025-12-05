package client

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientForwardsRequests(t *testing.T) {
	// Create a local server that will receive forwarded requests
	localHits := make(chan *http.Request, 1)
	localServer := startClientTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localHits <- r
		w.Header().Set("X-Local", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer localServer.Close()

	// Create client
	client := &Client{
		LocalAddr: localServer.URL,
		RelayAddr: "wss://tunnel.lobber.dev", // Not used in unit test
		Token:     "test-token",
		Domain:    "app.mysite.com",
	}

	// Test the ForwardRequest method directly
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "/api/test", nil)
	resp, err := client.ForwardToLocal(req)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	select {
	case hit := <-localHits:
		if hit.URL.Path != "/api/test" {
			t.Errorf("Path = %q, want %q", hit.URL.Path, "/api/test")
		}
	case <-time.After(time.Second):
		t.Error("local server not hit")
	}
}

func startClientTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("skipping test server start: %v", err)
		}
		t.Fatalf("listen error: %v", err)
	}

	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()
	return srv
}
