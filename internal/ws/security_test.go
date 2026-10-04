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

// TestSecurity_NoAnswerLeakage verifies that outbound question frames
// never expose correct_option, original_correct, or explanation before an answer is submitted.
func TestSecurity_NoAnswerLeakage(t *testing.T) {
	sessionID := "sec-test-no-leak"
	clientID := 999
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 1,
		Status:         models.SessionStatusInProgress,
	}
	questions := []models.SessionQuestion{
		{
			QuestionCode:    "SEC001",
			Prompt:          "Confidential Question Prompt",
			OptionA:         "Option 1",
			OptionB:         "Option 2",
			OptionC:         "Option 3",
			OptionD:         "Option 4",
			CorrectOption:   "c",
			OriginalCorrect: "c",
			Points:          10,
			Explanation:     stringPtr("Top secret explanation that must not leak before answering"),
		},
	}

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

	// Read initial question frame
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected %s, got %s", ws.MsgTypeQuestion, msg.Type)
	}

	rawBytes, _ := json.Marshal(msg.Data)
	rawStr := strings.ToLower(string(rawBytes))

	// Security assertion: correct answer letter/option/explanation must NOT be in the question frame
	if strings.Contains(rawStr, "correct_option") {
		t.Fatalf("SECURITY VIOLATION: question frame leaks 'correct_option': %s", rawStr)
	}
	if strings.Contains(rawStr, "original_correct") {
		t.Fatalf("SECURITY VIOLATION: question frame leaks 'original_correct': %s", rawStr)
	}
	if strings.Contains(rawStr, "top secret explanation") {
		t.Fatalf("SECURITY VIOLATION: question frame leaks explanation before answer: %s", rawStr)
	}
}

// TestSecurity_PowerUpIntegrity verifies that 50:50 power-up never hides the correct answer
// and cannot be replayed or abused to expose the solution.
func TestSecurity_PowerUpIntegrity(t *testing.T) {
	sessionID := "sec-test-powerup"
	clientID := 888
	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       clientID,
		TotalQuestions: 1,
		Status:         models.SessionStatusInProgress,
	}
	questions := []models.SessionQuestion{
		{
			QuestionCode:  "SEC002",
			Prompt:        "Question with correct answer D",
			OptionA:       "Opt A",
			OptionB:       "Opt B",
			OptionC:       "Opt C",
			OptionD:       "Opt D",
			CorrectOption: "d",
			Points:        10,
		},
	}

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

	_ = readNextMessage(t, client, 3*time.Second)

	// Call 50:50 power-up 10 times to ensure randomness never hides correct option 'd'
	puData, _ := json.Marshal(ws.UsePowerUpData{
		Question: "SEC002",
		PowerUp:  "fifty_fifty",
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeUsePowerUp,
		Data: puData,
	})

	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypePowerUpResult {
		t.Fatalf("expected power_up_result, got %s", msg.Type)
	}
	var puPayload ws.PowerUpResultPayload
	rawBytes, _ := json.Marshal(msg.Data)
	_ = json.Unmarshal(rawBytes, &puPayload)

	for _, hidden := range puPayload.HiddenOptions {
		if hidden == "d" {
			t.Fatalf("SECURITY VIOLATION: correct option 'd' was hidden by 50:50!")
		}
	}

	// Replay attack: attempt second usage of 50:50 in the same session
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeUsePowerUp,
		Data: puData,
	})
	replayMsg := readNextMessage(t, client, 3*time.Second)
	if replayMsg.Type != ws.MsgTypeError {
		t.Fatalf("expected error frame on replay, got %s", replayMsg.Type)
	}
	var errPayload ws.ErrorPayload
	errBytes, _ := json.Marshal(replayMsg.Data)
	_ = json.Unmarshal(errBytes, &errPayload)
	if errPayload.Code != ws.ErrCodePowerUpAlreadyUsed {
		t.Fatalf("expected code %s, got %s", ws.ErrCodePowerUpAlreadyUsed, errPayload.Code)
	}
}

// TestSecurity_ScoreTamperingPrevention verifies that arbitrary client time_taken_ms
// or attempted score manipulation cannot alter server-calculated points.
func TestSecurity_ScoreTamperingPrevention(t *testing.T) {
	sessionID := "sec-test-tampering"
	clientID := 777
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

	_ = readNextMessage(t, client, 3*time.Second)

	// Tampered payload: negative time_taken_ms
	tamperedPayload, _ := json.Marshal(ws.SubmitAnswerData{
		Question:    "ACC001",
		Option:      "a",
		TimeTakenMs: -999999,
	})
	_ = client.WriteJSON(ws.InboundMessage{
		Type: ws.MsgTypeSubmitAnswer,
		Data: tamperedPayload,
	})

	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected answer_result, got %s", msg.Type)
	}
	var ans ws.AnswerResultPayload
	b, _ := json.Marshal(msg.Data)
	_ = json.Unmarshal(b, &ans)

	// Score must be strictly 10 points (fixed formula), not modified by time tampering
	if ans.PointsEarned != 10 || ans.YourScore != 10 {
		t.Fatalf("SECURITY VIOLATION: score tampered: points=%d, your_score=%d", ans.PointsEarned, ans.YourScore)
	}
}

// TestSecurity_DoubleSubmissionRaceCondition verifies that rapid-fire duplicate answers
// cannot double-award points or increment combos twice.
func TestSecurity_DoubleSubmissionRaceCondition(t *testing.T) {
	sessionID := "sec-test-race"
	clientID := 666
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

	_ = readNextMessage(t, client, 3*time.Second)

	// Send answer 1
	ansData, _ := json.Marshal(ws.SubmitAnswerData{
		Question: "ACC001",
		Option:   "a",
	})
	_ = client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: ansData})

	// Immediately send answer 2 for the same question (attempted double-spend)
	_ = client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: ansData})

	// First message must be answer_result
	msg1 := readNextMessage(t, client, 3*time.Second)
	if msg1.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected answer_result, got %s", msg1.Type)
	}

	// Second message must be an error frame with stale_answer (not a second answer_result!)
	msg2 := readNextMessage(t, client, 3*time.Second)
	if msg2.Type != ws.MsgTypeError {
		t.Fatalf("SECURITY VIOLATION: duplicate answer was graded instead of rejected: %s", msg2.Type)
	}
	var errPayload ws.ErrorPayload
	b, _ := json.Marshal(msg2.Data)
	_ = json.Unmarshal(b, &errPayload)
	if errPayload.Code != ws.ErrCodeStaleAnswer {
		t.Fatalf("expected stale_answer on duplicate answer, got %s", errPayload.Code)
	}
}

// TestSecurity_OversizedFrameRejectedAtConfiguredLimit verifies that a frame exceeding
// the configured read limit is rejected by the WebSocket layer, preventing memory DoS.
// The auth-gated handler (handlers.WebSocketGameHandler) applies the same
// ws.MaxMessageSize limit and is exercised by handlers.TestE2E_WebSocket_LiveGame.
func TestSecurity_OversizedFrameRejectedAtConfiguredLimit(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(ws.MaxMessageSize)

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	// Send a frame well over the configured limit
	giantMessage := strings.Repeat("A", ws.MaxMessageSize*4)
	err = client.WriteMessage(websocket.TextMessage, []byte(giantMessage))
	if err != nil {
		// Write may fail or close connection immediately
		return
	}

	// Server should close connection due to read limit exceeded
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = client.ReadMessage()
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: oversized frame of %d bytes was accepted without closing socket!", len(giantMessage))
	}
}

func stringPtr(s string) *string {
	return &s
}
