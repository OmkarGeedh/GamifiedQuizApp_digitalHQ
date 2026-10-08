package ws_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	ws "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
)

// startSuddenDeath wires a session of the given mode up over a real WebSocket
// and returns the connected client.
func startSuddenDeath(t *testing.T, sessionID string, mode string, questions int) *websocket.Conn {
	t.Helper()

	gameSession := &models.GameSession{
		ID:             sessionID,
		ClientID:       77,
		GameMode:       mode,
		TotalQuestions: questions,
		Status:         models.SessionStatusInProgress,
	}
	gs := ws.NewGameSession(sessionID, gameSession.ClientID, gameSession, sampleQuestions())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go gs.Run(ctx)

	server, wsURL := setupTestServer(t, gs)
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// First frame is always question 1.
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected first %s frame, got %s", ws.MsgTypeQuestion, msg.Type)
	}
	return client
}

func submitOption(t *testing.T, client *websocket.Conn, question, option string) {
	t.Helper()
	data, _ := json.Marshal(ws.SubmitAnswerData{
		Question:    question,
		Option:      option,
		TimeTakenMs: 2000,
	})
	if err := client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeSubmitAnswer, Data: data}); err != nil {
		t.Fatalf("failed to write answer: %v", err)
	}
}

func readAnswerResult(t *testing.T, client *websocket.Conn) ws.AnswerResultPayload {
	t.Helper()
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("expected %s, got %s", ws.MsgTypeAnswerResult, msg.Type)
	}
	b, _ := json.Marshal(msg.Data)
	var payload ws.AnswerResultPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("failed to decode answer_result: %v", err)
	}
	return payload
}

func readGameOver(t *testing.T, client *websocket.Conn) ws.GameOverPayload {
	t.Helper()
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeGameOver {
		t.Fatalf("expected %s, got %s", ws.MsgTypeGameOver, msg.Type)
	}
	b, _ := json.Marshal(msg.Data)
	var payload ws.GameOverPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("failed to decode game_over: %v", err)
	}
	return payload
}

// A wrong answer must terminate a Sudden Death run instead of advancing.
func TestSuddenDeath_WrongAnswerEliminates(t *testing.T) {
	client := startSuddenDeath(t, "sd-wrong", models.GameModeSuddenDeath, 2)

	// ACC001's correct option is "a", so "b" is wrong.
	submitOption(t, client, "ACC001", "b")

	res := readAnswerResult(t, client)
	if res.IsCorrect || res.PointsEarned != 0 {
		t.Fatalf("expected an incorrect zero-point result, got %+v", res)
	}

	over := readGameOver(t, client)
	if over.EndReason != ws.EndReasonWrongAnswer {
		t.Fatalf("expected end_reason %q, got %q", ws.EndReasonWrongAnswer, over.EndReason)
	}
	if !over.EndedEarly {
		t.Fatal("expected ended_early to be true on elimination")
	}
	if over.CompletedSuccessfully {
		t.Fatal("wrong-answer elimination must not be completed successfully")
	}
	if over.GameMode != models.GameModeSuddenDeath {
		t.Fatalf("expected game_mode echoed as %q, got %q", models.GameModeSuddenDeath, over.GameMode)
	}
	// The run must stop on question 1, so nothing was scored.
	if over.CorrectCount != 0 || over.FinalScore != 0 {
		t.Fatalf("expected a zeroed run, got %+v", over)
	}
}

// Regression guard: after elimination the server must not serve question 2.
func TestSuddenDeath_NoFurtherQuestionsAfterElimination(t *testing.T) {
	client := startSuddenDeath(t, "sd-no-advance", models.GameModeSuddenDeath, 2)

	submitOption(t, client, "ACC001", "b")
	readAnswerResult(t, client)
	readGameOver(t, client)

	// The socket closes ~1s after game_over. Anything arriving before that
	// would mean the session kept playing.
	_ = client.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
	var msg ws.OutboundMessage
	if err := client.ReadJSON(&msg); err == nil {
		t.Fatalf("expected no further frames after elimination, got %s", msg.Type)
	}
}

