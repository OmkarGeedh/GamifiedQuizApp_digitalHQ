package ws

import "encoding/json"

// --- Inbound Messages (Client → Server) ---

// InboundMessage is the top-level wrapper for all client WebSocket messages.
type InboundMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// JoinGameData is the payload for "join_game" messages.
type JoinGameData struct {
	Session string `json:"session,omitempty"`
}

// SubmitAnswerData is the payload for "submit_answer" messages.
type SubmitAnswerData struct {
	Session      string `json:"session,omitempty"`
	Question     string `json:"question"`
	Option       string `json:"option"`
	SelectedText string `json:"selected_text,omitempty"`
	TimeTakenMs  int    `json:"time_taken_ms"`
}

// UsePowerUpData is the payload for "use_power_up" messages.
type UsePowerUpData struct {
	Session  string `json:"session,omitempty"`
	Question string `json:"question"`
	PowerUp  string `json:"power_up"`
}

// --- Outbound Messages (Server → Client) ---

// OutboundMessage is the top-level wrapper for all server WebSocket messages.
type OutboundMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Message type constants
const (
	MsgTypeJoinGame      = "join_game"
	MsgTypeSubmitAnswer  = "submit_answer"
	MsgTypeUsePowerUp    = "use_power_up"
	MsgTypeQuestion      = "question"
	MsgTypeAnswerResult  = "answer_result"
	MsgTypePowerUpResult = "power_up_result"
	MsgTypeGameOver      = "game_over"
	MsgTypeError         = "error"
)

// Machine-readable error codes
const (
	ErrCodeInvalidPayload       = "invalid_payload"
	ErrCodeMissingQuestion      = "missing_question"
	ErrCodeStaleAnswer          = "stale_answer"
	ErrCodeMissingOption        = "missing_option"
	ErrCodeUnsupportedPowerUp   = "unsupported_power_up"
	ErrCodePowerUpAlreadyUsed   = "power_up_already_used"
	ErrCodeSessionMismatch      = "session_mismatch"
	ErrCodeSessionNotActive     = "session_not_active"
	ErrCodeTransitionInProgress = "transition_in_progress"
	ErrCodeUnsupportedType      = "unsupported_type"
	ErrCodeInternalError        = "internal_error"
)

// QuestionPayload is the question data sent to the client.
type QuestionPayload struct {
	Question    string          `json:"question"`
	Prompt      string          `json:"prompt"`
	Points      int             `json:"points"`
	Hint        *string         `json:"hint,omitempty"`
	Options     []OptionPayload `json:"options"`
	TimeLimitMs int             `json:"time_limit_ms"`
	// RemainingTimeMs is always emitted. It must not be omitempty: a resync with
	// zero milliseconds left has to be distinguishable from the field being absent,
	// otherwise clients fall back to TimeLimitMs and believe they have a full timer.
	RemainingTimeMs int `json:"remaining_time_ms"`
	QuestionNumber  int `json:"question_number"`
	TotalQuestions  int `json:"total_questions"`
}

// OptionPayload is a single MCQ option in the WebSocket payload.
type OptionPayload struct {
	Option string `json:"option"`
	Text   string `json:"text"`
}

// AnswerResultPayload is the grading result sent after an answer is submitted or timed out.
type AnswerResultPayload struct {
	Question      string  `json:"question"`
	Option        string  `json:"option"`
	CorrectOption string  `json:"correct_option"`
	IsCorrect     bool    `json:"is_correct"`
	IsSkipped     bool    `json:"is_skipped"`
	Explanation   *string `json:"explanation,omitempty"`
	PointsEarned  int     `json:"points_earned"`
	CoinsEarned   int     `json:"coins_earned"`
	YourScore     int     `json:"your_score"`
	IsTimeout     bool    `json:"is_timeout"`
}

// PowerUpResultPayload communicates hidden options after a 50:50 power-up.
type PowerUpResultPayload struct {
	Question      string   `json:"question"`
	HiddenOptions []string `json:"hidden_options"`
}

// GameOverPayload is the final summary sent when the quiz ends.
type GameOverPayload struct {
	FinalScore     int                   `json:"final_score"`
	TotalQuestions int                   `json:"total_questions"`
	CorrectCount   int                   `json:"correct_count"`
	CoinsEarned    int                   `json:"coins_earned"`
	XPEarned       int                   `json:"xp_earned"`
	GemsEarned     int                   `json:"gems_earned"`
	NewLevel       int                   `json:"new_level"`
	DidLevelUp     bool                  `json:"did_level_up"`
	LevelUpReward  *LevelUpRewardPayload `json:"level_up_reward,omitempty"`
}

// LevelUpRewardPayload is separate from the base MCQ reward totals.
type LevelUpRewardPayload struct {
	Coins int `json:"coins"`
	XP    int `json:"xp"`
	Gems  int `json:"gems"`
}

// ErrorPayload communicates error details to the client.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
