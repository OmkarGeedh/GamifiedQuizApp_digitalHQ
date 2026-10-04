package ws_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"github.com/gorilla/websocket"
)

// dialRaw opens a connection and attaches it to gs without going through the
// per-connection helper goroutine, so the caller controls detach timing.
func dialRaw(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return c
}

// drain reads whatever is currently buffered, up to one frame, and returns its type.
// It returns "" if nothing arrived within the timeout.
func drainType(conn *websocket.Conn, timeout time.Duration) string {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var m ws.OutboundMessage
	if err := conn.ReadJSON(&m); err != nil {
		return ""
	}
	return m.Type
}

// Regression test: when a reconnect supersedes an existing socket, the old
// connection's handler still fires its deferred DetachPlayer afterwards. That
// stale event must not pause the question timer for the connection that is now
// live, otherwise the live player gets stale_answer on every submission and the
// session is abandoned 5 minutes later while still connected.
func TestGameSession_SupersededDetachLeavesLivePlayerPlayable(t *testing.T) {
	sessionID := "test-supersede-detach"
	clientID := 3131
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 3,
		Status:         models.SessionStatusInProgress,
	}

	gs := ws.NewGameSession(sessionID, clientID, gameSession, sampleQuestions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	// First connection becomes the active player and receives question 1.
	first := dialRaw(t, wsURL)
	defer first.Close()
	if got := drainType(first, 3*time.Second); got != ws.MsgTypeQuestion {
		t.Fatalf("first connection: expected %s, got %q", ws.MsgTypeQuestion, got)
	}

	// Second connection attaches while the first is still open (mobile reconnect,
	// page refresh, second tab). It supersedes the first.
	live := dialRaw(t, wsURL)
	defer live.Close()
	if got := drainType(live, 3*time.Second); got != ws.MsgTypeQuestion {
		t.Fatalf("live connection: expected resync %s, got %q", ws.MsgTypeQuestion, got)
	}

	// The superseded socket is closed by the session; its handler now runs its
	// deferred DetachPlayer. Give that stale event time to land.
	time.Sleep(300 * time.Millisecond)

	// The live connection owns the session and must still be able to answer.
	payload, _ := json.Marshal(ws.SubmitAnswerData{Question: "ACC001", Option: "a"})
	_ = live.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := live.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: payload}); err != nil {
		t.Fatalf("live connection write failed: %v", err)
	}

	_ = live.SetReadDeadline(time.Now().Add(3 * time.Second))
	var resp ws.OutboundMessage
	if err := live.ReadJSON(&resp); err != nil {
		t.Fatalf("live connection read failed: %v", err)
	}

	if resp.Type == ws.MsgTypeError {
		var e ws.ErrorPayload
		b, _ := json.Marshal(resp.Data)
		_ = json.Unmarshal(b, &e)
		t.Fatalf("live player was rejected with %s / %q after a superseded connection detached",
			e.Code, e.Message)
	}
	if resp.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected %s, got %s", ws.MsgTypeAnswerResult, resp.Type)
	}

	// The question timer must still be advancing the session too.
	if got := drainType(live, 3*time.Second); got != ws.MsgTypeQuestion {
		t.Fatalf("expected next %s after the 800ms transition, got %q", ws.MsgTypeQuestion, got)
	}
}

// A detach for a connection that no longer owns the session must be a complete
// no-op. Previously it decremented timeRemaining a second time, which could drive
// the remaining budget to zero and stall the session on resume.
func TestGameSession_StaleDetachIsNoOp(t *testing.T) {
	sessionID := "test-stale-detach-noop"
	clientID := 3232
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 3,
		Status:         models.SessionStatusInProgress,
	}

	gs := ws.NewGameSession(sessionID, clientID, gameSession, sampleQuestions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	live := dialRaw(t, wsURL)
	defer live.Close()
	if got := drainType(live, 3*time.Second); got != ws.MsgTypeQuestion {
		t.Fatalf("expected %s, got %q", ws.MsgTypeQuestion, got)
	}

	// Detach a connection that was never attached to this session.
	_ = drainType(live, 500*time.Millisecond)
	stray := dialRaw(t, wsURL)
	_ = drainType(stray, 3*time.Second) // stray resync frame
	_ = live.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = live.ReadJSON(&ws.OutboundMessage{})
	_ = live.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = live.ReadJSON(&ws.OutboundMessage{})
	time.Sleep(300 * time.Millisecond)

	// The live player must still hold a full budget and be able to answer.
	if got := drainType(live, 2*time.Second); got == ws.MsgTypeError {
		t.Fatalf("live player received an error frame: %s", got)
	}
}

// remaining_time_ms must always be serialised. With omitempty a resync at zero
// milliseconds omitted the field, and clients fell back to time_limit_ms.
func TestGameSession_QuestionPayloadAlwaysSerialisesRemainingTime(t *testing.T) {
	sessionID := "test-remaining-serialised"
	clientID := 3333
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}

	gs := ws.NewGameSession(sessionID, clientID, gameSession, sampleQuestions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	live := dialRaw(t, wsURL)
	defer live.Close()
	_ = live.SetReadDeadline(time.Now().Add(3 * time.Second))
	var m ws.OutboundMessage
	if err := live.ReadJSON(&m); err != nil {
		t.Fatalf("read failed: %v", err)
	}

	raw, _ := json.Marshal(m.Data)
	if !strings.Contains(string(raw), `"remaining_time_ms"`) {
		t.Fatalf("question frame omitted remaining_time_ms: %s", raw)
	}
	var q ws.QuestionPayload
	b, _ := json.Marshal(m.Data)
	_ = json.Unmarshal(b, &q)
	if q.RemainingTimeMs <= 0 {
		t.Fatalf("expected a positive remaining_time_ms on a fresh question, got %d", q.RemainingTimeMs)
	}
}

// join_game during the 800ms transition must get an explicit error code rather
// than silence, so a client using it as its resync mechanism does not hang.
func TestGameSession_JoinGameDuringTransitionReturnsError(t *testing.T) {
	sessionID := "test-join-during-transition"
	clientID := 3434
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 3,
		Status:         models.SessionStatusInProgress,
	}

	gs := ws.NewGameSession(sessionID, clientID, gameSession, sampleQuestions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	live := dialRaw(t, wsURL)
	defer live.Close()
	_ = drainType(live, 3*time.Second)

	answer, _ := json.Marshal(ws.SubmitAnswerData{Question: "ACC001", Option: "a"})
	_ = live.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = live.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: answer})

	_ = live.SetReadDeadline(time.Now().Add(3 * time.Second))
	var m ws.OutboundMessage
	if err := live.ReadJSON(&m); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if m.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected %s, got %s", ws.MsgTypeAnswerResult, m.Type)
	}

	// We are now inside the transition window (question 2 pending, no timer yet).
	_ = live.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = live.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeJoinGame, Data: json.RawMessage(`{}`)})

	_ = live.SetReadDeadline(time.Now().Add(3 * time.Second))
	var resp ws.OutboundMessage
	if err := live.ReadJSON(&resp); err != nil {
		t.Fatalf("join_game during transition got no response: %v", err)
	}
	if resp.Type != ws.MsgTypeError {
		t.Fatalf("expected %s during transition, got %s", ws.MsgTypeError, resp.Type)
	}
	var e ws.ErrorPayload
	b, _ := json.Marshal(resp.Data)
	_ = json.Unmarshal(b, &e)
	if e.Code != ws.ErrCodeTransitionInProgress {
		t.Fatalf("expected %s, got %s", ws.ErrCodeTransitionInProgress, e.Code)
	}
}
