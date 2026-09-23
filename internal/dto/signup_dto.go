package dto

import (
	"errors"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// SIGNUP FLOW DTOs (With Rate Limiting & Cooldown)
// -----------------------------------------------------------------------------

// SignupSendCodeRequest represents the Step 1 input from the frontend "Create your account" form.
type SignupSendCodeRequest struct {
	DisplayName string `json:"displayName"` // Primary field in UI
	Username    string `json:"username"`    // Fallback/alias
	Email       string `json:"email" binding:"required"`
	Password    string `json:"password" binding:"required"`
}

func (r *SignupSendCodeRequest) Validate() error {
	name := strings.TrimSpace(r.DisplayName)
	if name == "" {
		name = strings.TrimSpace(r.Username)
	}
	if name == "" {
		return errors.New("display name is required")
	}
	if len(name) < 2 || len(name) > 50 {
		return errors.New("display name must be between 2 and 50 characters")
	}
	r.DisplayName = name
	r.Username = name

	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	if r.Email == "" {
		return errors.New("email is required")
	}
	if !strings.Contains(r.Email, "@") || !strings.Contains(r.Email, ".") {
		return errors.New("invalid email address format")
	}

	if len(r.Password) < 6 {
		return errors.New("password must be at least 6 characters long")
	}
	return nil
}

// SignupResendCodeRequest represents the request to resend a verification code with cooldown.
type SignupResendCodeRequest struct {
	Email string `json:"email" binding:"required"`
}

func (r *SignupResendCodeRequest) Validate() error {
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	if r.Email == "" {
		return errors.New("email is required")
	}
	if !strings.Contains(r.Email, "@") || !strings.Contains(r.Email, ".") {
		return errors.New("invalid email address format")
	}
	return nil
}

// SignupVerifyRequest represents the Step 2 input from the "Verify your email" form.
type SignupVerifyRequest struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code"`
	OTP   string `json:"otp"` // Fallback alias
}

func (r *SignupVerifyRequest) Validate() error {
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	if r.Email == "" {
		return errors.New("email is required")
	}
	code := strings.TrimSpace(r.Code)
	if code == "" {
		code = strings.TrimSpace(r.OTP)
	}
	if code == "" {
		return errors.New("verification code is required")
	}
	if len(code) != 6 {
		return errors.New("verification code must be exactly 6 digits")
	}
	r.Code = code
	r.OTP = code
	return nil
}

// SignupSendCodeResponse is returned after sending or resending an OTP code.
type SignupSendCodeResponse struct {
	Email           string `json:"email"`
	CooldownSeconds int    `json:"cooldownSeconds"`
	ExpiresIn       int    `json:"expiresIn"`
	Message         string `json:"message,omitempty"`
	OTP             string `json:"otp,omitempty"`
}

// PendingSignupData holds pending registration state in Redis or memory store.
type PendingSignupData struct {
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"passwordHash"`
	OTP          string    `json:"otp"`
	Attempts     int       `json:"attempts"`
	CreatedAt    time.Time `json:"createdAt"`
}
