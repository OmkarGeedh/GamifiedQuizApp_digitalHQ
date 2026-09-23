package models

import (
	"encoding/json"
	"time"
)

// SessionQuestion represents the shuffled state of a question within a game session.
type SessionQuestion struct {
	QuestionCode    string  `json:"question_code"`
	Prompt          string  `json:"prompt"`
	OptionA         string  `json:"option_a"`
	OptionB         string  `json:"option_b"`
	OptionC         string  `json:"option_c"`
	OptionD         string  `json:"option_d"`
	CorrectOption   string  `json:"correct_option"` // 'a', 'b', 'c', or 'd' matching shuffled options
	OriginalCorrect string  `json:"original_correct,omitempty"` // original DB correct letter ('a', 'b', 'c', 'd')
	CorrectText     string  `json:"correct_text,omitempty"`    // the actual string text of the correct answer
	Difficulty      int     `json:"difficulty"`
	Points          int     `json:"points"`
	Hint            *string `json:"hint,omitempty"`
	Explanation     *string `json:"explanation,omitempty"`
}

// Question represents a single MCQ question stored in PostgreSQL.
// The CorrectOption field is tagged json:"-" to prevent accidental serialization to clients.
type Question struct {
	ID            string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	QuestionCode  string    `gorm:"column:question_code;size:20;not null;uniqueIndex" json:"question_code"`
	TopicID       string    `gorm:"column:topic_id;size:64;not null;index:idx_questions_topic" json:"topic_id"`
	Prompt        string    `gorm:"column:prompt;type:text;not null" json:"prompt"`
	OptionA       string    `gorm:"column:option_a;type:text;not null" json:"option_a"`
	OptionB       string    `gorm:"column:option_b;type:text;not null" json:"option_b"`
	OptionC       string    `gorm:"column:option_c;type:text;not null" json:"option_c"`
	OptionD       string    `gorm:"column:option_d;type:text;not null" json:"option_d"`
	CorrectOption string    `gorm:"column:correct_option;type:char(1);not null" json:"-"`
	Difficulty    int       `gorm:"column:difficulty;type:smallint;not null;default:1" json:"difficulty"`
	Points        int       `gorm:"column:points;not null;default:10" json:"points"`
	Hint          *string   `gorm:"column:hint;type:text" json:"hint,omitempty"`
	Explanation   *string   `gorm:"column:explanation;type:text" json:"explanation,omitempty"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (Question) TableName() string {
	return "questions"
}

// GameSession tracks a single quiz session for a player.
type GameSession struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClientID       int        `gorm:"column:client_id;not null;index:idx_game_sessions_client;index:idx_game_sessions_client_status,priority:1" json:"client_id"`
	TopicID        string     `gorm:"column:topic_id;size:64;not null;default:'general'" json:"topic_id"`
	TotalQuestions int        `gorm:"column:total_questions;not null;default:10" json:"total_questions"`
	CurrentIdx     int        `gorm:"column:current_idx;not null;default:0" json:"current_idx"`
	Score          int        `gorm:"column:score;not null;default:0" json:"score"`
	CorrectCount   int        `gorm:"column:correct_count;not null;default:0" json:"correct_count"`
	ComboStreak    int        `gorm:"column:combo_streak;not null;default:0" json:"combo_streak"`
	BestStreak     int        `gorm:"column:best_streak;not null;default:0" json:"best_streak"`
	Status         string     `gorm:"column:status;size:20;not null;default:'in_progress';index:idx_game_sessions_client_status,priority:2" json:"status"`
	QuestionsState string     `gorm:"column:questions_state;type:text" json:"-"`
	StartedAt      time.Time  `gorm:"column:started_at;autoCreateTime" json:"started_at"`
	EndedAt        *time.Time `gorm:"column:ended_at" json:"ended_at,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (GameSession) TableName() string {
	return "game_sessions"
}

// SetQuestions serializes and stores the session's shuffled questions.
func (s *GameSession) SetQuestions(questions []SessionQuestion) error {
	b, err := json.Marshal(questions)
	if err != nil {
		return err
	}
	s.QuestionsState = string(b)
	return nil
}

// GetQuestions deserializes the session's shuffled questions.
func (s *GameSession) GetQuestions() ([]SessionQuestion, error) {
	if s.QuestionsState == "" {
		return nil, nil
	}
	var questions []SessionQuestion
	if err := json.Unmarshal([]byte(s.QuestionsState), &questions); err != nil {
		return nil, err
	}
	return questions, nil
}

// Session status constants
const (
	SessionStatusInProgress = "in_progress"
	SessionStatusFinished   = "finished"
	SessionStatusAbandoned  = "abandoned"
)

// UserQuestionHistory is an immutable audit record of every answer submitted.
type UserQuestionHistory struct {
	ID             string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClientID       int       `gorm:"column:client_id;not null;index:idx_uqh_client" json:"client_id"`
	SessionID      string    `gorm:"column:session_id;type:uuid;not null;index:idx_uqh_session" json:"session_id"`
	QuestionCode   string    `gorm:"column:question_code;size:20;not null" json:"question_code"`
	SelectedOption string    `gorm:"column:selected_option;size:10;not null" json:"selected_option"`
	IsCorrect      bool      `gorm:"column:is_correct;not null;default:false" json:"is_correct"`
	TimeTakenMs    int       `gorm:"column:time_taken_ms;not null;default:0" json:"time_taken_ms"`
	PointsAwarded  int       `gorm:"column:points_awarded;not null;default:0" json:"points_awarded"`
	CoinsAwarded   int       `gorm:"column:coins_awarded;not null;default:0" json:"coins_awarded"`
	AnsweredAt     time.Time `gorm:"column:answered_at;autoCreateTime" json:"answered_at"`
}

func (UserQuestionHistory) TableName() string {
	return "user_question_history"
}

// WalletLedger is an append-only financial audit trail.
// Rows are never updated or deleted — only inserted.
type WalletLedger struct {
	ID              string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClientID        int       `gorm:"column:client_id;not null;index:idx_wallet_ledger_client;index:idx_wallet_ledger_client_created,priority:1" json:"client_id"`
	TransactionType string    `gorm:"column:transaction_type;size:50;not null" json:"transaction_type"`
	Direction       string    `gorm:"column:direction;size:10;not null;default:'credit'" json:"direction"`
	Amount          int       `gorm:"column:amount;not null" json:"amount"`
	Currency        string    `gorm:"column:currency;size:20;not null" json:"currency"`
	BalanceAfter    int       `gorm:"column:balance_after;not null;default:0" json:"balance_after"`
	ReferenceID     string    `gorm:"column:reference_id;size:64" json:"reference_id,omitempty"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime;index:idx_wallet_ledger_client_created,priority:2" json:"created_at"`
}

func (WalletLedger) TableName() string {
	return "wallet_ledger"
}

// Ledger transaction type constants
const (
	TxTypeQuizReward       = "quiz_reward"
	TxTypeLevelBonus       = "level_up_bonus"
	TxTypeStreakBonus      = "streak_bonus"
	TxTypePowerUpPurchase  = "power_up_purchase"
	TxTypeShopPurchase     = "shop_purchase"
	TxTypeManualDebit      = "manual_debit"
	TxTypeManualCredit     = "manual_credit"
)

// Transaction direction constants
const (
	DirectionCredit = "credit"
	DirectionDebit  = "debit"
)

// Ledger currency constants
const (
	CurrencyCoins      = "coins"
	CurrencyGems       = "gems"
	CurrencyExperience = "experience"
)
