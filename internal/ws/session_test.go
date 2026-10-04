package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func sampleQuestions() []models.SessionQuestion {
	return []models.SessionQuestion{
		{
			QuestionCode:  "ACC001",
			Prompt:        "Which is an asset?",
			OptionA:       "Cash",
			OptionB:       "Accounts Payable",
			OptionC:       "Loan",
			OptionD:       "Mortgage",
			CorrectOption: "a",
			Difficulty:    1,
			Points:        10,
		},
		{
			QuestionCode:  "ACC002",
			Prompt:        "Which is a liability?",
			OptionA:       "Equipment",
			OptionB:       "Notes Payable",
			OptionC:       "Inventory",
			OptionD:       "Building",
			CorrectOption: "b",
			Difficulty:    1,
			Points:        10,
		},
	}
}

func setupTestServer(t *testing.T, gs *ws.GameSession) (*httptest.Server, string) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		gs.AttachPlayer(conn, gs.ClientID)
		defer gs.DetachPlayer(conn)

		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg ws.InboundMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				gs.SendError(ws.ErrCodeInvalidPayload, "invalid JSON payload")
				continue
			}
			gs.PushEvent(msg.Type, msg.Data)
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	return s, wsURL
}

func readNextMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) ws.OutboundMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var msg ws.OutboundMessage
	err := conn.ReadJSON(&msg)
	if err != nil {
		t.Fatalf("failed to read JSON message: %v", err)
	}
	return msg
}

func TestGameSession_BasicQuizFlow(t *testing.T) {
	sessionID := "test-session-1"
	clientID := 42
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}
	questions := sampleQuestions()

	gs := ws.NewGameSession(sessionID, clientID, gameSession, questions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	// 1. First frame: Question 1
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected type %s, got %s", ws.MsgTypeQuestion, msg.Type)
	}
	qBytes, _ := json.Marshal(msg.Data)
	var qPayload ws.QuestionPayload
	_ = json.Unmarshal(qBytes, &qPayload)
	if qPayload.Question != "ACC001" || qPayload.QuestionNumber != 1 {
		t.Fatalf("unexpected question payload: %+v", qPayload)
	}
	if qPayload.TimeLimitMs != 15000 {
		t.Fatalf("expected time_limit_ms 15000, got %d", qPayload.TimeLimitMs)
	}

	// 2. Submit correct answer for Question 1
	ansData, _ := json.Marshal(ws.SubmitAnswerData{
		Question:    "ACC001",
		Option:      "a",
		TimeTakenMs: 2000,
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeSubmitAnswer,
		Data: ansData,
	})

	// 3. Receive answer_result
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected type %s, got %s", ws.MsgTypeAnswerResult, msg.Type)
	}
	ansBytes, _ := json.Marshal(msg.Data)
	var ansPayload ws.AnswerResultPayload
	_ = json.Unmarshal(ansBytes, &ansPayload)
	if !ansPayload.IsCorrect || ansPayload.IsSkipped || ansPayload.PointsEarned != 10 || ansPayload.CoinsEarned != 0 {
		t.Fatalf("unexpected answer result: %+v", ansPayload)
	}

	// 4. After 800ms transition delay, receive Question 2
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected second question, got %s", msg.Type)
	}
	qBytes, _ = json.Marshal(msg.Data)
	_ = json.Unmarshal(qBytes, &qPayload)
	if qPayload.Question != "ACC002" || qPayload.QuestionNumber != 2 {
		t.Fatalf("unexpected question 2 payload: %+v", qPayload)
	}

	// 5. Submit "skip" for Question 2
	skipData, _ := json.Marshal(ws.SubmitAnswerData{
		Question:    "ACC002",
		Option:      "skip",
		TimeTakenMs: 1500,
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeSubmitAnswer,
		Data: skipData,
	})

	// 6. Receive answer_result for skip
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected type %s, got %s", ws.MsgTypeAnswerResult, msg.Type)
	}
	ansBytes, _ = json.Marshal(msg.Data)
	_ = json.Unmarshal(ansBytes, &ansPayload)
	if ansPayload.IsCorrect || !ansPayload.IsSkipped || ansPayload.PointsEarned != 0 {
		t.Fatalf("expected skipped answer result: %+v", ansPayload)
	}

	// 7. Receive game_over
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeGameOver {
		t.Fatalf("expected type %s, got %s", ws.MsgTypeGameOver, msg.Type)
	}
	goBytes, _ := json.Marshal(msg.Data)
	var goPayload ws.GameOverPayload
	_ = json.Unmarshal(goBytes, &goPayload)
	if goPayload.FinalScore != 10 || goPayload.TotalQuestions != 2 || goPayload.CorrectCount != 1 {
		t.Fatalf("unexpected game_over payload: %+v", goPayload)
	}
}

