// internal/tunnel/protocol_test.go
package tunnel

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeRequest(t *testing.T) {
	req := &Request{
		ID:      "req-123",
		Method:  "POST",
		Path:    "/api/webhook",
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    []byte(`{"event":"test"}`),
	}

	var buf bytes.Buffer
	if err := EncodeRequest(&buf, req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeRequest(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != req.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, req.ID)
	}
	if decoded.Method != req.Method {
		t.Errorf("Method = %q, want %q", decoded.Method, req.Method)
	}
	if decoded.Path != req.Path {
		t.Errorf("Path = %q, want %q", decoded.Path, req.Path)
	}
	if !bytes.Equal(decoded.Body, req.Body) {
		t.Errorf("Body = %q, want %q", decoded.Body, req.Body)
	}
}

func TestEncodeDecodeResponse(t *testing.T) {
	resp := &Response{
		ID:         "req-123",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       []byte(`{"ok":true}`),
	}

	var buf bytes.Buffer
	if err := EncodeResponse(&buf, resp); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeResponse(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != resp.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, resp.ID)
	}
	if decoded.StatusCode != resp.StatusCode {
		t.Errorf("StatusCode = %d, want %d", decoded.StatusCode, resp.StatusCode)
	}
}
