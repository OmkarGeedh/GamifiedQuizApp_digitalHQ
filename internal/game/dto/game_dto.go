package dto

import (
	"errors"
	"strings"
)

// --- Request DTOs ---

// CreateSessionRequestDTO validates input for starting a new quiz session.
type CreateSessionRequestDTO struct {
	TopicID       string `json:"topic" binding:"required"`
	QuestionCount int    `json:"question_count"`
	AbandonStale  bool   `json:"abandon_stale,omitempty"`
}

// Validate ensures the request has sane defaults and limits.
func (r *CreateSessionRequestDTO) Validate() error {
	r.TopicID = strings.TrimSpace(r.TopicID)
	if r.TopicID == "" {
		return errors.New("topic is required")
	}
	if r.QuestionCount <= 0 {
		r.QuestionCount = 10
	}
	if r.QuestionCount > 25 {
		return errors.New("question_count must not exceed 25")
	}
	return nil
}

// SubmitAnswerRequestDTO validates a player's answer submission.
type SubmitAnswerRequestDTO struct {
	Session     string `json:"session" binding:"required"`
	Question    string `json:"question" binding:"required"`
	Option      string `json:"option" binding:"required"`
	TimeTakenMs int    `json:"time_taken_ms"`
}

// Validate ensures the answer fields are correctly formatted.
func (r *SubmitAnswerRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	r.Question = strings.TrimSpace(r.Question)
	if r.Question == "" {
		return errors.New("question is required")
	}
	r.Option = strings.ToLower(strings.TrimSpace(r.Option))
	if r.Option == "" {
		return errors.New("option is required")
	}
	validOptions := map[string]bool{"a": true, "b": true, "c": true, "d": true, "skip": true}
	if !validOptions[r.Option] {
		return errors.New("option must be 'a', 'b', 'c', 'd', or 'skip'")
	}
	if r.TimeTakenMs < 0 {
		return errors.New("time_taken_ms cannot be negative")
	}
	// Server-enforced maximum: 15s + 2s grace
	if r.TimeTakenMs > 17000 {
		r.TimeTakenMs = 17000
	}
	return nil
}

// FiftyFiftyRequestDTO validates a 50:50 power-up request.
type FiftyFiftyRequestDTO struct {
	Session  string `json:"session" binding:"required"`
	Question string `json:"question" binding:"required"`
}

// Validate ensures the fields are non-empty.
func (r *FiftyFiftyRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	r.Question = strings.TrimSpace(r.Question)
	if r.Question == "" {
		return errors.New("question is required")
	}
	return nil
}

// CompleteSessionRequestDTO validates a session completion request.
type CompleteSessionRequestDTO struct {
	Session string `json:"session" binding:"required"`
}

// Validate ensures the session field is non-empty.
func (r *CompleteSessionRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	return nil
}

// --- Response DTOs ---

// OptionDTO is a single option within a question payload.
type OptionDTO struct {
	Option string `json:"option"`
	Text   string `json:"text"`
}

// QuestionDTO is a question payload safe for client delivery.
// CorrectOption is included only when the client needs it for power-ups.
type QuestionDTO struct {
	Question      string      `json:"question"`
	Prompt        string      `json:"prompt"`
	Points        int         `json:"points"`
	Hint          *string     `json:"hint,omitempty"`
	Options       []OptionDTO `json:"options"`
	CorrectOption string      `json:"correct_option,omitempty"`
}

// TopicQuestionsResponseDTO wraps the list of questions for a topic.
type TopicQuestionsResponseDTO struct {
	Topic        string        `json:"topic"`
	Total        int           `json:"total"`
	TimeLimitSec int           `json:"time_limit_sec"`
	Questions    []QuestionDTO `json:"questions"`
}

// SessionCreatedResponseDTO is returned when a new quiz session is created.
type SessionCreatedResponseDTO struct {
	Session        string        `json:"session"`
	Topic          string        `json:"topic"`
	TotalQuestions int           `json:"total_questions"`
	TimeLimitSec   int           `json:"time_limit_sec"`
	Questions      []QuestionDTO `json:"questions"`
}

// AnswerResultDTO is the evaluation result for a submitted answer.
type AnswerResultDTO struct {
	Question      string  `json:"question"`
	Option        string  `json:"option"`
	CorrectOption string  `json:"correct_option"`
	IsCorrect     bool    `json:"is_correct"`
	IsSkipped     bool    `json:"is_skipped"`
	Explanation   *string `json:"explanation,omitempty"`
	PointsEarned  int     `json:"points_earned"`
	CoinsEarned   int     `json:"coins_earned"`
	ComboStreak   int     `json:"combo_streak"`
	TotalScore    int     `json:"total_score"`
}

// FiftyFiftyResponseDTO lists the two options to hide.
type FiftyFiftyResponseDTO struct {
	Question      string   `json:"question"`
	HiddenOptions []string `json:"hidden_options"`
}

// LevelInfoDTO contains level/XP progression data.
type LevelInfoDTO struct {
	Current      int `json:"current"`
	DidLevelUp   bool `json:"did_level_up"`
	Experience   int `json:"experience"`
	NextLevelExp int `json:"next_level_exp"`
}

// StreakInfoDTO contains streak summary data.
type StreakInfoDTO struct {
	CurrentStreak  int  `json:"current_streak"`
	TodayCompleted bool `json:"today_completed"`
}

// SessionCompleteResponseDTO is the final game summary after quiz completion.
type SessionCompleteResponseDTO struct {
	Session            string       `json:"session"`
	TotalQuestions     int          `json:"total_questions"`
	CorrectCount       int          `json:"correct_count"`
	AccuracyPercentage float64      `json:"accuracy_percentage"`
	FinalScore         int          `json:"final_score"`
	CoinsAwarded       int          `json:"coins_awarded"`
	XPAwarded          int          `json:"xp_awarded"`
	GemsAwarded        int          `json:"gems_awarded"`
	Level              LevelInfoDTO `json:"level"`
	Streak             StreakInfoDTO `json:"streak"`
}
