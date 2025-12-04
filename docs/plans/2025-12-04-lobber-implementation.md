# Lobber Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a tunnel service that exposes local applications to the internet via custom domains with HTTP/2 multiplexing.

**Architecture:** Go monorepo with three components: CLI client, relay server, and web dashboard. CLI establishes HTTP/2 tunnel to relay, relay terminates TLS and routes requests by hostname. PostgreSQL stores accounts, domains, and request logs.

**Tech Stack:** Go 1.22+, PostgreSQL, Let's Encrypt (autocert), Stripe, HTTP/2 (x/net/http2), GCP Cloud Run

---

## Project Structure

```
lobber/
├── cmd/
│   ├── lobber/          # CLI binary
│   │   └── main.go
│   └── relay/           # Relay server binary
│       └── main.go
├── internal/
│   ├── tunnel/          # Shared tunnel protocol
│   │   ├── protocol.go
│   │   └── protocol_test.go
│   ├── client/          # CLI tunnel client
│   │   ├── client.go
│   │   ├── client_test.go
│   │   └── inspector.go
│   ├── relay/           # Relay server
│   │   ├── server.go
│   │   ├── server_test.go
│   │   ├── router.go
│   │   └── tls.go
│   ├── auth/            # OAuth + token handling
│   │   ├── oauth.go
│   │   └── token.go
│   └── db/              # Database access
│       ├── db.go
│       ├── migrations/
│       └── queries/
├── web/                 # Dashboard (later phase)
├── docker/
│   ├── Dockerfile.cli
│   └── Dockerfile.relay
├── go.mod
├── go.sum
└── README.md
```

---

## Phase 1: Foundation (Parallel-Safe)

These tasks have no dependencies and can be worked on by separate agents.

---

### Task 1: Project Initialization

**Files:**
- Create: `go.mod`
- Create: `go.sum`
- Create: `README.md`
- Create: `.gitignore`

**Step 1: Initialize Go module**

```bash
go mod init github.com/lobber-dev/lobber
```

**Step 2: Create .gitignore**

```gitignore
# Binaries
/lobber
/relay
*.exe

# IDE
.idea/
.vscode/
*.swp

# Local config
.lobber/
*.local.yaml

# OS
.DS_Store

# Test coverage
coverage.out
```

**Step 3: Create README**

```markdown
# Lobber

Expose your local apps to the internet with your own domain.

## Quick Start

```bash
# Install
go install github.com/lobber-dev/lobber/cmd/lobber@latest

# Login
lobber login

# Expose your app
lobber up app.mysite.com:3000
```

## Development

```bash
# Run tests
go test ./...

# Build CLI
go build -o lobber ./cmd/lobber

# Build relay
go build -o relay ./cmd/relay
```
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: initialize Go module and project structure"
```

---

### Task 2: Database Schema

**Files:**
- Create: `internal/db/migrations/001_initial.sql`
- Create: `internal/db/db.go`

**Step 1: Write migration**

```sql
-- 001_initial.sql

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    name TEXT,
    stripe_customer_id TEXT,
    plan TEXT NOT NULL DEFAULT 'free', -- 'free', 'pro'
    bandwidth_used_bytes BIGINT NOT NULL DEFAULT 0,
    bandwidth_reset_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Domains table
CREATE TABLE domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hostname TEXT UNIQUE NOT NULL, -- e.g., 'app.mysite.com'
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_domains_hostname ON domains(hostname);
CREATE INDEX idx_domains_user_id ON domains(user_id);

-- API tokens table
CREATE TABLE api_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT UNIQUE NOT NULL, -- bcrypt hash of token
    name TEXT NOT NULL,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Request logs table
CREATE TABLE request_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    request_headers JSONB,
    response_headers JSONB,
    request_body_preview TEXT, -- First 10KB
    response_body_preview TEXT, -- First 10KB
    request_size_bytes INTEGER NOT NULL,
    response_size_bytes INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_request_logs_domain_id ON request_logs(domain_id);
CREATE INDEX idx_request_logs_created_at ON request_logs(created_at);

-- Tunnel sessions table (for uptime monitoring)
CREATE TABLE tunnel_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    disconnect_reason TEXT
);

CREATE INDEX idx_tunnel_sessions_domain_id ON tunnel_sessions(domain_id);
```

