package ws_test

import (
	"context"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	ws "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"gorm.io/gorm"
)

// cacheTurnSession caches a three-question HTTP Sudden Death run for clientID.
func cacheTurnSession(t *testing.T, clientID int, id string) {
	t.Helper()
	s := &models.GameSession{ID: id, ClientID: clientID, GameMode: models.GameModeSuddenDeath,
		Status: models.SessionStatusInProgress, TotalQuestions: 3}
	if err := s.SetQuestions([]models.SessionQuestion{
		{QuestionCode: "q1", OptionA: "right", OptionB: "wrong", CorrectOption: "a"},
		{QuestionCode: "q2", OptionA: "right", OptionB: "wrong", CorrectOption: "a"},
		{QuestionCode: "q3", OptionA: "right", OptionB: "wrong", CorrectOption: "a"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CacheSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.InvalidateCachedSession(context.Background(), id) })
}

func coinsOf(t *testing.T, db *gorm.DB, clientID int) int {
	t.Helper()
	var c int
	if err := db.Raw(`SELECT coins FROM clients WHERE id = ?`, clientID).Scan(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func eventTypes(res *ws.TurnResponse) []string {
	var out []string
	for _, e := range res.Events {
		out = append(out, e.Type)
	}
	return out
}

func errorCode(res *ws.TurnResponse) string {
	for _, e := range res.Events {
		if e.Type == ws.MsgTypeError {
			if p, ok := e.Data.(ws.ErrorPayload); ok {
				return p.Code
			}
		}
	}
	return ""
}

func TestTurns_AddTimeIsPaidOnTheServer(t *testing.T) {
	db := testutil.FundedPlayer(t, 501, 5) // not enough for +5s (12 coins)
	cacheTurnSession(t, 501, "turn-add-time")
	ctx := context.Background()
	now := time.Now()
	if _, err := ws.CurrentTurn(ctx, 501, "turn-add-time", now); err != nil {
		t.Fatal(err)
	}
	before, _ := repo.GetSessionByID(ctx, "turn-add-time")
	deadline := *before.QuestionDeadline

	res, err := ws.UseTurnPowerUp(ctx, 501, "turn-add-time", ws.UsePowerUpData{Question: "q1", PowerUp: "add_time"}, now)
	if err != nil || errorCode(res) != "insufficient_funds" {
		t.Fatalf("unpaid +5s: %v %v", eventTypes(res), err)
	}
	after, _ := repo.GetSessionByID(ctx, "turn-add-time")
	if !after.QuestionDeadline.Equal(deadline) || after.AddTimeUsed {
		t.Fatal("unpaid +5s changed the clock")
	}

	db.Exec(`UPDATE clients SET coins = 100 WHERE id = 501`)
	res, err = ws.UseTurnPowerUp(ctx, 501, "turn-add-time", ws.UsePowerUpData{Question: "q1", PowerUp: "add_time"}, now)
	if err != nil || len(res.Events) != 1 || res.Events[0].Type != ws.MsgTypePowerUpResult {
		t.Fatalf("paid +5s: %v %v", eventTypes(res), err)
	}
	if p := res.Events[0].Data.(ws.PowerUpResultPayload); p.PaidWith != models.PayWithCoins || p.Cost != 12 || p.CoinsBalance == nil || *p.CoinsBalance != 88 {
		t.Errorf("power_up_result payment fields: %+v", p)
	}
	if c := coinsOf(t, db, 501); c != 88 {
		t.Errorf("coins = %d, want 88", c)
	}
	after, _ = repo.GetSessionByID(ctx, "turn-add-time")
	if got := after.QuestionDeadline.Sub(deadline); got != 5*time.Second {
		t.Errorf("deadline moved by %v, want 5s", got)
	}
	// Once per run, and never charged again.
	res, _ = ws.UseTurnPowerUp(ctx, 501, "turn-add-time", ws.UsePowerUpData{Question: "q1", PowerUp: "add_time"}, now)
	if errorCode(res) != ws.ErrCodePowerUpAlreadyUsed {
		t.Errorf("second +5s: %v", eventTypes(res))
	}
	if c := coinsOf(t, db, 501); c != 88 {
		t.Errorf("coins after second attempt = %d, want 88", c)
	}
}

func TestTurns_SkipIsPaidAndOncePerRun(t *testing.T) {
	db := testutil.FundedPlayer(t, 502, 10) // not enough for Skip (35 coins)
	cacheTurnSession(t, 502, "turn-skip")
	ctx := context.Background()
	now := time.Now()
	if _, err := ws.CurrentTurn(ctx, 502, "turn-skip", now); err != nil {
		t.Fatal(err)
	}
	res, _ := ws.SubmitTurn(ctx, 502, "turn-skip", ws.SubmitAnswerData{Question: "q1", Option: "skip"}, now)
	if errorCode(res) != "insufficient_funds" {
		t.Fatalf("unpaid skip: %v", eventTypes(res))
	}
	// The question is still live: a real answer works afterwards... but first pay for a skip.
	db.Exec(`UPDATE clients SET coins = 100 WHERE id = 502`)
	res, err := ws.SubmitTurn(ctx, 502, "turn-skip", ws.SubmitAnswerData{Question: "q1", Option: "skip"}, now)
	if err != nil || len(res.Events) < 2 || res.Events[0].Type != ws.MsgTypeAnswerResult || res.Events[1].Type != ws.MsgTypeQuestion {
		t.Fatalf("paid skip: %v %v", eventTypes(res), err)
	}
	if c := coinsOf(t, db, 502); c != 65 {
		t.Errorf("coins = %d, want 65", c)
	}
	// A second skip in the same run is refused without charging.
	res, _ = ws.SubmitTurn(ctx, 502, "turn-skip", ws.SubmitAnswerData{Question: "q2", Option: "skip"}, now)
	if errorCode(res) != ws.ErrCodePowerUpAlreadyUsed {
		t.Errorf("second skip: %v", eventTypes(res))
	}
	if c := coinsOf(t, db, 502); c != 65 {
		t.Errorf("coins after refused skip = %d, want 65", c)
	}
	// And q2 is still answerable.
	res, _ = ws.SubmitTurn(ctx, 502, "turn-skip", ws.SubmitAnswerData{Question: "q2", Option: "a"}, now)
	if len(res.Events) == 0 || res.Events[0].Type != ws.MsgTypeAnswerResult {
		t.Errorf("answer after refused skip: %v", eventTypes(res))
	}
}

func TestTurns_SkipWithToken(t *testing.T) {
	db := testutil.FundedPlayer(t, 503, 0)
	db.Exec(`INSERT INTO client_power_ups (client_id, power_up, quantity) VALUES (503, 'skip', 1)`)
	cacheTurnSession(t, 503, "turn-skip-token")
	ctx := context.Background()
	now := time.Now()
	_, _ = ws.CurrentTurn(ctx, 503, "turn-skip-token", now)
	res, err := ws.SubmitTurn(ctx, 503, "turn-skip-token", ws.SubmitAnswerData{Question: "q1", Option: "skip", PayWith: "token"}, now)
	if err != nil || len(res.Events) == 0 || res.Events[0].Type != ws.MsgTypeAnswerResult {
		t.Fatalf("token skip: %v %v", eventTypes(res), err)
	}
	var left int
	db.Raw(`SELECT quantity FROM client_power_ups WHERE client_id = 503 AND power_up = 'skip'`).Scan(&left)
	if left != 0 {
		t.Errorf("tokens left = %d, want 0", left)
	}
	res, _ = ws.UseTurnPowerUp(ctx, 503, "turn-skip-token", ws.UsePowerUpData{Question: "q2", PowerUp: "add_time", PayWith: "token"}, now)
	if errorCode(res) != "no_power_up_token" {
		t.Errorf("+5s with no token: %v", eventTypes(res))
	}
}
