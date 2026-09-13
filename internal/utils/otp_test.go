package utils

import (
	"testing"
)

func TestGenerateOtp(t *testing.T) {
	otp, err := GenerateOtp(6)
	if err != nil {
		t.Fatalf("unexpected error generating OTP: %v", err)
	}
	if len(otp) != 6 {
		t.Errorf("expected OTP length 6, got %d (%s)", len(otp), otp)
	}

	for _, ch := range otp {
		if ch < '0' || ch > '9' {
			t.Errorf("expected numeric digit, got %c in %s", ch, otp)
		}
	}

	_, errInvalid := GenerateOtp(0)
	if errInvalid == nil {
		t.Errorf("expected error for length 0, got nil")
	}
}

func TestGenerateToken64(t *testing.T) {
	token, err := GenerateToken64()
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected token length 64, got %d (%s)", len(token), token)
	}
}