func TestGameSession_5050PowerUp(t *testing.T) {
	sessionID := "test-session-powerup"
	clientID := 101
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}
	questions := sampleQuestions()

	gs := ws.NewGameSession(sessionID, clientID, gameSession, questions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	// 1. Receive Question 1
	_ = readNextMessage(t, client, 3*time.Second)

	// 2. Use 50:50 power-up
	puData, _ := json.Marshal(ws.UsePowerUpData{
		Question: "ACC001",
		PowerUp:  "fifty_fifty",
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeUsePowerUp,
		Data: puData,
	})

	// 3. Receive power_up_result
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypePowerUpResult {
		t.Fatalf("expected %s, got %s", ws.MsgTypePowerUpResult, msg.Type)
	}
	puBytes, _ := json.Marshal(msg.Data)
	var puPayload ws.PowerUpResultPayload
	_ = json.Unmarshal(puBytes, &puPayload)
	if len(puPayload.HiddenOptions) != 2 {
		t.Fatalf("expected 2 hidden options, got %d", len(puPayload.HiddenOptions))
	}
	for _, hidden := range puPayload.HiddenOptions {
		if hidden == "a" {
			t.Fatalf("correct option 'a' was hidden!")
		}
	}

	// 4. Attempt to use 50:50 a second time in same session -> power_up_already_used
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeUsePowerUp,
		Data: puData,
	})
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeError {
		t.Fatalf("expected error frame, got %s", msg.Type)
	}
	errBytes, _ := json.Marshal(msg.Data)
	var errPayload ws.ErrorPayload
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodePowerUpAlreadyUsed {
		t.Fatalf("expected code %s, got %s", ws.ErrCodePowerUpAlreadyUsed, errPayload.Code)
	}

	// 5. Attempt unsupported power-up
	badData, _ := json.Marshal(ws.UsePowerUpData{
		Question: "ACC001",
		PowerUp:  "hint",
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeUsePowerUp,
		Data: badData,
	})
	msg = readNextMessage(t, client, 3*time.Second)
	errBytes, _ = json.Marshal(msg.Data)
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodeUnsupportedPowerUp {
		t.Fatalf("expected code %s, got %s", ws.ErrCodeUnsupportedPowerUp, errPayload.Code)
	}
}

func TestGameSession_JoinGameResync(t *testing.T) {
	sessionID := "test-session-join"
	clientID := 202
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}
	questions := sampleQuestions()

	gs := ws.NewGameSession(sessionID, clientID, gameSession, questions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	// 1. Initial Question 1
	_ = readNextMessage(t, client, 3*time.Second)

	// 2. Send join_game for resync
	joinData, _ := json.Marshal(ws.JoinGameData{
		Session: sessionID,
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeJoinGame,
		Data: joinData,
	})

	// 3. Receive resynced Question 1
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected question resync, got %s", msg.Type)
	}
	qBytes, _ := json.Marshal(msg.Data)
	var qPayload ws.QuestionPayload
	_ = json.Unmarshal(qBytes, &qPayload)
	if qPayload.Question != "ACC001" {
		t.Fatalf("expected ACC001, got %s", qPayload.Question)
	}
	if qPayload.RemainingTimeMs <= 0 || qPayload.RemainingTimeMs > 15000 {
		t.Fatalf("unexpected remaining_time_ms: %d", qPayload.RemainingTimeMs)
	}

	// 4. Send join_game with mismatched session ID -> session_mismatch
	badJoin, _ := json.Marshal(ws.JoinGameData{
		Session: "different-session-uuid",
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeJoinGame,
		Data: badJoin,
	})
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeError {
		t.Fatalf("expected error frame, got %s", msg.Type)
	}
	errBytes, _ := json.Marshal(msg.Data)
	var errPayload ws.ErrorPayload
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodeSessionMismatch {
		t.Fatalf("expected code %s, got %s", ws.ErrCodeSessionMismatch, errPayload.Code)
	}
}

