package dto

import (
	"errors"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// LOGIN DTOs
// -----------------------------------------------------------------------------

type LoginGenOTPRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (r *LoginGenOTPRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return errors.New("email is required")
	}
	if r.Password == "" {
		return errors.New("password is required")
	}
	return nil
}

type LoginGenOTPResponse struct {
	Phone   string `json:"phone,omitempty"`
	Message string `json:"message"`
}

type LoginResendOTPRequest struct {
	Email string `json:"email" binding:"required"`
}

func (r *LoginResendOTPRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return errors.New("email is required")
	}
	return nil
}

type LoginOTPVerifyRequest struct {
	Email string `json:"email" binding:"required"`
	OTP   string `json:"otp" binding:"required"`
}

func (r *LoginOTPVerifyRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	r.OTP = strings.TrimSpace(r.OTP)
	if r.Email == "" {
		return errors.New("email is required")
	}
	if r.OTP == "" {
		return errors.New("otp is required")
	}
	return nil
}

type LoginSuccessResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int    `json:"expiresIn"`
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
// REGISTRATION & RECOVERY DTOs
// -----------------------------------------------------------------------------

type RegisterEmailRequest struct {
	Email string `json:"email" binding:"required"`
}

func (r *RegisterEmailRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return errors.New("email is required")
	}
	return nil
}

type RegisterEmailVerifyRequest struct {
	Email string `json:"email" binding:"required"`
	OTP   string `json:"otp" binding:"required"`
}

func (r *RegisterEmailVerifyRequest) Validate() error {
	r.Email = strings.TrimSpace(r.Email)
	r.OTP = strings.TrimSpace(r.OTP)
	if r.Email == "" || r.OTP == "" {
		return errors.New("email and otp are required")
	}
	return nil
}

type RegisterPhoneRequest struct {
	Phone string `json:"phone" binding:"required"`
}

func (r *RegisterPhoneRequest) Validate() error {
	r.Phone = strings.TrimSpace(r.Phone)
	if r.Phone == "" {
		return errors.New("phone is required")
	}
	return nil
}

type RegisterPhoneVerifyRequest struct {
	Phone string `json:"phone" binding:"required"`
	OTP   string `json:"otp" binding:"required"`
}

func (r *RegisterPhoneVerifyRequest) Validate() error {
	r.Phone = strings.TrimSpace(r.Phone)
	r.OTP = strings.TrimSpace(r.OTP)
	if r.Phone == "" || r.OTP == "" {
		return errors.New("phone and otp are required")
	}
	return nil
}

type RegisterValidateBasicRequest struct {
	Username string  `json:"username" binding:"required"`
	Email    string  `json:"email" binding:"required"`
	Phone    *string `json:"phone,omitempty"`
	Password string  `json:"password" binding:"required"`
}

func (r *RegisterValidateBasicRequest) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.TrimSpace(r.Email)
	if r.Username == "" {
		return errors.New("username is required")
	}
	if r.Email == "" {
		return errors.New("email is required")
	}
	if len(r.Password) < 6 {
		return errors.New("password must be at least 6 characters long")
	}
	return nil
}

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
