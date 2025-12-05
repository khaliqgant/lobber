package auth

import (
	"strings"
	"testing"
)

func TestGenerateAPIToken(t *testing.T) {
	plaintext, hash, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken() error: %v", err)
	}

	// Token should have lb_ prefix
	if !strings.HasPrefix(plaintext, "lb_") {
		t.Errorf("token %q should have lb_ prefix", plaintext)
	}

	// Token should be 67 chars: lb_ (3) + 64 hex chars
	if len(plaintext) != 67 {
		t.Errorf("token length = %d, want 67", len(plaintext))
	}

	// Hash should not be empty
	if hash == "" {
		t.Error("hash should not be empty")
	}

	// Hash should be different from plaintext
	if hash == plaintext {
		t.Error("hash should be different from plaintext")
	}
}

func TestValidateAPIToken(t *testing.T) {
	plaintext, hash, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken() error: %v", err)
	}

	// Valid token should validate
	if !ValidateAPIToken(plaintext, hash) {
		t.Error("ValidateAPIToken() should return true for valid token")
	}

	// Invalid token should not validate
	if ValidateAPIToken("lb_invalid", hash) {
		t.Error("ValidateAPIToken() should return false for invalid token")
	}

	// Wrong hash should not validate
	if ValidateAPIToken(plaintext, "wronghash") {
		t.Error("ValidateAPIToken() should return false for wrong hash")
	}
}

func TestGenerateAPITokenUniqueness(t *testing.T) {
	// Generate multiple tokens and ensure they're unique
	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		plaintext, _, err := GenerateAPIToken()
		if err != nil {
			t.Fatalf("GenerateAPIToken() error: %v", err)
		}
		if tokens[plaintext] {
			t.Error("GenerateAPIToken() generated duplicate token")
		}
		tokens[plaintext] = true
	}
}
