package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/gorilla/websocket"
)

const (
	questionTimeLimitSec = 15
	transitionDelayMs    = 800
	disconnectTTL        = 5 * time.Minute
)

const (
	eventAttach = "internal_attach"
	eventDetach = "internal_detach"
)

type sessionState int

const (
	stateWaiting sessionState = iota
	stateQuestionActive
	stateTransitioning
	statePaused
	stateEnded
)

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

	state             sessionState
	fiftyFiftyUsed    bool
	questionStartTime time.Time
	timeRemaining     time.Duration
}

// sessionEvent is an internal event dispatched to the session goroutine.
type sessionEvent struct {
	eventType string
	data      interface{}
}

// NewGameSession creates a session ready to run.
func NewGameSession(sessionID string, clientID int, session *models.GameSession, questions []models.SessionQuestion) *GameSession {
	return &GameSession{
		ID:            sessionID,
		ClientID:      clientID,
		Session:       session,
		Questions:     questions,
		inbox:         make(chan sessionEvent, 64),
		state:         stateWaiting,
		timeRemaining: questionTimeLimitSec * time.Second,
	}
}

// AttachPlayer binds a WebSocket connection to the session.
func (gs *GameSession) AttachPlayer(conn *websocket.Conn, clientID int) {
	gs.PushEvent(eventAttach, NewPlayerConn(conn, clientID))
}

// DetachPlayer notifies the session that the player's connection has closed.
func (gs *GameSession) DetachPlayer(conn *websocket.Conn) {
	gs.PushEvent(eventDetach, conn)
}

// PushEvent sends an event to the session event loop.
func (gs *GameSession) PushEvent(eventType string, data interface{}) {
	select {
	case gs.inbox <- sessionEvent{eventType: eventType, data: data}:
	default:
		log.Printf("[ws] session %s: inbox full, dropping event %s", gs.ID, eventType)
	}
}

// SendError sends an error frame to the connected player if present.
func (gs *GameSession) SendError(code, message string) {
	gs.sendError(code, message)
}

// Stop cleanly cancels the session event loop.
func (gs *GameSession) Stop() {
	if gs.cancel != nil {
		gs.cancel()
	}
}

// Run starts the session event loop. This method blocks until the session ends
// or the context is cancelled. It must be called in its own goroutine.
func (gs *GameSession) Run(ctx context.Context) {
	ctx, gs.cancel = context.WithCancel(ctx)
	defer gs.cancel()

	qTimer := time.NewTimer(questionTimeLimitSec * time.Second)
	stopTimer(qTimer)
	defer qTimer.Stop()

	tTimer := time.NewTimer(transitionDelayMs * time.Millisecond)
	stopTimer(tTimer)
	defer tTimer.Stop()

	dTimer := time.NewTimer(disconnectTTL)
	stopTimer(dTimer)
	defer dTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			gs.handleGameEnd(ctx)
			return

		case <-qTimer.C:
			if gs.state == stateQuestionActive {
				gs.handleTimeout(ctx, tTimer)
				if gs.Session.Status != models.SessionStatusInProgress {
					return
				}
			}

		case <-tTimer.C:
			if gs.state == stateTransitioning {
				gs.handleTransitionDone(ctx, qTimer)
				if gs.Session.Status != models.SessionStatusInProgress {
					return
				}
			}

		case <-dTimer.C:
			if gs.handleDisconnectTimeout(ctx) {
				log.Printf("[ws] session %s: disconnected for %v, auto-terminating", gs.ID, disconnectTTL)
				return
			}
			// A player reattached before eviction fired; keep serving the session.

		case event := <-gs.inbox:
			switch event.eventType {
			case eventAttach:
				if player, ok := event.data.(*PlayerConn); ok {
					gs.onPlayerAttached(ctx, player, qTimer, tTimer, dTimer)
				}

			case eventDetach:
				if conn, ok := event.data.(*websocket.Conn); ok {
					gs.onPlayerDetached(conn, qTimer, dTimer)
				}

			case MsgTypeJoinGame:
				if rawData, ok := event.data.(json.RawMessage); ok {
					gs.handleJoinGame(rawData)
				}

			case MsgTypeSubmitAnswer:
				if rawData, ok := event.data.(json.RawMessage); ok {
					gs.handleAnswer(ctx, rawData, qTimer, tTimer)
					if gs.Session.Status != models.SessionStatusInProgress {
						return
					}
				}

			case MsgTypeUsePowerUp:
				if rawData, ok := event.data.(json.RawMessage); ok {
					gs.handleUsePowerUp(rawData)
				}
			}
		}
	}
}

// stopTimer safely stops and drains a timer channel to prevent leaks and phantom firings.
func stopTimer(t *time.Timer) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}