func TestGameSession_InputValidation(t *testing.T) {
	sessionID := "test-session-val"
	clientID := 303
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}
	questions := sampleQuestions()

	gs := ws.NewGameSession(sessionID, clientID, gameSession, questions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	// Initial Question
	_ = readNextMessage(t, client, 3*time.Second)

	// Case 1: missing_question
	data1, _ := json.Marshal(ws.SubmitAnswerData{
		Option: "a",
	})
	_ = client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: data1})
	msg := readNextMessage(t, client, 3*time.Second)
	var errPayload ws.ErrorPayload
	errBytes, _ := json.Marshal(msg.Data)
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodeMissingQuestion {
		t.Fatalf("expected code %s, got %s", ws.ErrCodeMissingQuestion, errPayload.Code)
	}

	// Case 2: stale_answer (wrong question code)
	data2, _ := json.Marshal(ws.SubmitAnswerData{
		Question: "ACC999",
		Option:   "a",
	})
	_ = client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: data2})
	msg = readNextMessage(t, client, 3*time.Second)
	errBytes, _ = json.Marshal(msg.Data)
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodeStaleAnswer {
		t.Fatalf("expected code %s, got %s", ws.ErrCodeStaleAnswer, errPayload.Code)
	}

	// Case 3: missing_option
	data3, _ := json.Marshal(ws.SubmitAnswerData{
		Question: "ACC001",
		Option:   "",
	})
	_ = client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: data3})
	msg = readNextMessage(t, client, 3*time.Second)
	errBytes, _ = json.Marshal(msg.Data)
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodeMissingOption {
		t.Fatalf("expected code %s, got %s", ws.ErrCodeMissingOption, errPayload.Code)
	}

	// Case 4: session_mismatch
	data4, _ := json.Marshal(ws.SubmitAnswerData{
		Session:  "wrong-id",
		Question: "ACC001",
		Option:   "a",
	})
	_ = client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: data4})
	msg = readNextMessage(t, client, 3*time.Second)
	errBytes, _ = json.Marshal(msg.Data)
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodeSessionMismatch {
		t.Fatalf("expected code %s, got %s", ws.ErrCodeSessionMismatch, errPayload.Code)
	}
}

func TestGameSession_TimerPauseAndResumeOnDisconnect(t *testing.T) {
	sessionID := "test-session-pause"
	clientID := 404
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}
	questions := sampleQuestions()

	gs := ws.NewGameSession(sessionID, clientID, gameSession, questions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	defer server.Close()

	// 1. Connect client 1
	client1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	// Read initial question
	msg := readNextMessage(t, client1, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected question, got %s", msg.Type)
	}

	// Let 200ms pass, then close client 1
	time.Sleep(200 * time.Millisecond)
	_ = client1.Close()

	// Sleep 300ms while disconnected
	time.Sleep(300 * time.Millisecond)

	// 2. Connect client 2 (reconnect)
	client2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client2.Close()

	// 3. Client 2 should receive resumed question with remaining time
	msg = readNextMessage(t, client2, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected question on reconnect, got %s", msg.Type)
	}
	qBytes, _ := json.Marshal(msg.Data)
	var qPayload ws.QuestionPayload
	_ = json.Unmarshal(qBytes, &qPayload)
	if qPayload.Question != "ACC001" {
		t.Fatalf("expected question ACC001, got %s", qPayload.Question)
	}
	if qPayload.RemainingTimeMs <= 0 || qPayload.RemainingTimeMs > 15000 {
		t.Fatalf("unexpected remaining_time_ms: %d", qPayload.RemainingTimeMs)
	}
}