**Step 2: Write database connection helper**

```go
// internal/db/db.go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

type DB struct {
	*sql.DB
}

func New(ctx context.Context) (*DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &DB{db}, nil
}

func (d *DB) Close() error {
	return d.DB.Close()
}
```

**Step 3: Add dependency**

```bash
go get github.com/lib/pq
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add database schema and connection helper"
```

---

### Task 3: Tunnel Protocol Definition

**Files:**
- Create: `internal/tunnel/protocol.go`
- Create: `internal/tunnel/protocol_test.go`

**Step 1: Write failing test**

```go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/tunnel/... -v
```

Expected: FAIL (types not defined)

**Step 3: Write implementation**

```go
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
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/tunnel/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add tunnel protocol with request/response encoding"
```

---

## Phase 2: CLI Core

---

### Task 4: CLI Entry Point and Commands

**Files:**
- Create: `cmd/lobber/main.go`
- Create: `internal/cli/commands.go`

**Step 1: Create CLI entry point**

```go
// cmd/lobber/main.go
package main

import (
	"fmt"
	"os"

	"github.com/lobber-dev/lobber/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 2: Create CLI commands**

```go
// internal/cli/commands.go
package cli

import (
	"flag"
	"fmt"
)

func Run(args []string) error {
	if len(args) == 0 {
		return showHelp()
	}

	switch args[0] {
	case "login":
		return runLogin(args[1:])
	case "logout":
		return runLogout(args[1:])
	case "up":
		return runUp(args[1:])
	case "status":
		return runStatus(args[1:])
	case "domains":
		return runDomains(args[1:])
	case "help", "-h", "--help":
		return showHelp()
	case "version", "-v", "--version":
		return showVersion()
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func showHelp() error {
	fmt.Println(`Lobber - Expose your local apps to the internet

Usage:
  lobber <command> [flags]

Commands:
  login       Authenticate with Lobber
  logout      Clear saved credentials
  up          Start a tunnel
  status      Show active tunnels
  domains     List verified domains
  version     Show version

Flags:
  -h, --help     Show help
  -v, --version  Show version

Examples:
  lobber login
  lobber up app.mysite.com:3000
  lobber up app.mysite.com:3000 --inspect`)
	return nil
}

func showVersion() error {
	fmt.Println("lobber version 0.1.0")
	return nil
}

func runLogin(args []string) error {
	// TODO: Implement OAuth flow
	fmt.Println("Opening browser for authentication...")
	return nil
}

func runLogout(args []string) error {
	// TODO: Clear stored credentials
	fmt.Println("Logged out successfully")
	return nil
}

func runUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	token := fs.String("token", "", "API token (for CI/CD)")
	inspect := fs.Bool("inspect", true, "Enable local inspector")
	inspectPort := fs.Int("inspect-port", 4040, "Inspector port")
	noInspect := fs.Bool("no-inspect", false, "Disable local inspector")
	quiet := fs.Bool("quiet", false, "Minimal output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: lobber up <domain>:<port>")
	}

	target := fs.Arg(0)
	_ = token
	_ = inspect
	_ = inspectPort
	_ = noInspect
	_ = quiet

	fmt.Printf("Starting tunnel for %s...\n", target)
	// TODO: Implement tunnel
	return nil
}

func runStatus(args []string) error {
	fmt.Println("No active tunnels")
	return nil
}

