package auth

import (
	"crypto/rand"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// GenerateAPIToken creates a new API token with lb_ prefix
// Returns plaintext token and bcrypt hash for storage
func GenerateAPIToken() (plaintext, hash string, err error) {
	// Generate 32 random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	plaintext = "lb_" + hex.EncodeToString(bytes)

	// Hash for storage
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	hash = string(hashBytes)

	return plaintext, hash, nil
}

// ValidateAPIToken checks if a token matches a hash
func ValidateAPIToken(plaintext, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	return err == nil
}
