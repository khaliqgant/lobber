// internal/tunnel/protocol.go
package tunnel

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Message types for framing
const (
	TypeRequest  byte = 0x01
	TypeResponse byte = 0x02
)

// Request represents an HTTP request to forward through tunnel
type Request struct {
	ID      string              `json:"id"`
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

// Response represents an HTTP response from the tunnel client
type Response struct {
	ID         string              `json:"id"`
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
}

// EncodeRequest writes a request to the wire
func EncodeRequest(w io.Writer, req *Request) error {
	return encodeMessage(w, TypeRequest, req)
}

// DecodeRequest reads a request from the wire
func DecodeRequest(r io.Reader) (*Request, error) {
	var req Request
	if err := decodeMessage(r, TypeRequest, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// EncodeResponse writes a response to the wire
func EncodeResponse(w io.Writer, resp *Response) error {
	return encodeMessage(w, TypeResponse, resp)
}

// DecodeResponse reads a response from the wire
func DecodeResponse(r io.Reader) (*Response, error) {
	var resp Response
	if err := decodeMessage(r, TypeResponse, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func encodeMessage(w io.Writer, msgType byte, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Frame format: [type:1][length:4][payload:n]
	if err := binary.Write(w, binary.BigEndian, msgType); err != nil {
		return fmt.Errorf("write type: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

func decodeMessage(r io.Reader, expectedType byte, v any) error {
	var msgType byte
	if err := binary.Read(r, binary.BigEndian, &msgType); err != nil {
		return fmt.Errorf("read type: %w", err)
	}
	if msgType != expectedType {
		return fmt.Errorf("unexpected message type: got %d, want %d", msgType, expectedType)
	}

	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return fmt.Errorf("read length: %w", err)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("read payload: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}
