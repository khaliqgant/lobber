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
	inspect := fs.Bool("inspect", true, "Enable local inspector")
	inspectPort := fs.Int("inspect-port", 4040, "Inspector port")
	noInspect := fs.Bool("no-inspect", false, "Disable local inspector")
	quiet := fs.Bool("quiet", false, "Minimal output")
	domain := fs.String("domain", "", "Custom domain to use")

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
	_ = domain

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
