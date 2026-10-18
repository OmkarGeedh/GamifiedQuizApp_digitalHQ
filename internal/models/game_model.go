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
	PairType        string  `json:"pair_type,omitempty"`
	CorrectOption   string  `json:"correct_option"`             // 'a', 'b', 'c', or 'd' matching shuffled options
	OriginalCorrect string  `json:"original_correct,omitempty"` // original DB correct letter ('a', 'b', 'c', 'd')
	CorrectText     string  `json:"correct_text,omitempty"`     // the actual string text of the correct answer
	Difficulty      int     `json:"difficulty"`
	Points          int     `json:"points"`
	Hint            *string     `json:"hint,omitempty"`
	Explanation     *string     `json:"explanation,omitempty"`
	MatchPairs      []MatchPair `json:"match_pairs,omitempty"`
}

// MatchPair represents a single item pair within a Match the Following board.
type MatchPair struct {
	ID         string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	QuestionID string    `gorm:"column:question_id;type:uuid;not null;index:idx_match_pairs_question" json:"question_id"`
	PairOrder  int       `gorm:"column:pair_order;not null;default:1" json:"pair_order"`
	LeftText   string    `gorm:"column:left_text;type:text;not null" json:"left_text"`
	RightText  string    `gorm:"column:right_text;type:text;not null" json:"right_text"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (MatchPair) TableName() string {
	return "match_pairs"
}

// MCQQuestion represents a 4-option question for standard MCQ and Sudden Death modes.
// The CorrectOption field is tagged json:"-" to prevent accidental serialization to clients.
type MCQQuestion struct {
	ID            string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	QuestionCode  string    `gorm:"column:question_code;size:20;not null;uniqueIndex" json:"question_code"`
	QuestionType  string    `gorm:"column:question_type;size:32;not null;default:'mcq';index:idx_mcq_type" json:"question_type"`
	TopicID       string    `gorm:"column:topic_id;size:64;not null;index:idx_mcq_topic" json:"topic_id"`
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

func (MCQQuestion) TableName() string {
	return "mcq_questions"
}

// Question is an alias to MCQQuestion for seamless backward compatibility.
type Question = MCQQuestion

// SortItOutQuestion represents a binary choice rapid-fire swipe question (Debit/Credit, True/False, Asset/Liability).
// Zero sparse or null columns: strictly OptionA and OptionB.
type SortItOutQuestion struct {
	ID            string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	QuestionCode  string    `gorm:"column:question_code;size:20;not null;uniqueIndex" json:"question_code"`
	TopicID       string    `gorm:"column:topic_id;size:64;not null;index:idx_sio_topic" json:"topic_id"`
	PairType      string    `gorm:"column:pair_type;size:32;not null;index:idx_sio_pair" json:"pair_type"`
	Prompt        string    `gorm:"column:prompt;type:text;not null" json:"prompt"`
	OptionA       string    `gorm:"column:option_a;type:text;not null" json:"option_a"`
	OptionB       string    `gorm:"column:option_b;type:text;not null" json:"option_b"`
	CorrectOption string    `gorm:"column:correct_option;type:char(1);not null" json:"-"` // 'a' or 'b'
	Difficulty    int       `gorm:"column:difficulty;type:smallint;not null;default:1" json:"difficulty"`
	Points        int       `gorm:"column:points;not null;default:10" json:"points"`
	Hint          *string   `gorm:"column:hint;type:text" json:"hint,omitempty"`
	Explanation   *string   `gorm:"column:explanation;type:text" json:"explanation,omitempty"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (SortItOutQuestion) TableName() string {
	return "sort_it_out_questions"
}

// MatchTheFollowingQuestion represents a multi-board column matching puzzle board.
type MatchTheFollowingQuestion struct {
	ID           string      `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	QuestionCode string      `gorm:"column:question_code;size:20;not null;uniqueIndex" json:"question_code"`
	TopicID      string      `gorm:"column:topic_id;size:64;not null;index:idx_mtf_topic" json:"topic_id"`
	Prompt       string      `gorm:"column:prompt;type:text;not null" json:"prompt"`
	TimeLimitSec int         `gorm:"column:time_limit_sec;not null;default:25" json:"time_limit_sec"`
	Difficulty   int         `gorm:"column:difficulty;type:smallint;not null;default:1" json:"difficulty"`
	Points       int         `gorm:"column:points;not null;default:40" json:"points"`
	Hint         *string     `gorm:"column:hint;type:text" json:"hint,omitempty"`
	Explanation  *string     `gorm:"column:explanation;type:text" json:"explanation,omitempty"`
	Pairs        []MatchPair `gorm:"foreignKey:QuestionID;references:ID" json:"pairs,omitempty"`
	CreatedAt    time.Time   `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time   `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (MatchTheFollowingQuestion) TableName() string {
	return "match_the_following_questions"
}

// GameSession tracks a single quiz session for a player.
type GameSession struct {
	ID                    string     `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClientID              int        `gorm:"column:client_id;not null;index:idx_game_sessions_client;index:idx_game_sessions_client_status,priority:1" json:"client_id"`
	TopicID               string     `gorm:"column:topic_id;size:64;not null;default:'general'" json:"topic_id"`
	GameMode              string     `gorm:"column:game_mode;size:20;not null;default:'mcq'" json:"game_mode"`
	PairType              string     `gorm:"column:pair_type;size:32" json:"pair_type,omitempty"`
	TotalQuestions        int        `gorm:"column:total_questions;not null;default:10" json:"total_questions"`
	CurrentIdx            int        `gorm:"column:current_idx;not null;default:0" json:"current_idx"`
	Score                 int        `gorm:"column:score;not null;default:0" json:"score"`
	CorrectCount          int        `gorm:"column:correct_count;not null;default:0" json:"correct_count"`
	ComboStreak           int        `gorm:"column:combo_streak;not null;default:0" json:"combo_streak"`
	BestStreak            int        `gorm:"column:best_streak;not null;default:0" json:"best_streak"`
	StreakMilestones      int        `gorm:"column:streak_milestones;not null;default:0" json:"streak_milestones"`
	SkippedCount          int        `gorm:"column:skipped_count;not null;default:0" json:"skipped_count"`
	EndReason             string     `gorm:"column:end_reason;size:32;not null;default:''" json:"end_reason"`
	CompletedSuccessfully bool       `gorm:"column:completed_successfully;not null;default:false" json:"completed_successfully"`
	Status                string     `gorm:"column:status;size:20;not null;default:'in_progress';index:idx_game_sessions_client_status,priority:2" json:"status"`
	QuestionsState        string     `gorm:"column:questions_state;type:text" json:"-"`
	StartedAt             time.Time  `gorm:"column:started_at;autoCreateTime" json:"started_at"`
	EndedAt               *time.Time `gorm:"column:ended_at" json:"ended_at,omitempty"`
	CreatedAt             time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (GameSession) TableName() string {
	return "game_sessions"
}

// Game mode constants. The mode is persisted on the session so a reconnecting
// player resumes the same rules they started with.
const (
	// GameModeMCQ is the original survival-free flow: answer every question,
	// wrong answers only reset the streak, and the run ends after the last one.
	GameModeMCQ = "mcq"
	// GameModeSuddenDeath is the elimination flow: the first wrong answer or
	// timeout terminates the run immediately.
	GameModeSuddenDeath = "sudden_death"
	// GameModeSortItOut is the rapid-fire binary choice swipe mini-game.
	GameModeSortItOut = "sort_it_out"
	// GameModeMatchTheFollowing is the multi-board matching puzzle game mode.
	GameModeMatchTheFollowing = "match_the_following"
)

// IsSuddenDeath reports whether this session uses elimination rules.
//
// The comparison is an explicit match on purpose: rows written before the
// game_mode column existed carry an empty string, and those must keep behaving
// like GameModeMCQ rather than silently gaining elimination semantics.
func (s *GameSession) IsSuddenDeath() bool {
	return s != nil && s.GameMode == GameModeSuddenDeath
}

// IsSortItOut reports whether this session uses Sort It Out swipe game rules.
func (s *GameSession) IsSortItOut() bool {
	return s != nil && s.GameMode == GameModeSortItOut
}

// IsMatchTheFollowing reports whether this session uses Match the Following puzzle rules.
func (s *GameSession) IsMatchTheFollowing() bool {
	return s != nil && s.GameMode == GameModeMatchTheFollowing
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

// ActiveSession is the in-memory runtime session representation stored in Redis RAM.
type ActiveSession = GameSession

// UserQuestionHistory is an immutable audit record of every answer submitted across all game modes.
type UserQuestionHistory struct {
	ID             string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClientID       int       `gorm:"column:client_id;not null;index:idx_uqh_client;index:idx_uqh_client_answered,priority:1" json:"client_id"`
	SessionID      string    `gorm:"column:session_id;type:uuid;not null;index:idx_uqh_session;index:idx_uqh_session_question,priority:1" json:"session_id"`
	GameMode       string    `gorm:"column:game_mode;size:32;not null;default:'mcq';index:idx_uqh_mode" json:"game_mode"`
	QuestionCode   string    `gorm:"column:question_code;size:20;not null;index:idx_uqh_session_question,priority:2" json:"question_code"`
	SelectedOption string    `gorm:"column:selected_option;size:50;not null" json:"selected_option"`
	IsCorrect      bool      `gorm:"column:is_correct;not null;default:false" json:"is_correct"`
	TimeTakenMs    int       `gorm:"column:time_taken_ms;not null;default:0" json:"time_taken_ms"`
	PointsAwarded  int       `gorm:"column:points_awarded;not null;default:0" json:"points_awarded"`
	CoinsAwarded   int       `gorm:"column:coins_awarded;not null;default:0" json:"coins_awarded"`
	AnsweredAt     time.Time `gorm:"column:answered_at;autoCreateTime;index:idx_uqh_client_answered,priority:2" json:"answered_at"`
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
	TxTypeQuizReward      = "quiz_reward"
	TxTypeLevelBonus      = "level_up_bonus"
	TxTypeStreakBonus     = "streak_bonus"
	TxTypePowerUpPurchase = "power_up_purchase"
	TxTypeShopPurchase    = "shop_purchase"
	TxTypeManualDebit     = "manual_debit"
	TxTypeManualCredit    = "manual_credit"
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
