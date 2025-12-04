// internal/relay/server_test.go
package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
