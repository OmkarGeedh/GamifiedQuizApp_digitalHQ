package ws

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/gorilla/websocket"
)

const (
	questionTimeLimitSec = 15
	writeWait            = 10 * time.Second
	pongWait             = 60 * time.Second
	pingInterval         = (pongWait * 9) / 10
	maxMessageSize       = 4096
)

// PlayerConn represents a connected player's WebSocket connection.
type PlayerConn struct {
	Conn     *websocket.Conn
	ClientID int
	mu       sync.Mutex
}

// WriteJSON safely writes a JSON message to the client.
func (p *PlayerConn) WriteJSON(v interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.Conn.SetWriteDeadline(time.Now().Add(writeWait))
	return p.Conn.WriteJSON(v)
}

// GameSession manages a live quiz session with server-authoritative timing.
type GameSession struct {
	ID        string
	ClientID  int
	Session   *models.GameSession
	Questions []models.SessionQuestion
	Player    *PlayerConn
	inbox     chan sessionEvent
	cancel    context.CancelFunc
	mu        sync.Mutex
}

// sessionEvent is an internal event dispatched to the session goroutine.
type sessionEvent struct {
	eventType string
	data      json.RawMessage
}

// NewGameSession creates a session ready to run.
func NewGameSession(sessionID string, clientID int, session *models.GameSession, questions []models.SessionQuestion) *GameSession {
	return &GameSession{
		ID:        sessionID,
		ClientID:  clientID,
		Session:   session,
		Questions: questions,
		inbox:     make(chan sessionEvent, 16),
	}
}

// AttachPlayer binds a WebSocket connection to the session and immediately delivers the active question.
func (gs *GameSession) AttachPlayer(conn *websocket.Conn, clientID int) {
	gs.mu.Lock()
	gs.Player = &PlayerConn{Conn: conn, ClientID: clientID}
	gs.mu.Unlock()

	// Immediately deliver current question to connected player
	gs.broadcastQuestion()
}

// Run starts the session event loop. This method blocks until the session ends
// or the context is cancelled. It must be called in its own goroutine.
func (gs *GameSession) Run(ctx context.Context) {
	ctx, gs.cancel = context.WithCancel(ctx)
	defer gs.cancel()

	// Set up the question timer (non-blocking via channel signal)
	timer := time.NewTimer(questionTimeLimitSec * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			gs.handleGameEnd(ctx)
			return

		case <-timer.C:
			// Sudden Death: time expired for current question
			gs.handleTimeout(ctx)
			if gs.Session.Status != models.SessionStatusInProgress {
				return
			}
			// Reset timer for next question
			timer.Reset(questionTimeLimitSec * time.Second)

		case event := <-gs.inbox:
			switch event.eventType {
			case MsgTypeSubmitAnswer:
				gs.handleAnswer(ctx, event.data)
				if gs.Session.Status != models.SessionStatusInProgress {
					return
				}
				// Reset timer for next question
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(questionTimeLimitSec * time.Second)
			}
		}
	}
}

// PushEvent sends an event to the session event loop.
func (gs *GameSession) PushEvent(eventType string, data json.RawMessage) {
	select {
	case gs.inbox <- sessionEvent{eventType: eventType, data: data}:
	default:
		log.Printf("[ws] session %s: inbox full, dropping event %s", gs.ID, eventType)
	}
}

// Stop cleanly cancels the session event loop.
func (gs *GameSession) Stop() {
	if gs.cancel != nil {
		gs.cancel()
	}
}

// --- Internal Event Handlers ---

func (gs *GameSession) broadcastQuestion() {
	gs.mu.Lock()
	if gs.Session.CurrentIdx >= len(gs.Questions) {
		gs.mu.Unlock()
		return
	}
	idx := gs.Session.CurrentIdx
	q := gs.Questions[idx]
	total := gs.Session.TotalQuestions
	gs.mu.Unlock()

	payload := OutboundMessage{
		Type: MsgTypeQuestion,
		Data: QuestionPayload{
			Question:       q.QuestionCode,
			Prompt:         q.Prompt,
			Points:         q.Points,
			Hint:           q.Hint,
			Options: []OptionPayload{
				{Option: "a", Text: q.OptionA},
				{Option: "b", Text: q.OptionB},
				{Option: "c", Text: q.OptionC},
				{Option: "d", Text: q.OptionD},
			},
			TimeLimitMs:    questionTimeLimitSec * 1000,
			QuestionNumber: idx + 1,
			TotalQuestions: total,
		},
	}
	gs.sendToPlayer(payload)
}

