package dto

import (
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func TestCreateSessionRequest_ValidateGameMode(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{
			name:  "omitted mode defaults to mcq so older clients keep working",
			input: "",
			want:  models.GameModeMCQ,
		},
		{
			name:  "explicit mcq is preserved",
			input: "mcq",
			want:  models.GameModeMCQ,
		},
		{
			name:  "sudden death is accepted",
			input: "sudden_death",
			want:  models.GameModeSuddenDeath,
		},
		{
			name:  "mode is trimmed and lowercased",
			input: "  SUDDEN_DEATH  ",
			want:  models.GameModeSuddenDeath,
		},
		{
			name:      "unknown mode is rejected",
			input:     "battle_royale",
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &CreateSessionRequestDTO{TopicID: "accounting", GameMode: tc.input}

			err := req.Validate()
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected an error for game_mode %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GameMode != tc.want {
				t.Fatalf("expected game_mode %q, got %q", tc.want, req.GameMode)
			}
		})
	}
}

// Validation must reject a bad mode even when the rest of the request is fine.
func TestCreateSessionRequest_ValidateRejectsBadModeWithValidTopic(t *testing.T) {
	req := &CreateSessionRequestDTO{TopicID: "  accounting  ", QuestionCount: 5, GameMode: "nope"}
	if err := req.Validate(); err == nil {
		t.Fatal("expected an error for an unsupported game_mode")
	}
}

// A missing topic is still an error regardless of mode.
func TestCreateSessionRequest_ValidateRequiresTopic(t *testing.T) {
	req := &CreateSessionRequestDTO{GameMode: models.GameModeSuddenDeath}
	if err := req.Validate(); err == nil {
		t.Fatal("expected an error when topic is empty")
	}
}

// The mode must not disturb the existing question_count rules.
func TestCreateSessionRequest_ValidateKeepsQuestionCountRules(t *testing.T) {
	req := &CreateSessionRequestDTO{TopicID: "accounting", GameMode: models.GameModeSuddenDeath}
	if err := req.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.QuestionCount != 10 {
		t.Fatalf("expected the default question_count of 10, got %d", req.QuestionCount)
	}

	tooMany := &CreateSessionRequestDTO{TopicID: "accounting", QuestionCount: 26, GameMode: models.GameModeMCQ}
	if err := tooMany.Validate(); err == nil {
		t.Fatal("expected question_count above 25 to be rejected")
	}
}

// IsSuddenDeath must be an explicit match so pre-migration rows stay on MCQ.
func TestGameSession_IsSuddenDeath(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want bool
	}{
		{name: "sudden death", mode: models.GameModeSuddenDeath, want: true},
		{name: "mcq", mode: models.GameModeMCQ, want: false},
		{name: "legacy empty", mode: "", want: false},
		{name: "unknown", mode: "battle_royale", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.GameSession{GameMode: tc.mode}
			if got := s.IsSuddenDeath(); got != tc.want {
				t.Fatalf("mode %q: expected %v, got %v", tc.mode, tc.want, got)
			}
		})
	}
}

func TestGameSession_IsSuddenDeathNilSafe(t *testing.T) {
	var s *models.GameSession
	if s.IsSuddenDeath() {
		t.Fatal("a nil session must not report sudden death")
	}
}