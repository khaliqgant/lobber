# Lobber - Next Tasks

> Resume instructions: Run `lobber up` in this directory and check agent-mail inbox for full context.

## Day 2: Integration Phase

### Priority 1: Core Tunnel Functionality (Critical Path)
- [ ] **Implement HTTP/2 tunnel handshake** - `internal/client/client.go:Connect()`
  - Client connects to `/_lobber/connect` on relay
  - Upgrade to HTTP/2 stream
  - Exchange auth token and domain claim
- [ ] **Implement relay tunnel handler** - `internal/relay/server.go:handleConnect()`
  - Validate auth token
  - Verify domain ownership (or create if new)
  - Register tunnel in memory map
- [ ] **Request multiplexing** - Forward incoming requests through tunnel
  - Relay encodes request using `internal/tunnel/protocol.go`
  - Client decodes, forwards to local, encodes response
  - Relay decodes response, sends to original requester
- [ ] **End-to-end integration test**
  - Start relay locally
  - Run `lobber up localhost:8080`
  - Verify requests flow through

### Priority 2: Authentication
- [ ] **OAuth flow** - `internal/auth/oauth.go`
  - GitHub OAuth app setup
  - Browser redirect flow in CLI
  - Token exchange and storage
- [ ] **API token generation** - For CI/CD use
  - Generate in dashboard
  - Accept via `--token` flag

### Priority 3: Domain Verification
- [ ] **CNAME checking** - `internal/relay/domain.go`
  - DNS lookup for CNAME record
  - Verify points to tunnel.lobber.dev
- [ ] **Domain persistence** - Store verified domains in DB
- [ ] **TLS provisioning** - Auto-provision on first verified connect

### Priority 4: Billing (Stripe)
- [ ] **Customer creation** - On signup
- [ ] **Bandwidth metering** - Track bytes per tunnel
- [ ] **Usage records** - Report to Stripe daily
- [ ] **Subscription handling** - Pro tier logic

### Priority 5: Web Dashboard
- [ ] **Tech choice** - Next.js or Go templates?
- [ ] **Account page** - Email, plan, usage
- [ ] **Domains page** - List, verify status
- [ ] **Logs page** - Request inspector (Pro)
- [ ] **Billing page** - Usage, invoices

## Agent Coordination

Previous agents:
- **BrownMountain** - CLI + Client
- **RedDog** - Relay Server
- **ChartreuseDog** - Foundation

Coordinator: **WhiteSnow**

Use agent-mail to check inbox and send updates:
```
mcp__mcp-agent-mail__fetch_inbox
mcp__mcp-agent-mail__send_message
```

## Quick Commands

```bash
# Run tests
go test ./...

# Build CLI
go build -o lobber ./cmd/lobber

# Build relay
go build -o relay ./cmd/relay

# Start local dev environment
docker-compose up -d postgres
DATABASE_URL="postgres://lobber:lobber@localhost:5432/lobber?sslmode=disable" ./relay
```
