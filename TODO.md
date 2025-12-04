# Lobber - Project Status & Next Tasks

## Project Overview

**Lobber** - Tunnel service for exposing local apps to the internet via custom domains.

| Attribute | Value |
|-----------|-------|
| Domain | lobber.dev |
| Tech Stack | Go, PostgreSQL, GCP Cloud Run, Stripe |
| Tunnel Protocol | HTTP/2 with binary framing |
| Pricing | Free 5GB, PAYG $0.10/GB, Pro $15/mo |

## Completed Work (Day 1)

### Foundation
- [x] Go module: `github.com/lobber-dev/lobber`
- [x] Database schema (users, domains, api_tokens, request_logs, tunnel_sessions)
- [x] Tunnel protocol with binary framing
- [x] Docker infrastructure (CLI + Relay Dockerfiles, docker-compose)

### CLI
- [x] Entry point with commands: login, logout, up, status, domains, help, version
- [x] Config storage in `~/.lobber/config.yaml`
- [x] Tunnel client with local request forwarding
- [x] Local inspector on :4040 with web UI

### Relay Server
- [x] HTTP handler with health check and tunnel routing
- [x] TLS certificate management with Let's Encrypt
- [x] Server entry point with HTTP/HTTPS support

### Tests
All passing:
- `internal/cli` - config save/load
- `internal/client` - forwarding + inspector
- `internal/relay` - health, routing, TLS policy
- `internal/tunnel` - protocol encoding

## File Structure

```
lobber/
├── cmd/
│   ├── lobber/main.go          # CLI entry point
│   └── relay/main.go           # Relay server entry point
├── internal/
│   ├── cli/
│   │   ├── commands.go         # CLI command handlers
│   │   ├── commands_test.go
│   │   ├── config.go           # ~/.lobber/config.yaml
│   │   └── config_test.go
│   ├── client/
│   │   ├── client.go           # Tunnel client
│   │   ├── client_test.go
│   │   ├── inspector.go        # Local :4040 dashboard
│   │   ├── inspector_test.go
│   │   └── static/index.html   # Inspector web UI
│   ├── relay/
│   │   ├── server.go           # HTTP handler + routing
│   │   ├── server_test.go
│   │   ├── tls.go              # Let's Encrypt autocert
│   │   └── tls_test.go
│   ├── tunnel/
│   │   ├── protocol.go         # Binary framing protocol
│   │   └── protocol_test.go
│   └── db/
│       ├── db.go               # PostgreSQL connection
│       └── migrations/
│           └── 001_initial.sql # Schema
├── docker/
│   ├── Dockerfile.cli
│   └── Dockerfile.relay
├── docker-compose.yml
├── go.mod
├── go.sum
├── README.md
└── docs/plans/
    ├── 2025-12-04-lobber-design.md
    └── 2025-12-04-lobber-implementation.md
```

## Git Log (10 commits)

```
f28ecff feat: update Dockerfiles to use Go 1.24 for compatibility
346ccae feat: add local inspector with request viewing
315a798 feat: add TLS certificate management with Let's Encrypt
69d11d9 feat: add tunnel client with local forwarding
1b0f8a9 feat: add config file storage for CLI credentials
e3863bb feat: add tunnel protocol with request/response encoding
70c708a feat: add database schema and connection helper
f2ef2af feat: initialize Go module and project structure
6b1466f Add detailed implementation plan for Lobber
83489b8 Add Lobber design document
```

---

## Day 2: Integration Phase

### Priority 1: Core Tunnel Functionality (CRITICAL PATH)

This is the most important work - making `lobber up` actually work end-to-end.

#### Task 1.1: HTTP/2 Tunnel Handshake

**Files to modify:**
- `internal/client/client.go` - Add `Connect()` implementation
- `internal/relay/server.go` - Add `handleConnect()` implementation

**Client side (`internal/client/client.go`):**

```go
func (c *Client) Connect(ctx context.Context) error {
    // 1. Create HTTP/2 transport
    transport := &http2.Transport{
        AllowHTTP: false,
        TLSClientConfig: &tls.Config{},
    }

    // 2. Connect to relay's /_lobber/connect endpoint
    req, _ := http.NewRequestWithContext(ctx, "POST", c.RelayAddr+"/_lobber/connect", nil)
    req.Header.Set("Authorization", "Bearer "+c.Token)
    req.Header.Set("X-Lobber-Domain", c.Domain)

    // 3. Get HTTP/2 client conn for multiplexing
    conn, err := transport.NewClientConn(/* ... */)

    // 4. Open stream for tunnel communication
    c.tunnelStream = conn.NewStream()

    return nil
}
```

**Relay side (`internal/relay/server.go`):**