func (gs *GameSession) handleAnswer(ctx context.Context, rawData json.RawMessage) {
	var data SubmitAnswerData
	if err := json.Unmarshal(rawData, &data); err != nil {
		gs.sendError("invalid answer payload")
		return
	}

	// Validate option
	option := strings.ToLower(strings.TrimSpace(data.Option))
	validOptions := map[string]bool{"a": true, "b": true, "c": true, "d": true, "skip": true}
	if !validOptions[option] {
		gs.sendError("option must be 'a', 'b', 'c', 'd', or 'skip'")
		return
	}

	gs.mu.Lock()
	if gs.Session.CurrentIdx >= len(gs.Questions) {
		gs.mu.Unlock()
		return
	}
	q := gs.Questions[gs.Session.CurrentIdx]
	gs.mu.Unlock()

	// Grade
	isSkipped := option == "skip"
	isCorrect := false
	if !isSkipped {
		isCorrect = strings.EqualFold(option, q.CorrectOption)
	}

	pointsEarned := 0
	coinsEarned := 0

	if isCorrect {
		gs.Session.ComboStreak++
		if gs.Session.ComboStreak > gs.Session.BestStreak {
			gs.Session.BestStreak = gs.Session.ComboStreak
		}
		pointsEarned = services.CalculatePoints(q.Points, q.Difficulty, data.TimeTakenMs, gs.Session.ComboStreak)
		coinsEarned = services.CalculateCoins(pointsEarned)
		gs.Session.CorrectCount++
	} else {
		gs.Session.ComboStreak = 0
	}
	gs.Session.Score += pointsEarned
	gs.mu.Unlock()

	// Record in DB
	_ = repo.RecordAnswer(ctx, &models.UserQuestionHistory{
		ClientID:       gs.ClientID,
		SessionID:      gs.Session.ID,
		QuestionCode:   q.QuestionCode,
		SelectedOption: option,
		IsCorrect:      isCorrect,
		TimeTakenMs:    data.TimeTakenMs,
		PointsAwarded:  pointsEarned,
		CoinsAwarded:   coinsEarned,
	})

	// Send result
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeAnswerResult,
		Data: AnswerResultPayload{
			Question:      q.QuestionCode,
			Option:        option,
			CorrectOption: strings.ToLower(q.CorrectOption),
			IsCorrect:     isCorrect,
			Explanation:   q.Explanation,
			PointsEarned:  pointsEarned,
			CoinsEarned:   coinsEarned,
			YourScore:     gs.Session.Score,
			IsTimeout:     false,
		},
	})

	gs.advanceQuestion(ctx)
}

func (gs *GameSession) handleTimeout(ctx context.Context) {
	gs.mu.Lock()
	if gs.Session.CurrentIdx >= len(gs.Questions) {
		gs.mu.Unlock()
		return
	}
	q := gs.Questions[gs.Session.CurrentIdx]
	gs.Session.ComboStreak = 0
	gs.mu.Unlock()

	// Record timeout as unanswered
	_ = repo.RecordAnswer(ctx, &models.UserQuestionHistory{
		ClientID:       gs.ClientID,
		SessionID:      gs.Session.ID,
		QuestionCode:   q.QuestionCode,
		SelectedOption: "timeout",
		IsCorrect:      false,
		TimeTakenMs:    questionTimeLimitSec * 1000,
		PointsAwarded:  0,
		CoinsAwarded:   0,
	})

	// Send timeout result
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeAnswerResult,
		Data: AnswerResultPayload{
			Question:      q.QuestionCode,
			Option:        "timeout",
			CorrectOption: strings.ToLower(q.CorrectOption),
			IsCorrect:     false,
			Explanation:   q.Explanation,
			PointsEarned:  0,
			CoinsEarned:   0,
			YourScore:     gs.Session.Score,
			IsTimeout:     true,
		},
	})

	gs.advanceQuestion(ctx)
}

func (gs *GameSession) advanceQuestion(ctx context.Context) {
	gs.mu.Lock()
	gs.Session.CurrentIdx++
	idx := gs.Session.CurrentIdx
	total := len(gs.Questions)
	gs.mu.Unlock()

	// Persist progress
	_ = repo.UpdateSession(ctx, gs.Session)

	if idx >= total {
		gs.handleGameEnd(ctx)
		return
	}

	// Brief delay before next question (non-blocking via goroutine)
	go func() {
		time.Sleep(800 * time.Millisecond)
		gs.broadcastQuestion()
	}()
}

func (gs *GameSession) handleGameEnd(ctx context.Context) {
	gs.mu.Lock()
	if gs.Session.Status != models.SessionStatusInProgress {
		gs.mu.Unlock()
		return
	}
	gs.mu.Unlock()

	// Calculate rewards
	coins, xp, gems := services.CalculateGameRewards(gs.Session.Score, gs.Session.CorrectCount, gs.Session.TotalQuestions)

	// Fetch profile for level info
	profile, err := services.GetOrCreateProfile(ctx, gs.ClientID)
	newLevel := 1
	didLevelUp := false
	if err == nil {
		oldLevel := profile.Level
		newLevel = services.CalculateNewLevel(profile.Experience + xp)
		didLevelUp = newLevel > oldLevel
		if didLevelUp {
			coins += (newLevel - oldLevel) * 50
			gems += (newLevel - oldLevel)
		}
	}

	// Atomic finalization
	_ = repo.FinalizeSession(ctx, gs.Session, coins, xp, gems)

	if didLevelUp && profile != nil {
		_ = repo.UpdateProfileLevel(ctx, gs.ClientID, newLevel)
	}

	// Send game over
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeGameOver,
		Data: GameOverPayload{
			FinalScore:     gs.Session.Score,
			TotalQuestions: gs.Session.TotalQuestions,
			CorrectCount:   gs.Session.CorrectCount,
			CoinsEarned:    coins,
			XPEarned:       xp,
			GemsEarned:     gems,
			NewLevel:       newLevel,
			DidLevelUp:     didLevelUp,
		},
	})
}

func (gs *GameSession) sendToPlayer(msg OutboundMessage) {
	gs.mu.Lock()
	player := gs.Player
	gs.mu.Unlock()

	if player == nil {
		return
	}
	if err := player.WriteJSON(msg); err != nil {
		log.Printf("[ws] session %s: failed to send message: %v", gs.ID, err)
	}
}

func (gs *GameSession) sendError(message string) {
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeError,
		Data: ErrorPayload{Message: message},
	})
}
