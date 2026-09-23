package dto

import (
	"testing"
)

func TestSignupSendCodeRequest_Validate(t *testing.T) {
	tests := []struct {
		name      string
		req       SignupSendCodeRequest
		wantError bool
		errMsg    string
	}{
		{
			name: "Valid request with displayName",
			req: SignupSendCodeRequest{
				DisplayName: "Omkar",
				Email:       "test@gmail.com",
				Password:    "SecurePass123!",
			},
			wantError: false,
		},
		{
			name: "Valid request with username fallback",
			req: SignupSendCodeRequest{
				Username: "Omkar",
				Email:    "test@gmail.com",
				Password: "SecurePass123!",
			},
			wantError: false,
		},
		{
			name: "Missing name",
			req: SignupSendCodeRequest{
				Email:    "test@gmail.com",
				Password: "SecurePass123!",
			},
			wantError: true,
			errMsg:    "display name is required",
		},
		{
			name: "Name too short",
			req: SignupSendCodeRequest{
				DisplayName: "A",
				Email:       "test@gmail.com",
				Password:    "SecurePass123!",
			},
			wantError: true,
			errMsg:    "display name must be between 2 and 50 characters",
		},
		{
			name: "Invalid email",
			req: SignupSendCodeRequest{
				DisplayName: "Omkar",
				Email:       "invalid-email",
				Password:    "SecurePass123!",
			},
			wantError: true,
			errMsg:    "invalid email address format",
		},
		{
			name: "Password too short",
			req: SignupSendCodeRequest{
				DisplayName: "Omkar",
				Email:       "test@gmail.com",
				Password:    "12345",
			},
			wantError: true,
			errMsg:    "password must be at least 6 characters long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && err.Error() != tt.errMsg {
				t.Errorf("Validate() errMsg = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestSignupVerifyRequest_Validate(t *testing.T) {
	tests := []struct {
		name      string
		req       SignupVerifyRequest
		wantError bool
		errMsg    string
	}{
		{
			name: "Valid request with code",
			req: SignupVerifyRequest{
				Email: "test@gmail.com",
				Code:  "123456",
			},
			wantError: false,
		},
		{
			name: "Valid request with otp field",
			req: SignupVerifyRequest{
				Email: "test@gmail.com",
				OTP:   "654321",
			},
			wantError: false,
		},
		{
			name: "Missing email",
			req: SignupVerifyRequest{
				Code: "123456",
			},
			wantError: true,
			errMsg:    "email is required",
		},
		{
			name: "Missing code",
			req: SignupVerifyRequest{
				Email: "test@gmail.com",
			},
			wantError: true,
			errMsg:    "verification code is required",
		},
		{
			name: "Code wrong length",
			req: SignupVerifyRequest{
				Email: "test@gmail.com",
				Code:  "12345",
			},
			wantError: true,
			errMsg:    "verification code must be exactly 6 digits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && err.Error() != tt.errMsg {
				t.Errorf("Validate() errMsg = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestSignupResendCodeRequest_Validate(t *testing.T) {
	valid := SignupResendCodeRequest{Email: "test@gmail.com"}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	invalid := SignupResendCodeRequest{Email: "invalid"}
	if err := invalid.Validate(); err == nil {
		t.Errorf("expected error for invalid email, got nil")
	}
}
