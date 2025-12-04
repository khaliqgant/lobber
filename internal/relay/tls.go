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
