package ws

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
)

// HTTP (turn-based) Sudden Death. Same rules and payloads as the WebSocket game,
// but every state change happens inside a request and all timing state lives on the
// cached models.GameSession, so it works behind proxies that cannot carry WebSockets
// (Zoho Catalyst AppSail) and survives instance restarts when Redis is configured.
// Each response is the ordered list of events the socket would have pushed.

// answerGrace absorbs HTTP round-trip jitter on top of the authoritative deadline.
const answerGrace = time.Second

type TurnResponse struct {
	Events []OutboundMessage `json:"events"`
}

// TurnError is a transport-level failure (unknown session, wrong owner, wrong mode).
type TurnError struct {
	Status int
	Err    error
}

func (e *TurnError) Error() string { return e.Err.Error() }

func turnErr(status int, format string, args ...any) *TurnError {
	return &TurnError{Status: status, Err: fmt.Errorf(format, args...)}
}

type turn struct {
	s         *models.GameSession
	questions []models.SessionQuestion
	clientID  int
	now       time.Time
	events    []OutboundMessage
}

func loadTurn(ctx context.Context, clientID int, sessionID string, now time.Time) (*turn, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, turnErr(http.StatusBadRequest, "session is required")
	}
	s, err := repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, turnErr(http.StatusNotFound, "session not found")
	}
	if s.ClientID != clientID {
		return nil, turnErr(http.StatusForbidden, "session belongs to another player")
	}
	if !s.IsSuddenDeath() {
		return nil, turnErr(http.StatusBadRequest, "session is not a sudden_death session")
	}
	questions, err := s.GetQuestions()
	if err != nil || len(questions) == 0 {
		return nil, turnErr(http.StatusInternalServerError, "session has no questions")
	}
	return &turn{s: s, questions: questions, clientID: clientID, now: now}, nil
}

func (t *turn) emit(msgType string, data any) {
	t.events = append(t.events, OutboundMessage{Type: msgType, Data: data})
}

func (t *turn) fail(code, message string) {
	t.emit(MsgTypeError, ErrorPayload{Code: code, Message: message})
}

func (t *turn) active() bool {
	if t.s.Status != models.SessionStatusInProgress {
		t.fail(ErrCodeSessionNotActive, "session has finished or been abandoned")
		return false
	}
	return true
}

func (t *turn) current() *models.SessionQuestion { return &t.questions[t.s.CurrentIdx] }

func (t *turn) expired() bool {
	return t.s.QuestionDeadline != nil && t.now.After(t.s.QuestionDeadline.Add(answerGrace))
}

func (t *turn) remainingMs() int {
	if t.s.QuestionDeadline == nil {
		return questionTimeLimitSec * 1000
	}
	if r := t.s.QuestionDeadline.Sub(t.now); r > 0 {
		return int(r.Milliseconds())
	}
	return 0
}

// serve starts the current question's clock (once) and emits it. The first serve of
// a question after an answer includes the client transition delay in the deadline.
func (t *turn) serve(afterTransition bool) {
	if t.s.QuestionDeadline == nil {
		d := t.now.Add(questionTimeLimitSec * time.Second)
		if afterTransition {
			d = d.Add(transitionDelayMs * time.Millisecond)
		}
		t.s.QuestionDeadline = &d
	}
	payload := precomputeQuestionPayloads(t.questions[t.s.CurrentIdx:t.s.CurrentIdx+1], t.s.TotalQuestions)[0]
	payload.QuestionNumber = t.s.CurrentIdx + 1
	payload.RemainingTimeMs = min(t.remainingMs(), questionTimeLimitSec*1000)
	payload.ServerTimestampMs = t.now.UnixMilli()
	t.emit(MsgTypeQuestion, payload)
}

// record writes answer history in the background so the turn response never waits
// on PostgreSQL; the session itself lives in Redis.
func (t *turn) record(_ context.Context, q *models.SessionQuestion, option string, correct bool, timeTakenMs, points int) {
	rec := &models.UserQuestionHistory{
		ClientID:       t.clientID,
		SessionID:      t.s.ID,
		GameMode:       t.s.GameMode,
		QuestionCode:   q.QuestionCode,
		SelectedOption: option,
		IsCorrect:      correct,
		TimeTakenMs:    max(0, min(timeTakenMs, questionTimeLimitSec*1000+int(addTimeDuration.Milliseconds()))),
		PointsAwarded:  points,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.RecordAnswer(ctx, rec); err != nil {
			log.Printf("[sudden-death] async persist answer failed for %s: %v", rec.QuestionCode, err)
		}
	}()
}

func (t *turn) end(ctx context.Context, reason string) error {
	gameOver, err := finalizeRun(ctx, t.clientID, t.s, reason)
	if err != nil {
		return err
	}
	t.emit(MsgTypeGameOver, *gameOver)
	return nil
}

func (t *turn) timeout(ctx context.Context) error {
	q := t.current()
	t.s.ComboStreak = 0
	t.record(ctx, q, "timeout", false, questionTimeLimitSec*1000, 0)
	t.emit(MsgTypeAnswerResult, AnswerResultPayload{
		Question:      q.QuestionCode,
		Option:        "timeout",
		CorrectOption: strings.ToLower(q.CorrectOption),
		Explanation:   q.Explanation,
		YourScore:     t.s.Score,
		IsTimeout:     true,
	})
	return t.end(ctx, EndReasonTimeout)
}

