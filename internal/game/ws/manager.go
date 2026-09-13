package ws

import (
	"context"
	"log"
	"sync"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/models"
)

// SessionManager is a thread-safe registry of active WebSocket game sessions.
// It stores sessions in memory for the lifetime of the game; completed sessions
// are removed automatically.
type SessionManager struct {
	sessions map[string]*GameSession
	mu       sync.RWMutex
}

// NewSessionManager creates a new manager instance.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*GameSession),
	}
}

// GetOrCreate retrieves an existing active session or creates and starts a new one.
// Returns the session and true if a new session was started.
func (sm *SessionManager) GetOrCreate(ctx context.Context, sessionID string, clientID int, session *models.GameSession, questions []models.SessionQuestion) (*GameSession, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if gs, exists := sm.sessions[sessionID]; exists {
		return gs, false
	}

	gs := NewGameSession(sessionID, clientID, session, questions)
	sm.sessions[sessionID] = gs

	// Start the session event loop in a dedicated goroutine.
	// On completion, the session is automatically removed from the manager.
	go func() {
		gs.Run(ctx)
		sm.Remove(sessionID)
		log.Printf("[ws] session %s: event loop finished, removed from manager", sessionID)
	}()

	return gs, true
}

// Get retrieves an active session without creating one.
func (sm *SessionManager) Get(sessionID string) (*GameSession, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	gs, exists := sm.sessions[sessionID]
	return gs, exists
}

// Remove deletes a session from the manager.
func (sm *SessionManager) Remove(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, sessionID)
}

// ActiveCount returns the number of currently active sessions.
func (sm *SessionManager) ActiveCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}