// Clearing every question must report a clean sweep, not an elimination.
func TestSuddenDeath_ClearingAllQuestionsReportsCleared(t *testing.T) {
	client := startSuddenDeath(t, "sd-clear", models.GameModeSuddenDeath, 2)

	submitOption(t, client, "ACC001", "a")
	if res := readAnswerResult(t, client); !res.IsCorrect {
		t.Fatalf("expected ACC001 to be correct, got %+v", res)
	}

	// Surviving question 1 must still advance to question 2.
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected question 2 after a correct answer, got %s", msg.Type)
	}

	submitOption(t, client, "ACC002", "b")
	if res := readAnswerResult(t, client); !res.IsCorrect {
		t.Fatalf("expected ACC002 to be correct, got %+v", res)
	}

	over := readGameOver(t, client)
	if over.EndReason != ws.EndReasonCompleted {
		t.Fatalf("expected end_reason %q, got %q", ws.EndReasonCompleted, over.EndReason)
	}
	if over.EndedEarly {
		t.Fatal("expected ended_early to be false after clearing the set")
	}
	if over.CorrectCount != 2 || over.FinalScore != 20 {
		t.Fatalf("expected a full 2/2 run worth 20, got %+v", over)
	}
	if !over.CompletedSuccessfully || over.BestStreak != 2 {
		t.Fatalf("expected successful completion with best streak 2, got %+v", over)
	}
}

// A skip is a purchased power-up, not a mistake, so it must not eliminate.
func TestSuddenDeath_SkipDoesNotEliminate(t *testing.T) {
	client := startSuddenDeath(t, "sd-skip", models.GameModeSuddenDeath, 2)

	submitOption(t, client, "ACC001", "skip")

	res := readAnswerResult(t, client)
	if !res.IsSkipped {
		t.Fatalf("expected a skipped result, got %+v", res)
	}

	// The run continues to question 2 rather than ending.
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected question 2 after a skip, got %s", msg.Type)
	}

	submitOption(t, client, "ACC002", "b")
	if res := readAnswerResult(t, client); !res.IsCorrect {
		t.Fatalf("expected ACC002 to be correct, got %+v", res)
	}
	over := readGameOver(t, client)
	if over.SkippedCount != 1 || over.BestStreak != 1 || !over.CompletedSuccessfully {
		t.Fatalf("unexpected final skip/streak summary: %+v", over)
	}
}

// Regression guard: plain MCQ must keep advancing past wrong answers.
func TestMCQMode_WrongAnswerStillAdvances(t *testing.T) {
	client := startSuddenDeath(t, "mcq-wrong", models.GameModeMCQ, 2)

	submitOption(t, client, "ACC001", "b")
	if res := readAnswerResult(t, client); res.IsCorrect {
		t.Fatalf("expected an incorrect result, got %+v", res)
	}

	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("MCQ must advance past a wrong answer, got %s", msg.Type)
	}

	b, _ := json.Marshal(msg.Data)
	var q ws.QuestionPayload
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatalf("failed to decode question: %v", err)
	}
	if q.QuestionNumber != 2 {
		t.Fatalf("expected question 2, got %d", q.QuestionNumber)
	}
}

// Regression guard: sessions predating the game_mode column carry an empty
// string and must keep the non-eliminating behaviour.
func TestLegacyEmptyModeBehavesAsMCQ(t *testing.T) {
	client := startSuddenDeath(t, "legacy-empty-mode", "", 2)

	submitOption(t, client, "ACC001", "b")
	readAnswerResult(t, client)

	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("empty game_mode must not eliminate, got %s", msg.Type)
	}
}

// A full MCQ run that ends on the last question must report "cleared", not
// "eliminated", even though some answers were wrong.
func TestMCQMode_CompletionIsNotReportedAsElimination(t *testing.T) {
	client := startSuddenDeath(t, "mcq-complete", models.GameModeMCQ, 2)

	// Answer question 1 wrong, question 2 correctly.
	submitOption(t, client, "ACC001", "b")
	readAnswerResult(t, client)
	readNextMessage(t, client, 3*time.Second) // question 2

	submitOption(t, client, "ACC002", "b")
	readAnswerResult(t, client)

	over := readGameOver(t, client)
	if over.EndReason != ws.EndReasonCleared {
		t.Fatalf("expected %q for a completed MCQ run, got %q", ws.EndReasonCleared, over.EndReason)
	}
	if over.EndedEarly {
		t.Fatal("a completed MCQ run must not set ended_early")
	}
	if over.CorrectCount != 1 {
		t.Fatalf("expected 1 correct answer, got %d", over.CorrectCount)
	}
}

