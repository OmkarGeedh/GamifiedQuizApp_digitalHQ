package dto

import (
	"errors"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// LOGIN DTOs
// -----------------------------------------------------------------------------

type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (r *LoginRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return errors.New("email is required")
	}
	if r.Password == "" {
		return errors.New("password is required")
	}
	return nil
}

type LoginSuccessResponse struct {
	AccessToken  string                 `json:"accessToken"`
	RefreshToken string                 `json:"refreshToken"`
	TokenType    string                 `json:"tokenType"`
	ExpiresIn    int                    `json:"expiresIn"`
	Client       *ClientSessionResponse `json:"client,omitempty"`
	Message      string                 `json:"message,omitempty"`
}

// -----------------------------------------------------------------------------
// SESSION DTOs
// -----------------------------------------------------------------------------

type ClientVerifySessionResponse struct {
	Active bool   `json:"active"`
	Status string `json:"status,omitempty"`
}

type ClientSessionResponse struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Phone     *string   `json:"phone,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// -----------------------------------------------------------------------------
// PASSWORD CHANGE DTOs
// -----------------------------------------------------------------------------

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

func (r *ChangePasswordRequest) Validate() error {
	if r.OldPassword == "" {
		return errors.New("old password is required")
	}
	if len(r.NewPassword) < 6 {
		return errors.New("new password must be at least 6 characters long")
	}
	return nil
}

type ChangePasswordVerifyRequest struct {
	OTP string `json:"otp" binding:"required"`
}

func (r *ChangePasswordVerifyRequest) Validate() error {
	r.OTP = strings.TrimSpace(r.OTP)
	if r.OTP == "" {
		return errors.New("otp is required")
	}
	return nil
}

type ChangePasswordStash struct {
	OTP         string `json:"otp"`
	NewPassword string `json:"newPassword"`
}

// -----------------------------------------------------------------------------
// PASSWORD RECOVERY DTOs
// -----------------------------------------------------------------------------

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required"`
}

func (r *ForgotPasswordRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return errors.New("email is required")
	}
	return nil
}

type ForgotPasswordVerifyTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

func (r *ForgotPasswordVerifyTokenRequest) Validate() error {
	r.Token = strings.TrimSpace(r.Token)
	if r.Token == "" {
		return errors.New("token is required")
	}
	return nil
}

type ResetPasswordVerifyRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

func (r *ResetPasswordVerifyRequest) Validate() error {
	r.Token = strings.TrimSpace(r.Token)
	if r.Token == "" {
		return errors.New("token is required")
	}
	if len(r.NewPassword) < 6 {
		return errors.New("new password must be at least 6 characters long")
	}
	return nil
}


