package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateToken64 generates a crypto-secure 64-character hex string (32 random bytes)
func GenerateToken64() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
