# Lobber - Design Document

**Date:** 2025-12-04
**Status:** Approved
**Domain:** lobber.dev

## Overview

Lobber is a tunnel service that exposes local applications to the internet via custom domains.

**Core value prop:**
- Bring your own domain (no DNS provider lock-in)
- Single command: `lobber up app.mysite.com:3000`
- Usage-based pricing that's transparent and sustainable

**How it works:**
1. User adds CNAME: `app.mysite.com → tunnel.lobber.dev`
2. User runs `lobber up app.mysite.com:3000`
3. Lobber provisions TLS via Let's Encrypt
4. HTTP/2 tunnel established between CLI and relay server
5. Requests to `app.mysite.com` are proxied through tunnel to `localhost:3000`

**Target users:**
- Developers exposing local apps for testing/demos
- Teams needing consistent staging URLs
- Webhook development (Stripe, GitHub, etc.)
- Docker-compose deployments needing public URLs

**Domain persistence:**
- CNAME stays in user's DNS permanently
- Domain linked to user account on first use
- Reconnect anytime with same domain - instant resume

## Technical Architecture

```
┌─────────────┐         ┌─────────────────┐         ┌─────────────┐
│  lobber CLI │◄──H2───►│  Relay Server   │◄──TLS──►│   Internet  │
│  (user's    │         │  (lobber.dev)   │         │   (users)   │
│   machine)  │         │                 │         │             │
└─────────────┘         └─────────────────┘         └─────────────┘
      │                        │
      ▼                        ▼
 localhost:3000          PostgreSQL
                         (accounts, domains, logs)
```

