package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// GenerateOtp generates a crypto-secure numeric OTP of the specified length (e.g. 6 digits)
func GenerateOtp(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}

	max := big.NewInt(1)
	for i := 0; i < length; i++ {
		max.Mul(max, big.NewInt(10))
	}

	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("failed to generate random OTP: %w", err)
	}

	format := fmt.Sprintf("%%0%dd", length)
	return fmt.Sprintf(format, n), nil
}
