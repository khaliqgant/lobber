package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lobber-dev/lobber/internal/client"
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
  lobber up app.mysite.com:3000 --domain my.custom.com
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
	relay := fs.String("relay", "https://lobber.dev", "Relay server URL")
	inspect := fs.Bool("inspect", true, "Enable local inspector")
	inspectPort := fs.Int("inspect-port", 4040, "Inspector port")
	noInspect := fs.Bool("no-inspect", false, "Disable local inspector")
	quiet := fs.Bool("quiet", false, "Minimal output")
	domain := fs.String("domain", "", "Custom domain to use")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: lobber up <domain>:<port> [--relay URL]")
	}

	target := fs.Arg(0)
	_ = inspect
	_ = inspectPort
	_ = noInspect

	// Parse target (domain:port or just port)
	var tunnelDomain string
	var localPort string

	if strings.Contains(target, ":") {
		parts := strings.SplitN(target, ":", 2)
		tunnelDomain = parts[0]
		localPort = parts[1]
	} else {
		localPort = target
		tunnelDomain = "tunnel.lobber.dev" // default
	}

	// Override domain if specified
	if *domain != "" {
		tunnelDomain = *domain
	}

	// Build local address
	localAddr := fmt.Sprintf("http://localhost:%s", localPort)

	// Get token from flag or config
	authToken := *token
	if authToken == "" {
		cfg, err := LoadConfig()
		if err == nil && cfg.Token != "" {
			authToken = cfg.Token
		} else {
			// Use a default dev token for local testing
			authToken = "dev-token"
		}
	}

	if !*quiet {
		fmt.Printf("Starting tunnel...\n")
		fmt.Printf("  Local:  %s\n", localAddr)
		fmt.Printf("  Domain: %s\n", tunnelDomain)
		fmt.Printf("  Relay:  %s\n", *relay)
		fmt.Println()
	}

	// Create client
	c := client.New(localAddr, *relay, authToken, tunnelDomain)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		if !*quiet {
			fmt.Println("\nShutting down tunnel...")
		}
		cancel()
	}()

	// Set ready callback
	c.SetOnReady(func() {
		if !*quiet {
			fmt.Printf("Tunnel ready! Forwarding %s -> %s\n", tunnelDomain, localAddr)
			fmt.Println("Press Ctrl+C to stop")
		}
	})

	// Run the tunnel (blocks until cancelled or error)
	if err := c.Run(ctx); err != nil {
		if err == context.Canceled {
			return nil // Normal shutdown
		}
		return fmt.Errorf("tunnel error: %w", err)
	}

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
