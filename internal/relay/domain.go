package relay

import (
	"fmt"
	"net"
	"strings"
)

const ServiceDomain = "tunnel.lobber.dev"

// DNSResolver is a function that looks up the CNAME for a domain
type DNSResolver func(domain string) (cname string, err error)

// DefaultDNSResolver uses net.LookupCNAME
func DefaultDNSResolver(domain string) (string, error) {
	cname, err := net.LookupCNAME(domain)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(cname, "."), nil
}

// VerifyCNAME checks if domain has correct CNAME record pointing to tunnel.lobber.dev
func VerifyCNAME(domain string) error {
	return VerifyCNAMEWithResolver(domain, DefaultDNSResolver)
}

// VerifyCNAMEWithResolver checks CNAME using a custom resolver (for testing)
func VerifyCNAMEWithResolver(domain string, resolver DNSResolver) error {
	cname, err := resolver(domain)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	// Remove trailing dot if present
	cname = strings.TrimSuffix(cname, ".")

	if cname != ServiceDomain {
		return fmt.Errorf("CNAME points to %s, expected %s", cname, ServiceDomain)
	}

	return nil
}