func (t *turn) save(ctx context.Context) error {
	if t.s.Status != models.SessionStatusInProgress {
		return nil // finalizeRun already persisted the finished session
	}
	if err := repo.UpdateSession(ctx, t.s); err != nil {
		return err
	}
	services.RefreshSessionTTL(ctx, t.s.ID)
	return nil
}

func (t *turn) finish(ctx context.Context, err error) (*TurnResponse, error) {
	if err == nil {
		err = t.save(ctx)
	}
	if err != nil {
		var te *TurnError
		if errors.As(err, &te) {
			return nil, te
		}
		return nil, &TurnError{Status: http.StatusInternalServerError, Err: err}
	}
	return &TurnResponse{Events: t.events}, nil
}

// CurrentTurn returns the active question (starting its clock on first call). It is
// idempotent and doubles as resync; an expired question is resolved as a timeout.
func CurrentTurn(ctx context.Context, clientID int, sessionID string, now time.Time) (*TurnResponse, error) {
	t, err := loadTurn(ctx, clientID, sessionID, now)
	if err != nil {
		return nil, err
	}
	if !t.active() {
		return t.finish(ctx, nil)
	}
	if t.expired() {
		return t.finish(ctx, t.timeout(ctx))
	}
	t.serve(false)
	return t.finish(ctx, nil)
}

// SubmitTurn grades an answer, Skip, or client-reported "timeout" for the active
// question and returns the result followed by the next question or game_over.
func SubmitTurn(ctx context.Context, clientID int, sessionID string, data SubmitAnswerData, now time.Time) (*TurnResponse, error) {
	t, err := loadTurn(ctx, clientID, sessionID, now)
	if err != nil {
		return nil, err
	}
	if !t.active() {
		return t.finish(ctx, nil)
	}
	question, option := strings.TrimSpace(data.Question), strings.TrimSpace(data.Option)
	q := t.current()
	switch {
	case question == "":
		t.fail(ErrCodeMissingQuestion, "answer sent without a question")
	case option == "":
		t.fail(ErrCodeMissingOption, "answer sent without an option")
	case t.s.QuestionDeadline == nil:
		t.fail(ErrCodeStaleAnswer, "question has not been served yet; fetch the current question first")
	case question != q.QuestionCode:
		t.fail(ErrCodeStaleAnswer, fmt.Sprintf("stale answer: current question is %s", q.QuestionCode))
	case !cache.AcquireAnswerLock(ctx, t.s.ID, q.QuestionCode):
		t.fail(ErrCodeStaleAnswer, "answer for this question is already being processed")
	case strings.EqualFold(option, "timeout") || t.expired():
		return t.finish(ctx, t.timeout(ctx))
	default:
		return t.finish(ctx, t.answer(ctx, q, option, data))
	}
	return t.finish(ctx, nil)
}

func (t *turn) answer(ctx context.Context, q *models.SessionQuestion, option string, data SubmitAnswerData) error {
	isCorrect, isSkipped, points, resolved := scoreAnswer(t.s, q, option, data.SelectedText)
	t.record(ctx, q, option, isCorrect, data.TimeTakenMs, points)
	t.emit(MsgTypeAnswerResult, AnswerResultPayload{
		Question:      q.QuestionCode,
		Option:        option,
		CorrectOption: strings.ToLower(resolved),
		IsCorrect:     isCorrect,
		IsSkipped:     isSkipped,
		Explanation:   q.Explanation,
		PointsEarned:  points,
		YourScore:     t.s.Score,
	})
	if !isCorrect && !isSkipped {
		return t.end(ctx, EndReasonWrongAnswer)
	}
	t.s.CurrentIdx++
	t.s.QuestionDeadline = nil
	if t.s.CurrentIdx >= len(t.questions) {
		return t.end(ctx, EndReasonCompleted)
	}
	t.serve(true)
	return nil
}

// UseTurnPowerUp applies add_time (+5s, once per run) to the active question.
func UseTurnPowerUp(ctx context.Context, clientID int, sessionID string, data UsePowerUpData, now time.Time) (*TurnResponse, error) {
	t, err := loadTurn(ctx, clientID, sessionID, now)
	if err != nil {
		return nil, err
	}
	if !t.active() {
		return t.finish(ctx, nil)
	}
	q := t.current()
	powerUp := strings.ToLower(strings.TrimSpace(data.PowerUp))
	switch {
	case strings.TrimSpace(data.Question) == "":
		t.fail(ErrCodeMissingQuestion, "use_power_up sent without a question")
	case t.s.QuestionDeadline == nil || strings.TrimSpace(data.Question) != q.QuestionCode:
		t.fail(ErrCodeStaleAnswer, fmt.Sprintf("stale request: current question is %s", q.QuestionCode))
	case t.expired():
		return t.finish(ctx, t.timeout(ctx))
	case powerUp != "add_time":
		t.fail(ErrCodeUnsupportedPowerUp, "only add_time is supported for sudden_death")
	case t.s.AddTimeUsed:
		t.fail(ErrCodePowerUpAlreadyUsed, "add_time power-up has already been used in this session")
	default:
		d := t.s.QuestionDeadline.Add(addTimeDuration)
		t.s.QuestionDeadline = &d
		t.s.AddTimeUsed = true
		t.emit(MsgTypePowerUpResult, PowerUpResultPayload{
			Question:          q.QuestionCode,
			PowerUp:           powerUp,
			AddedTimeMs:       int(addTimeDuration.Milliseconds()),
			RemainingTimeMs:   t.remainingMs(),
			ServerTimestampMs: now.UnixMilli(),
		})
	}
	return t.finish(ctx, nil)
}
