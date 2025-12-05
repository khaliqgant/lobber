# Lobber

Expose localhost to the internet with your own domain. No random subdomains.

## How it works

1. Add a CNAME record: `app.yourdomain.com → tunnel.lobber.dev`
2. Run: `lobber up app.yourdomain.com:3000`
3. Your local app is live at `https://app.yourdomain.com`

## Install

```bash
go install github.com/lobber-dev/lobber/cmd/lobber@latest
```

## Usage

```bash
lobber login                      # Authenticate (opens browser)
lobber up app.mysite.com:3000     # Start tunnel
lobber status                     # Show active tunnels
lobber logs                       # Tail request logs
```

## Why Lobber?

- **Your domain** - Use `app.yourcompany.com`, not `random-slug.ngrok.io`
- **Persistent URLs** - Same domain works every time you reconnect
- **Request inspector** - Debug webhooks at `localhost:4040`
- **Webhook replay** - Re-send failed requests with one click

## Development

```bash
go test ./...                     # Run tests
go build -o lobber ./cmd/lobber   # Build CLI
go build -o relay ./cmd/relay     # Build relay server
```
