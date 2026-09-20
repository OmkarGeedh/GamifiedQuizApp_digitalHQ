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
	Session string `json:"session"`
}

// SubmitAnswerData is the payload for "submit_answer" messages.
type SubmitAnswerData struct {
	Question     string `json:"question"`
	Option       string `json:"option"`
	SelectedText string `json:"selected_text,omitempty"`
	TimeTakenMs  int    `json:"time_taken_ms"`
}

// --- Outbound Messages (Server → Client) ---

// OutboundMessage is the top-level wrapper for all server WebSocket messages.
type OutboundMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Message type constants
const (
	MsgTypeJoinGame     = "join_game"
	MsgTypeSubmitAnswer = "submit_answer"
	MsgTypeQuestion     = "question"
	MsgTypeAnswerResult = "answer_result"
	MsgTypeGameOver     = "game_over"
	MsgTypeError        = "error"
)

// QuestionPayload is the question data sent to the client.
type QuestionPayload struct {
	Question       string          `json:"question"`
	Prompt         string          `json:"prompt"`
	Points         int             `json:"points"`
	Hint           *string         `json:"hint,omitempty"`
	Options        []OptionPayload `json:"options"`
	TimeLimitMs    int             `json:"time_limit_ms"`
	QuestionNumber int             `json:"question_number"`
	TotalQuestions int             `json:"total_questions"`
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
	Explanation   *string `json:"explanation,omitempty"`
	PointsEarned  int     `json:"points_earned"`
	CoinsEarned   int     `json:"coins_earned"`
	YourScore     int     `json:"your_score"`
	IsTimeout     bool    `json:"is_timeout"`
}

// GameOverPayload is the final summary sent when the quiz ends.
type GameOverPayload struct {
	FinalScore     int  `json:"final_score"`
	TotalQuestions int  `json:"total_questions"`
	CorrectCount   int  `json:"correct_count"`
	CoinsEarned    int  `json:"coins_earned"`
	XPEarned       int  `json:"xp_earned"`
	GemsEarned     int  `json:"gems_earned"`
	NewLevel       int  `json:"new_level"`
	DidLevelUp     bool `json:"did_level_up"`
}

// ErrorPayload communicates error details to the client.
type ErrorPayload struct {
	Message string `json:"message"`
}
