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
