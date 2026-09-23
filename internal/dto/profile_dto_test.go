package dto_test

import (
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
)

func TestProfileSetupRequestDTO_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.ProfileSetupRequestDTO
		wantErr bool
	}{
		{
			name: "Valid request",
			req: dto.ProfileSetupRequestDTO{
				AvatarID: "user",
				Name:     "Omkar",
				Class:    "11th",
				Board:    "Maharashtra State Board",
				Subjects: []string{"accounting"},
			},
			wantErr: false,
		},
		{
			name: "Valid request with default avatarId",
			req: dto.ProfileSetupRequestDTO{
				AvatarID: "",
				Name:     "Omkar",
				Class:    "11th",
				Board:    "Maharashtra State Board",
				Subjects: []string{"accounting"},
			},
			wantErr: false,
		},
		{
			name: "Empty name",
			req: dto.ProfileSetupRequestDTO{
				AvatarID: "user",
				Name:     "",
				Class:    "11th",
				Board:    "Maharashtra State Board",
				Subjects: []string{"accounting"},
			},
			wantErr: true,
		},
		{
			name: "Empty class",
			req: dto.ProfileSetupRequestDTO{
				AvatarID: "user",
				Name:     "Omkar",
				Class:    "",
				Board:    "Maharashtra State Board",
				Subjects: []string{"accounting"},
			},
			wantErr: true,
		},
		{
			name: "Empty board",
			req: dto.ProfileSetupRequestDTO{
				AvatarID: "user",
				Name:     "Omkar",
				Class:    "11th",
				Board:    "",
				Subjects: []string{"accounting"},
			},
			wantErr: true,
		},
		{
			name: "Empty subjects",
			req: dto.ProfileSetupRequestDTO{
				AvatarID: "user",
				Name:     "Omkar",
				Class:    "11th",
				Board:    "Maharashtra State Board",
				Subjects: []string{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.req.AvatarID == "" {
				t.Errorf("Validate() did not default empty AvatarID to 'user'")
			}
		})
	}
}
