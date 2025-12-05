package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lobber-dev/lobber/internal/auth"
)

func TestConnectRequiresAuth(t *testing.T) {
	s := NewServer(nil)
	srv := httptest.NewServer(s)
	defer srv.Close()

	// Request without auth should fail
	req, _ := http.NewRequest("POST", srv.URL+"/_lobber/connect", nil)
	req.Header.Set("X-Lobber-Domain", "test.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (Unauthorized)", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestConnectWithValidToken(t *testing.T) {
	// Generate a valid token
	plaintext, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken() error: %v", err)
	}

	// Create server with token validator
	s := NewServer(nil)
	s.SetTokenValidator(func(token string) (string, bool) {
		if auth.ValidateAPIToken(token, hash) {
			return "user123", true
		}
		return "", false
	})

	srv := httptest.NewServer(s)
	defer srv.Close()

	// Request with valid token should succeed (will hijack connection)
	// We just verify it doesn't return 401
	req, _ := http.NewRequest("POST", srv.URL+"/_lobber/connect", nil)
	req.Header.Set("X-Lobber-Domain", "test.example.com")
	req.Header.Set("Authorization", "Bearer "+plaintext)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Connection may be hijacked, which causes client error - that's OK
		return
	}
	defer resp.Body.Close()

	// Should not be unauthorized
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("valid token should not return 401")
	}
}