```go
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
    // 1. Validate auth token
    token := r.Header.Get("Authorization")
    userID, err := s.validateToken(token)
    if err != nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    // 2. Get claimed domain
    domain := r.Header.Get("X-Lobber-Domain")

    // 3. Verify domain ownership or create new
    if err := s.verifyDomainOwnership(userID, domain); err != nil {
        http.Error(w, "domain not allowed", http.StatusForbidden)
        return
    }

    // 4. Upgrade to HTTP/2 bidirectional stream
    // 5. Register tunnel in map
    s.RegisterTunnel(&Tunnel{
        Domain: domain,
        UserID: userID,
        Stream: /* HTTP/2 stream */,
    })

    // 6. Keep connection open, handle requests
}
```

#### Task 1.2: Request Multiplexing

**Files to modify:**
- `internal/relay/server.go` - Update `handleProxy()`
- `internal/client/client.go` - Add `Run()` loop

**Flow:**
```
Internet Request → Relay → Encode (tunnel/protocol.go) → HTTP/2 Stream → Client
                                                                           ↓
                                                                    Decode Request
                                                                           ↓
                                                                    Forward to localhost
                                                                           ↓
                                                                    Encode Response
                                                                           ↓
Internet Response ← Relay ← Decode ← HTTP/2 Stream ←←←←←←←←←←←←←←←←←←←←←←←
```

**Relay proxy handler:**

```go
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
    hostname := r.Host

    s.mu.RLock()
    tunnel, ok := s.tunnels[hostname]
    s.mu.RUnlock()

    if !ok {
        http.Error(w, "tunnel not found", http.StatusBadGateway)
        return
    }

    // Read request body
    body, _ := io.ReadAll(r.Body)

    // Create tunnel request
    tunnelReq := &tunnel.Request{
        ID:      uuid.New().String(),
        Method:  r.Method,
        Path:    r.URL.RequestURI(),
        Headers: r.Header,
        Body:    body,
    }

    // Send through tunnel
    if err := tunnel.EncodeRequest(tunnel.Stream, tunnelReq); err != nil {
        http.Error(w, "tunnel error", http.StatusBadGateway)
        return
    }

    // Wait for response
    tunnelResp, err := tunnel.DecodeResponse(tunnel.Stream)
    if err != nil {
        http.Error(w, "tunnel error", http.StatusBadGateway)
        return
    }

    // Write response
    for k, v := range tunnelResp.Headers {
        w.Header()[k] = v
    }
    w.WriteHeader(tunnelResp.StatusCode)
    w.Write(tunnelResp.Body)
}
```

#### Task 1.3: End-to-End Integration Test

**New file:** `integration_test.go`

```go
func TestEndToEndTunnel(t *testing.T) {
    // 1. Start a local HTTP server
    localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("hello from local"))
    }))
    defer localServer.Close()

    // 2. Start relay (in-memory, no real TLS)
    relay := relay.NewServer(nil)
    relayServer := httptest.NewServer(relay)
    defer relayServer.Close()

    // 3. Create and connect client
    client := client.New(localServer.URL, relayServer.URL, "test-token", "test.example.com")
    go client.Run(context.Background())

    // 4. Wait for tunnel registration
    time.Sleep(100 * time.Millisecond)

    // 5. Make request to relay with Host header
    req, _ := http.NewRequest("GET", relayServer.URL+"/api/test", nil)
    req.Host = "test.example.com"

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)

    body, _ := io.ReadAll(resp.Body)
    assert.Equal(t, "hello from local", string(body))
}
```

---

### Priority 2: Authentication

#### Task 2.1: OAuth Flow

**New file:** `internal/auth/oauth.go`

```go
package auth

import (
    "golang.org/x/oauth2"
    "golang.org/x/oauth2/github"
)

var githubConfig = &oauth2.Config{
    ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
    ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
    Scopes:       []string{"user:email"},
    Endpoint:     github.Endpoint,
}

// StartOAuthFlow opens browser and starts local callback server
func StartOAuthFlow() (string, error) {
    // 1. Generate state token
    state := generateRandomState()

    // 2. Start local HTTP server on random port for callback
    listener, _ := net.Listen("tcp", "127.0.0.1:0")
    port := listener.Addr().(*net.TCPAddr).Port

    // 3. Set redirect URL
    githubConfig.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", port)

    // 4. Open browser to auth URL
    authURL := githubConfig.AuthCodeURL(state)
    browser.OpenURL(authURL)

    // 5. Wait for callback
    var authCode string
    http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authCode = r.URL.Query().Get("code")
        w.Write([]byte("Login successful! You can close this window."))
        listener.Close()
    }))

    // 6. Exchange code for token
    token, err := githubConfig.Exchange(context.Background(), authCode)
    if err != nil {
        return "", err
    }

    return token.AccessToken, nil
}
```

**Update:** `internal/cli/commands.go:runLogin()`

