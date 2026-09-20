package utils

import (
	"fmt"
	/*
		"crypto/rand"
		"math/big"
	*/
)

// GenerateOtp generates a numeric OTP of the specified length (hardcoded to 123456 for testing)
func GenerateOtp(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}

	// Hardcoded OTP for testing as requested
	return "123456", nil

	/*
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
	*/
}
