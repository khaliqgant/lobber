package relay

import (
	"testing"
)

func TestVerifyCNAME(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		resolver  DNSResolver
		wantErr   bool
		errSubstr string
	}{
		{
			name:   "valid CNAME to tunnel.lobber.dev",
			domain: "myapp.example.com",
			resolver: func(domain string) (string, error) {
				return "tunnel.lobber.dev", nil
			},
			wantErr: false,
		},
		{
			name:   "invalid CNAME to wrong target",
			domain: "myapp.example.com",
			resolver: func(domain string) (string, error) {
				return "other.example.com", nil
			},
			wantErr:   true,
			errSubstr: "expected tunnel.lobber.dev",
		},
		{
			name:   "CNAME with trailing dot",
			domain: "myapp.example.com",
			resolver: func(domain string) (string, error) {
				return "tunnel.lobber.dev.", nil
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyCNAMEWithResolver(tt.domain, tt.resolver)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
