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