func runDomains(args []string) error {
	fmt.Println("No verified domains")
	return nil
}
```

**Step 3: Build and test**

```bash
go build -o lobber ./cmd/lobber
./lobber help
./lobber up app.mysite.com:3000
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add CLI entry point and command structure"
```

---

### Task 5: Config and Token Storage

**Files:**
- Create: `internal/cli/config.go`
- Create: `internal/cli/config_test.go`

**Step 1: Write failing test**

```go
// internal/cli/config_test.go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveLoad(t *testing.T) {
	// Use temp directory
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &Config{
		Token:          "test-token-123",
		DefaultInspect: true,
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Token != cfg.Token {
		t.Errorf("Token = %q, want %q", loaded.Token, cfg.Token)
	}
	if loaded.DefaultInspect != cfg.DefaultInspect {
		t.Errorf("DefaultInspect = %v, want %v", loaded.DefaultInspect, cfg.DefaultInspect)
	}

	// Verify file was created in right place
	configPath := filepath.Join(tmpDir, ".lobber", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file not created")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/... -v -run TestConfigSaveLoad
```

Expected: FAIL

**Step 3: Write implementation**

```go
// internal/cli/config.go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Token          string `yaml:"token,omitempty"`
	DefaultInspect bool   `yaml:"default_inspect,omitempty"`
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".lobber"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func LoadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func ClearConfig() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove config: %w", err)
	}
	return nil
}
```

**Step 4: Add dependency and run test**

```bash
go get gopkg.in/yaml.v3
go test ./internal/cli/... -v -run TestConfigSaveLoad
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add config file storage for CLI credentials"
```

---

### Task 6: Tunnel Client

**Files:**
- Create: `internal/client/client.go`
- Create: `internal/client/client_test.go`

**Step 1: Write failing test**

```go
// internal/client/client_test.go
package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientForwardsRequests(t *testing.T) {
	// Create a local server that will receive forwarded requests
	localHits := make(chan *http.Request, 1)
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localHits <- r
		w.Header().Set("X-Local", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer localServer.Close()

	// Create client
	client := &Client{
		LocalAddr:   localServer.URL,
		RelayAddr:   "wss://tunnel.lobber.dev", // Not used in unit test
		Token:       "test-token",
		Domain:      "app.mysite.com",
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/client/... -v
```

Expected: FAIL

**Step 3: Write implementation**

```go
// internal/client/client.go
package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	LocalAddr   string
	RelayAddr   string
	Token       string
	Domain      string
	InspectPort int

	httpClient *http.Client
}

func New(localAddr, relayAddr, token, domain string) *Client {
	return &Client{
		LocalAddr: localAddr,
		RelayAddr: relayAddr,
		Token:     token,
		Domain:    domain,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ForwardToLocal forwards an incoming request to the local server
func (c *Client) ForwardToLocal(req *http.Request) (*http.Response, error) {
	// Build the local URL
	localURL, err := url.Parse(c.LocalAddr)
	if err != nil {
		return nil, fmt.Errorf("parse local addr: %w", err)
	}

	// Create a new request to the local server
	localURL.Path = req.URL.Path
	localURL.RawQuery = req.URL.RawQuery

	localReq, err := http.NewRequestWithContext(req.Context(), req.Method, localURL.String(), req.Body)
	if err != nil {
		return nil, fmt.Errorf("create local request: %w", err)
	}

	// Copy headers
	for k, v := range req.Header {
		localReq.Header[k] = v
	}

	// Send to local server
	resp, err := c.httpClient.Do(localReq)
	if err != nil {
		return nil, fmt.Errorf("local request: %w", err)
	}

	return resp, nil
}

// Connect establishes HTTP/2 tunnel to relay server
func (c *Client) Connect(ctx context.Context) error {
	// TODO: Implement HTTP/2 tunnel connection
	return fmt.Errorf("not implemented")
}

// Run starts the tunnel and processes incoming requests
func (c *Client) Run(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// TODO: Read requests from tunnel, forward to local, send responses back
	<-ctx.Done()
	return ctx.Err()
}

// ReadResponse reads the full response body
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/client/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add tunnel client with local forwarding"
```

---

## Phase 3: Relay Server

---

### Task 7: Relay Server HTTP Handler

**Files:**
- Create: `internal/relay/server.go`
- Create: `internal/relay/server_test.go`

**Step 1: Write failing test**

```go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/relay/... -v
```

Expected: FAIL

**Step 3: Write implementation**

```go
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
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/relay/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add relay server with routing and health check"
```

---

### Task 8: TLS Certificate Management

**Files:**
- Create: `internal/relay/tls.go`
- Create: `internal/relay/tls_test.go`

**Step 1: Write failing test**

```go
// internal/relay/tls_test.go
package relay

import (
	"testing"
)

func TestHostPolicy(t *testing.T) {
	mgr := &TLSManager{
		AllowedDomains: map[string]bool{
			"app.mysite.com": true,
		},
	}

	tests := []struct {
		host    string
		wantErr bool
	}{
		{"app.mysite.com", false},
		{"unknown.com", true},
		{"tunnel.lobber.dev", false}, // Always allow service domain
	}

	for _, tt := range tests {
		err := mgr.HostPolicy(nil, tt.host)
		if (err != nil) != tt.wantErr {
			t.Errorf("HostPolicy(%q) error = %v, wantErr %v", tt.host, err, tt.wantErr)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/relay/... -v -run TestHostPolicy
```

Expected: FAIL

**Step 3: Write implementation**

```go
// internal/relay/tls.go
package relay

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	"golang.org/x/crypto/acme/autocert"
)

type TLSManager struct {
	mu             sync.RWMutex
	AllowedDomains map[string]bool
	ServiceDomain  string
	certManager    *autocert.Manager
}

func NewTLSManager(serviceDomain, cacheDir string) *TLSManager {
	mgr := &TLSManager{
		AllowedDomains: make(map[string]bool),
		ServiceDomain:  serviceDomain,
	}

	mgr.certManager = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: mgr.HostPolicy,
		Cache:      autocert.DirCache(cacheDir),
	}

	return mgr
}

func (m *TLSManager) HostPolicy(ctx context.Context, host string) error {
	// Always allow service domain
	if host == m.ServiceDomain || host == "tunnel.lobber.dev" {
		return nil
	}

	m.mu.RLock()
	allowed := m.AllowedDomains[host]
	m.mu.RUnlock()

	if !allowed {
		return fmt.Errorf("host %q not allowed", host)
	}

	return nil
}

func (m *TLSManager) AddDomain(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AllowedDomains[domain] = true
}

func (m *TLSManager) RemoveDomain(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.AllowedDomains, domain)
}

func (m *TLSManager) TLSConfig() *tls.Config {
	return m.certManager.TLSConfig()
}

func (m *TLSManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.certManager.GetCertificate(hello)
}
```

**Step 4: Add dependency and run test**

```bash
go get golang.org/x/crypto/acme/autocert
go test ./internal/relay/... -v -run TestHostPolicy
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add TLS certificate management with Let's Encrypt"
```

---

### Task 9: Relay Server Entry Point

**Files:**
- Create: `cmd/relay/main.go`

**Step 1: Create relay entry point**

```go
// cmd/relay/main.go
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lobber-dev/lobber/internal/db"
	"github.com/lobber-dev/lobber/internal/relay"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("error: %v", err)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Connect to database
	database, err := db.New(ctx)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()

	// Create server
	server := relay.NewServer(database)

	// Set up TLS
	serviceDomain := os.Getenv("SERVICE_DOMAIN")
	if serviceDomain == "" {
		serviceDomain = "tunnel.lobber.dev"
	}

	cacheDir := os.Getenv("CERT_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = "/var/cache/lobber/certs"
	}

	tlsMgr := relay.NewTLSManager(serviceDomain, cacheDir)

	// HTTP server for ACME challenges
	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = ":80"
	}

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: tlsMgr.HTTPHandler(server),
	}

	// HTTPS server
	httpsAddr := os.Getenv("HTTPS_ADDR")
	if httpsAddr == "" {
		httpsAddr = ":443"
	}

	httpsServer := &http.Server{
		Addr:    httpsAddr,
		Handler: server,
		TLSConfig: &tls.Config{
			GetCertificate: tlsMgr.GetCertificate,
			NextProtos:     []string{"h2", "http/1.1"},
		},
	}

	// Start servers
	errCh := make(chan error, 2)

	go func() {
		log.Printf("HTTP server listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()

	go func() {
		log.Printf("HTTPS server listening on %s", httpsAddr)
		if err := httpsServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("https: %w", err)
		}
	}()

	// Wait for shutdown
	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	if err := httpsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTPS shutdown error: %v", err)
	}

	return nil
}
```

**Step 2: Add HTTPHandler to TLS manager**

Add to `internal/relay/tls.go`:

```go
func (m *TLSManager) HTTPHandler(fallback http.Handler) http.Handler {
	return m.certManager.HTTPHandler(fallback)
}
```

**Step 3: Build relay**

```bash
go build -o relay ./cmd/relay
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add relay server entry point with TLS support"
```

---

## Phase 4: Docker & Deployment

---

### Task 10: Dockerfiles

**Files:**
- Create: `docker/Dockerfile.cli`
- Create: `docker/Dockerfile.relay`
- Create: `docker-compose.yml`

**Step 1: Create CLI Dockerfile**

```dockerfile
# docker/Dockerfile.cli
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /lobber ./cmd/lobber

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /lobber /usr/local/bin/lobber
ENTRYPOINT ["lobber"]
```

**Step 2: Create relay Dockerfile**

```dockerfile
# docker/Dockerfile.relay
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /relay ./cmd/relay

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /relay /usr/local/bin/relay

EXPOSE 80 443
ENTRYPOINT ["relay"]
```

**Step 3: Create docker-compose.yml**

```yaml
# docker-compose.yml
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: lobber
      POSTGRES_PASSWORD: lobber
      POSTGRES_DB: lobber
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./internal/db/migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U lobber"]
      interval: 5s
      timeout: 5s
      retries: 5

  relay:
    build:
      context: .
      dockerfile: docker/Dockerfile.relay
    environment:
      DATABASE_URL: postgres://lobber:lobber@postgres:5432/lobber?sslmode=disable
      SERVICE_DOMAIN: tunnel.localhost
      CERT_CACHE_DIR: /var/cache/lobber/certs
      HTTP_ADDR: ":80"
      HTTPS_ADDR: ":443"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - cert_cache:/var/cache/lobber/certs
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  postgres_data:
  cert_cache:
```

**Step 4: Build and test**

```bash
docker-compose build
docker-compose up -d postgres
docker-compose up relay
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add Dockerfiles and docker-compose configuration"
```

---

## Phase 5: Local Inspector (Parallel-Safe)

---

### Task 11: Local Inspector HTTP Server

**Files:**
- Create: `internal/client/inspector.go`
- Create: `internal/client/inspector_test.go`
- Create: `internal/client/static/` (embedded HTML/JS)

**Step 1: Write failing test**

```go
// internal/client/inspector_test.go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/client/... -v -run TestInspectorReturnsRequests
```

Expected: FAIL

**Step 3: Write implementation**

```go
// internal/client/inspector.go
package client

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

type InspectedRequest struct {
	ID              string              `json:"id"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	StatusCode      int                 `json:"status_code"`
	RequestHeaders  map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	RequestBody     string              `json:"request_body,omitempty"`
	ResponseBody    string              `json:"response_body,omitempty"`
	DurationMs      int64               `json:"duration_ms"`
	Timestamp       time.Time           `json:"timestamp"`
}

type Inspector struct {
	mu       sync.RWMutex
	requests []*InspectedRequest
	maxSize  int
	mux      *http.ServeMux
}

func NewInspector() *Inspector {
	i := &Inspector{
		requests: make([]*InspectedRequest, 0, 100),
		maxSize:  100,
		mux:      http.NewServeMux(),
	}

	// API routes
	i.mux.HandleFunc("/api/requests", i.handleListRequests)
	i.mux.HandleFunc("/api/requests/", i.handleGetRequest)
	i.mux.HandleFunc("/api/replay/", i.handleReplay)

	// Static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	i.mux.Handle("/", http.FileServer(http.FS(staticFS)))

	return i
}

func (i *Inspector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.mux.ServeHTTP(w, r)
}

func (i *Inspector) AddRequest(req *InspectedRequest) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if req.Timestamp.IsZero() {
		req.Timestamp = time.Now()
	}

	i.requests = append([]*InspectedRequest{req}, i.requests...)

	if len(i.requests) > i.maxSize {
		i.requests = i.requests[:i.maxSize]
	}
}

func (i *Inspector) handleListRequests(w http.ResponseWriter, r *http.Request) {
	i.mu.RLock()
	requests := make([]*InspectedRequest, len(i.requests))
	copy(requests, i.requests)
	i.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(requests)
}

func (i *Inspector) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	// TODO: Get single request by ID
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (i *Inspector) handleReplay(w http.ResponseWriter, r *http.Request) {
	// TODO: Replay request
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
```

**Step 4: Create minimal static files**

```bash
mkdir -p internal/client/static
```

```html
<!-- internal/client/static/index.html -->
<!DOCTYPE html>
<html>
<head>
    <title>Lobber Inspector</title>
    <style>
        body { font-family: system-ui, sans-serif; margin: 0; padding: 20px; background: #1a1a2e; color: #eee; }
        h1 { color: #00d9ff; }
        .request { background: #16213e; padding: 15px; margin: 10px 0; border-radius: 8px; cursor: pointer; }
        .request:hover { background: #1f3460; }
        .method { font-weight: bold; color: #00d9ff; }
        .path { color: #fff; }
        .status { float: right; }
        .status.ok { color: #4ade80; }
        .status.error { color: #f87171; }
    </style>
</head>
<body>
    <h1>Lobber Inspector</h1>
    <div id="requests"></div>
    <script>
        async function loadRequests() {
            const resp = await fetch('/api/requests');
            const requests = await resp.json();
            const container = document.getElementById('requests');
            container.innerHTML = requests.map(r => `
                <div class="request">
                    <span class="method">${r.method}</span>
                    <span class="path">${r.path}</span>
                    <span class="status ${r.status_code < 400 ? 'ok' : 'error'}">${r.status_code}</span>
                </div>
            `).join('');
        }
        loadRequests();
        setInterval(loadRequests, 1000);
    </script>
</body>
</html>
```

**Step 5: Run test**

```bash
go test ./internal/client/... -v -run TestInspectorReturnsRequests
```

Expected: PASS

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add local inspector with request viewing"
```

---

## Summary: Parallel Work Assignment

The following tasks can be worked on in parallel by different agents:

**Agent A: CLI + Tunnel Client**
- Task 4: CLI Entry Point
- Task 5: Config and Token Storage
- Task 6: Tunnel Client
- Task 11: Local Inspector

**Agent B: Relay Server**
- Task 7: Relay Server HTTP Handler
- Task 8: TLS Certificate Management
- Task 9: Relay Server Entry Point

**Agent C: Foundation + Infrastructure**
- Task 1: Project Initialization
- Task 2: Database Schema
- Task 3: Tunnel Protocol
- Task 10: Dockerfiles

After Phase 1-3 complete, integration work requires coordination.
