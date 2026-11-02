package dto

import (
	"testing"
)

func TestLoginRequest_Validate(t *testing.T) {
	tests := []struct {
		name      string
		req       LoginRequest
		wantError bool
		errMsg    string
	}{
		{
			name: "Valid request",
			req: LoginRequest{
				Email:    "player@example.com",
				Password: "secretpassword123",
			},
			wantError: false,
		},
		{
			name: "Valid request with leading/trailing spaces",
			req: LoginRequest{
				Email:    "   player@example.com   ",
				Password: "secretpassword123",
			},
			wantError: false,
		},
		{
			name: "Missing email",
			req: LoginRequest{
				Email:    "",
				Password: "secretpassword123",
			},
			wantError: true,
			errMsg:    "email is required",
		},
		{
			name: "Missing password",
			req: LoginRequest{
				Email:    "player@example.com",
				Password: "",
			},
			wantError: true,
			errMsg:    "password is required",
		},
		{
			name: "Spaces only email",
			req: LoginRequest{
				Email:    "     ",
				Password: "secretpassword123",
			},
			wantError: true,
			errMsg:    "email is required",
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
