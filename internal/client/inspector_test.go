package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInspectorReturnsRequests(t *testing.T) {
	inspector := NewInspector()

	// Add a test request
	inspector.AddRequest(&InspectedRequest{
		ID:         "req-1",
		Method:     "POST",
		Path:       "/webhook",
		StatusCode: 200,
	})

	req := httptest.NewRequest("GET", "/api/requests", nil)
	rec := httptest.NewRecorder()

	inspector.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var requests []InspectedRequest
	if err := json.NewDecoder(rec.Body).Decode(&requests); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("len = %d, want 1", len(requests))
	}

	if requests[0].ID != "req-1" {
		t.Errorf("ID = %q, want %q", requests[0].ID, "req-1")
	}
}