func TestSuddenDeath_AddTimeExtendsAuthoritativeTimerAndResync(t *testing.T) {
	client := startSuddenDeath(t, "sd-add-time", models.GameModeSuddenDeath, 2)

	powerUp, _ := json.Marshal(ws.UsePowerUpData{
		Session:  "sd-add-time",
		Question: "ACC001",
		PowerUp:  "add_time",
	})
	if err := client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeUsePowerUp, Data: powerUp}); err != nil {
		t.Fatalf("write add_time: %v", err)
	}
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypePowerUpResult {
		t.Fatalf("expected %s, got %s", ws.MsgTypePowerUpResult, msg.Type)
	}
	b, _ := json.Marshal(msg.Data)
	var result ws.PowerUpResultPayload
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("decode add_time result: %v", err)
	}
	if result.PowerUp != "add_time" || result.AddedTimeMs != 5000 {
		t.Fatalf("unexpected add_time confirmation: %+v", result)
	}
	if result.RemainingTimeMs <= 15000 || result.RemainingTimeMs > 20000 {
		t.Fatalf("expected authoritative extended time in (15000, 20000], got %d", result.RemainingTimeMs)
	}

	join, _ := json.Marshal(ws.JoinGameData{Session: "sd-add-time"})
	if err := client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeJoinGame, Data: join}); err != nil {
		t.Fatalf("write join_game: %v", err)
	}
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeQuestion {
		t.Fatalf("expected resync question, got %s", msg.Type)
	}
	b, _ = json.Marshal(msg.Data)
	var question ws.QuestionPayload
	_ = json.Unmarshal(b, &question)
	if question.RemainingTimeMs <= 15000 || question.RemainingTimeMs > result.RemainingTimeMs {
		t.Fatalf("resync did not preserve extended deadline: result=%d resync=%d", result.RemainingTimeMs, question.RemainingTimeMs)
	}

	if err := client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeUsePowerUp, Data: powerUp}); err != nil {
		t.Fatalf("write duplicate add_time: %v", err)
	}
	msg = readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeError {
		t.Fatalf("expected duplicate add_time error, got %s", msg.Type)
	}
	b, _ = json.Marshal(msg.Data)
	var wsErr ws.ErrorPayload
	_ = json.Unmarshal(b, &wsErr)
	if wsErr.Code != ws.ErrCodePowerUpAlreadyUsed {
		t.Fatalf("expected %s, got %s", ws.ErrCodePowerUpAlreadyUsed, wsErr.Code)
	}
}

func TestSuddenDeath_FiftyFiftyIsRejected(t *testing.T) {
	client := startSuddenDeath(t, "sd-no-fifty-fifty", models.GameModeSuddenDeath, 2)
	powerUp, _ := json.Marshal(ws.UsePowerUpData{
		Question: "ACC001",
		PowerUp:  "fifty_fifty",
	})
	if err := client.WriteJSON(ws.InboundMessage{Type: ws.MsgTypeUsePowerUp, Data: powerUp}); err != nil {
		t.Fatalf("write fifty_fifty: %v", err)
	}
	msg := readNextMessage(t, client, 3*time.Second)
	if msg.Type != ws.MsgTypeError {
		t.Fatalf("expected error, got %s", msg.Type)
	}
	b, _ := json.Marshal(msg.Data)
	var wsErr ws.ErrorPayload
	_ = json.Unmarshal(b, &wsErr)
	if wsErr.Code != ws.ErrCodeUnsupportedPowerUp {
		t.Fatalf("expected %s, got %s", ws.ErrCodeUnsupportedPowerUp, wsErr.Code)
	}
}
