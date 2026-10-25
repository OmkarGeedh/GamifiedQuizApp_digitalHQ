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

func TestCreateSessionRequest_ValidateMatchTheFollowing(t *testing.T) {
	t.Run("default question_count and time_limit", func(t *testing.T) {
		req := &CreateSessionRequestDTO{
			TopicID:  "accounting",
			GameMode: models.GameModeMatchTheFollowing,
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.QuestionCount != 10 {
			t.Fatalf("expected default question_count=10, got %d", req.QuestionCount)
		}
		if req.TimeLimitPerQuestionSec != 25 {
			t.Fatalf("expected default time_limit=25, got %d", req.TimeLimitPerQuestionSec)
		}
	})

	t.Run("valid custom boards and time_limit", func(t *testing.T) {
		req := &CreateSessionRequestDTO{
			TopicID:                 "accounting",
			GameMode:                models.GameModeMatchTheFollowing,
			QuestionCount:           10,
			TimeLimitPerQuestionSec: 30,
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.QuestionCount != 10 {
			t.Fatalf("expected question_count=10, got %d", req.QuestionCount)
		}
		if req.TimeLimitPerQuestionSec != 30 {
			t.Fatalf("expected time_limit=30, got %d", req.TimeLimitPerQuestionSec)
		}
	})

	t.Run("question_count out of range rejected", func(t *testing.T) {
		tooFew := &CreateSessionRequestDTO{
			TopicID:       "accounting",
			GameMode:      models.GameModeMatchTheFollowing,
			QuestionCount: -1, // will default to 10 in Validate, but explicit 0 or negative
		}
		_ = tooFew.Validate()
		if tooFew.QuestionCount != 10 {
			t.Fatalf("expected -1 to default to 10, got %d", tooFew.QuestionCount)
		}

		tooMany := &CreateSessionRequestDTO{
			TopicID:       "accounting",
			GameMode:      models.GameModeMatchTheFollowing,
			QuestionCount: 26,
		}
		if err := tooMany.Validate(); err == nil {
			t.Fatal("expected error for question_count > 25")
		}
	})

	t.Run("time_limit out of range rejected", func(t *testing.T) {
		tooLow := &CreateSessionRequestDTO{
			TopicID:                 "accounting",
			GameMode:                models.GameModeMatchTheFollowing,
			TimeLimitPerQuestionSec: 10,
		}
		if err := tooLow.Validate(); err == nil {
			t.Fatal("expected error for time_limit < 15")
		}

		tooHigh := &CreateSessionRequestDTO{
			TopicID:                 "accounting",
			GameMode:                models.GameModeMatchTheFollowing,
			TimeLimitPerQuestionSec: 50,
		}
		if err := tooHigh.Validate(); err == nil {
			t.Fatal("expected error for time_limit > 45")
		}
	})
}

func TestCompleteMatchTheFollowingRequestDTO_Validate(t *testing.T) {
	t.Run("empty session rejected", func(t *testing.T) {
		req := CompleteMatchTheFollowingRequestDTO{
			Session: "",
			Boards: []MatchBoardAnswerSubmissionDTO{
				{QuestionCode: "mtf_001"},
			},
		}
		if err := req.Validate(); err == nil {
			t.Fatal("expected error for empty session")
		}
	})

	t.Run("empty boards rejected", func(t *testing.T) {
		req := CompleteMatchTheFollowingRequestDTO{
			Session: "sess-123",
			Boards:  []MatchBoardAnswerSubmissionDTO{},
		}
		if err := req.Validate(); err == nil {
			t.Fatal("expected error for empty boards")
		}
	})

	t.Run("missing question_code rejected", func(t *testing.T) {
		req := CompleteMatchTheFollowingRequestDTO{
			Session: "sess-123",
			Boards: []MatchBoardAnswerSubmissionDTO{
				{QuestionCode: ""},
			},
		}
		if err := req.Validate(); err == nil {
			t.Fatal("expected error for missing question_code")
		}
	})

	t.Run("negative mistakes rejected", func(t *testing.T) {
		req := CompleteMatchTheFollowingRequestDTO{
			Session: "sess-123",
			Boards: []MatchBoardAnswerSubmissionDTO{
				{QuestionCode: "mtf_001", Mistakes: -1},
			},
		}
		if err := req.Validate(); err == nil {
			t.Fatal("expected error for negative mistakes")
		}
	})

	t.Run("negative time rejected", func(t *testing.T) {
		req := CompleteMatchTheFollowingRequestDTO{
			Session: "sess-123",
			Boards: []MatchBoardAnswerSubmissionDTO{
				{QuestionCode: "mtf_001", TimeTakenMs: -50},
			},
		}
		if err := req.Validate(); err == nil {
			t.Fatal("expected error for negative time_taken_ms")
		}
	})

	t.Run("valid payload passes", func(t *testing.T) {
		req := CompleteMatchTheFollowingRequestDTO{
			Session: "sess-123",
			Boards: []MatchBoardAnswerSubmissionDTO{
				{
					QuestionCode: "mtf_001",
					Matches:      map[string]string{"left_1": "right_1"},
					Mistakes:     0,
					TimeTakenMs:  12000,
				},
			},
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error for valid payload: %v", err)
		}
	})
}