**Relay server responsibilities:**
- Accept HTTP/2 tunnel connections from CLIs
- Terminate TLS for custom domains (Let's Encrypt)
- Route incoming requests to correct tunnel by hostname
- Meter bandwidth for billing
- Store request logs for inspector/replay

**CLI responsibilities:**
- Authenticate user (browser OAuth)
- Establish HTTP/2 tunnel to relay
- Receive multiplexed requests, forward to local port
- Send responses back through tunnel
- Run local inspector dashboard on :4040

**Tech stack:**
- Go for both CLI and server
- PostgreSQL for persistence
- Let's Encrypt with autocert
- Stripe for billing
- HTTP/2 for tunnel protocol (TURN-style with multiplexing)

## Premium Features

### Request Inspector
- All requests/responses logged to PostgreSQL
- Web dashboard shows live stream of requests
- Filter by status code, path, method
- View headers, body (truncated if large)
- Free tier: last 100 requests, in-memory only
- Pro tier: persisted, searchable, 30-day retention

### Webhook Replay
- One-click replay any logged request
- Useful for debugging failed webhooks
- Replays from relay server, not original source
- Pro tier only

### Persisted Logs
- Free: 24-hour retention, 1000 requests max
- Pro: 30-day retention, unlimited requests
- Export as JSON/CSV from dashboard

### Uptime Monitoring
- Relay server pings CLI every 30 seconds
- If tunnel disconnects, record downtime
- Dashboard shows uptime percentage
- Optional webhook/email alerts on disconnect
- Pro tier only

**Storage implications:**
- Request logs: ~1KB per request (headers + truncated body)
- At 10K requests/day = ~10MB/day = ~300MB/month per active user
- Pro users only, so manageable

## Pricing & Billing

**Account requirement:**
- Credit card required at signup (Stripe)
- Prevents abuse, enables seamless billing

### Free Tier
- 5GB bandwidth/month
- 24-hour log retention
- 100 requests in inspector
- No webhook replay
- No uptime monitoring

### Pay-as-you-go (after free tier)
- $0.10 per GB
- Billed monthly in arrears

### Pro Tier ($15/month)
- 50GB bandwidth included
- $0.08/GB overage (20% discount)
- 30-day log retention
- Unlimited inspector history
- Webhook replay
- Uptime monitoring + alerts

**Billing mechanics:**
- Stripe Subscriptions for Pro
- Stripe Usage Records for bandwidth metering
- Relay server reports bandwidth per-tunnel
- Daily aggregation job updates Stripe

**Example costs:**
| Usage | Free | PAYG | Pro |
|-------|------|------|-----|
| 3GB/mo | $0 | $0 | $15 |
| 10GB/mo | $0.50 | $0.50 | $15 |
| 60GB/mo | $5.50 | $5.50 | $15.80 |
| 200GB/mo | $19.50 | $19.50 | $27 |

## CLI & Docker Experience

### Installation
```bash
# macOS
brew install lobber/tap/lobber

# Linux
curl -fsSL https://lobber.dev/install.sh | sh

# Go
go install lobber.dev/cli@latest
```

### CLI Commands
```bash
lobber login              # Browser OAuth, saves token locally
lobber up <domain>:<port> # Start tunnel
lobber status             # Show active tunnels
lobber domains            # List verified domains
lobber logs               # Tail request logs
lobber logout             # Clear saved credentials
```

### Flags
```bash
lobber up app.mysite.com:3000 \
  --token <api-token>     # For CI/CD (skip interactive login)
  --inspect               # Open inspector in browser
  --inspect=4050          # Custom inspector port
  --no-inspect            # Disable local dashboard
  --quiet                 # Minimal output
```

### Local Inspector Dashboard
```bash
$ lobber up app.mysite.com:3000
→ Live: https://app.mysite.com → localhost:3000
→ Inspector: http://localhost:4040
```

Features:
- Real-time request/response stream
- Click to expand full headers + body
- Replay button (sends request again)
- Filter by status, method, path
- Works offline (no account needed for local view)

### Docker Image
```yaml
services:
  app:
    image: my-app:latest
    ports:
      - "3000:3000"

  tunnel:
    image: lobber/lobber:latest
    command: up app.mysite.com:3000
    environment:
      - LOBBER_TOKEN=${LOBBER_TOKEN}
    depends_on:
      - app
    network_mode: "service:app"
```

### Config File (~/.lobber/config.yaml)
```yaml
token: xxx
default_inspect: true
```

## Infrastructure & Deployment

### GCP Architecture (Single Region)
```
┌─────────────────────────────────────────────────────┐
│                    GCP (us-central1)                │
│  ┌─────────────────┐    ┌─────────────────────────┐ │
│  │  Cloud Run      │    │  Cloud Run              │ │
│  │  (Relay Server) │    │  (Web Dashboard)        │ │
│  └────────┬────────┘    └───────────┬─────────────┘ │
│           │                         │               │
│           ▼                         ▼               │
│  ┌─────────────────────────────────────────────────┐│
│  │         Cloud SQL (PostgreSQL)                  ││
│  └─────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────┘
```

### GCP Services
- **Cloud Run** - Relay + Dashboard (scales to zero, pay per use)
- **Cloud SQL** - Managed PostgreSQL
- **Cloud Load Balancer** - TLS termination, custom domains
- **Secret Manager** - API keys, Stripe secrets
- **Cloud Storage** - (optional) Large request body storage

### DNS Setup
- `lobber.dev` → Web dashboard
- `tunnel.lobber.dev` → Relay server
- Users CNAME `app.theirs.com → tunnel.lobber.dev`

### TLS
- Let's Encrypt with `autocert` package
- Challenge: TLS-ALPN-01 (no HTTP needed)
- Certs cached in PostgreSQL or filesystem

### Estimated GCP Costs (Early Stage)
- Cloud Run: ~$5-20/month (scales with traffic)
- Cloud SQL: ~$10/month (db-f1-micro)
- Load Balancer: ~$18/month
- Bandwidth: $0.12/GB egress
- **Total: ~$35-60/month baseline**

## Future Enhancements (Not in MVP)
- Multi-region edge nodes (premium feature)
- QUIC/HTTP3 transport upgrade
- Team collaboration features
- Custom authentication layers (OAuth, IP allowlist)
- Rate limiting at the edge
- Wildcard subdomain support