```go
func runLogin(args []string) error {
    fmt.Println("Opening browser for authentication...")

    token, err := auth.StartOAuthFlow()
    if err != nil {
        return fmt.Errorf("login failed: %w", err)
    }

    // Save token
    cfg, _ := LoadConfig()
    cfg.Token = token
    if err := SaveConfig(cfg); err != nil {
        return fmt.Errorf("save config: %w", err)
    }

    fmt.Println("Login successful!")
    return nil
}
```

#### Task 2.2: API Token Validation

**New file:** `internal/auth/token.go`

```go
package auth

import (
    "crypto/rand"
    "encoding/hex"

    "golang.org/x/crypto/bcrypt"
)

// GenerateAPIToken creates a new API token
func GenerateAPIToken() (plaintext, hash string, err error) {
    // Generate 32 random bytes
    bytes := make([]byte, 32)
    rand.Read(bytes)
    plaintext = "lb_" + hex.EncodeToString(bytes)

    // Hash for storage
    hashBytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
    if err != nil {
        return "", "", err
    }
    hash = string(hashBytes)

    return plaintext, hash, nil
}

// ValidateAPIToken checks if a token matches a hash
func ValidateAPIToken(plaintext, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
    return err == nil
}
```

---

### Priority 3: Domain Verification

#### Task 3.1: CNAME Verification

**New file:** `internal/relay/domain.go`

```go
package relay

import (
    "fmt"
    "net"
    "strings"
)

const ServiceDomain = "tunnel.lobber.dev"

// VerifyCNAME checks if domain has correct CNAME record
func VerifyCNAME(domain string) error {
    cname, err := net.LookupCNAME(domain)
    if err != nil {
        return fmt.Errorf("DNS lookup failed: %w", err)
    }

    // Remove trailing dot
    cname = strings.TrimSuffix(cname, ".")

    if cname != ServiceDomain {
        return fmt.Errorf("CNAME points to %s, expected %s", cname, ServiceDomain)
    }

    return nil
}
```

---

### Priority 4: Billing (Stripe)

#### Task 4.1: Stripe Integration

**New file:** `internal/billing/stripe.go`

```go
package billing

import (
    "github.com/stripe/stripe-go/v76"
    "github.com/stripe/stripe-go/v76/customer"
    "github.com/stripe/stripe-go/v76/usagerecord"
)

func init() {
    stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
}

// CreateCustomer creates a Stripe customer for a new user
func CreateCustomer(email, name string) (string, error) {
    params := &stripe.CustomerParams{
        Email: stripe.String(email),
        Name:  stripe.String(name),
    }
    c, err := customer.New(params)
    if err != nil {
        return "", err
    }
    return c.ID, nil
}

// ReportUsage reports bandwidth usage to Stripe
func ReportUsage(subscriptionItemID string, bytes int64) error {
    // Convert bytes to GB (Stripe uses quantity)
    gbUsed := bytes / (1024 * 1024 * 1024)
    if gbUsed == 0 {
        gbUsed = 1 // Minimum 1 GB
    }

    params := &stripe.UsageRecordParams{
        SubscriptionItem: stripe.String(subscriptionItemID),
        Quantity:         stripe.Int64(gbUsed),
        Action:          stripe.String("increment"),
    }
    _, err := usagerecord.New(params)
    return err
}
```

---

### Priority 5: Web Dashboard

**Tech recommendation:** Go templates with htmx for simplicity (no build step, same language as backend).

**New files:**
- `web/templates/layout.html`
- `web/templates/account.html`
- `web/templates/domains.html`
- `web/templates/logs.html`
- `web/handler.go`

---

## Agent Coordination (for parallel work)

**Previous agents:**
- **BrownMountain** - CLI + Client
- **RedDog** - Relay Server
- **ChartreuseDog** - Foundation

**Coordinator:** WhiteSnow

**Suggested parallel assignment:**
- Agent A: Priority 1 (tunnel integration) - CRITICAL PATH
- Agent B: Priority 2 (auth) - can work in parallel
- Agent C: Priority 3+4 (domain + billing) - start after auth basics done

**Agent-mail commands:**
```
mcp__mcp-agent-mail__register_agent
mcp__mcp-agent-mail__fetch_inbox
mcp__mcp-agent-mail__send_message
```

---

## Quick Commands

```bash
# Run all tests
go test ./...

# Build CLI
go build -o lobber ./cmd/lobber

# Build relay
go build -o relay ./cmd/relay

# Start dev database
docker-compose up -d postgres

# Run relay locally
DATABASE_URL="postgres://lobber:lobber@localhost:5432/lobber?sslmode=disable" ./relay

# Test CLI
./lobber help
./lobber up test.example.com:3000
```

---

## Resume Instructions

1. Read this file for full context
2. Read `docs/plans/2025-12-04-lobber-design.md` for product vision
3. Check agent-mail inbox if available: `mcp__mcp-agent-mail__fetch_inbox`
4. Start with Priority 1: HTTP/2 tunnel connection
5. Use TDD: write test first, verify fail, implement, verify pass, commit
