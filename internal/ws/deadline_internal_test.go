package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func newDeadlineTestSession(t *testing.T, deadline time.Time) (*GameSession, *time.Timer, *time.Timer) {
	t.Helper()
	session := &models.GameSession{
		ID:             "deadline-test",
		ClientID:       42,
		GameMode:       models.GameModeSuddenDeath,
		TotalQuestions: 2,
		Status:         models.SessionStatusInProgress,
	}
	questions := []models.SessionQuestion{
		{QuestionCode: "sd-1", OptionA: "correct", OptionB: "wrong", CorrectOption: "a", Points: 10},
		{QuestionCode: "sd-2", OptionA: "wrong", OptionB: "correct", CorrectOption: "b", Points: 10},
	}
	gs := NewGameSession(session.ID, session.ClientID, session, questions)
	gs.state = stateQuestionActive
	gs.questionDeadline = deadline
	gs.timeRemaining = time.Until(deadline)
	qTimer := time.NewTimer(time.Hour)
	tTimer := time.NewTimer(time.Hour)
	t.Cleanup(func() {
		stopTimer(qTimer)
		stopTimer(tTimer)
	})
	return gs, qTimer, tTimer
}

func answerData(question, option, selectedText string) json.RawMessage {
	raw, _ := json.Marshal(SubmitAnswerData{
		Question:     question,
		Option:       option,
		SelectedText: selectedText,
	})
	return raw
}

func powerUpData(question string) json.RawMessage {
	raw, _ := json.Marshal(UsePowerUpData{Question: question, PowerUp: "add_time"})
	return raw
}

func TestDeadlineBoundary_AnswerAndSkip(t *testing.T) {
	t.Run("answer before deadline is accepted", func(t *testing.T) {
		gs, qTimer, tTimer := newDeadlineTestSession(t, time.Now().Add(time.Second))
		gs.handleAnswer(context.Background(), answerData("sd-1", "a", ""), qTimer, tTimer)
		if gs.Session.CorrectCount != 1 || gs.Session.CurrentIdx != 1 || gs.endReason != "" {
			t.Fatalf("answer before deadline was not accepted: %+v reason=%q", gs.Session, gs.endReason)
		}
	})

	t.Run("answer at deadline loses to timeout", func(t *testing.T) {
		gs, qTimer, tTimer := newDeadlineTestSession(t, time.Now())
		gs.handleAnswer(context.Background(), answerData("sd-1", "a", ""), qTimer, tTimer)
		if gs.Session.CorrectCount != 0 || gs.endReason != EndReasonTimeout || gs.state != stateEnded {
			t.Fatalf("timeout did not win: %+v reason=%q state=%d", gs.Session, gs.endReason, gs.state)
		}
	})

	t.Run("skip before deadline advances", func(t *testing.T) {
		gs, qTimer, tTimer := newDeadlineTestSession(t, time.Now().Add(time.Second))
		gs.handleAnswer(context.Background(), answerData("sd-1", "skip", ""), qTimer, tTimer)
		if gs.Session.SkippedCount != 1 || gs.Session.CurrentIdx != 1 || gs.Session.ComboStreak != 0 {
			t.Fatalf("skip before deadline did not advance exactly once: %+v", gs.Session)
		}
	})

	t.Run("skip at deadline loses to timeout", func(t *testing.T) {
		gs, qTimer, tTimer := newDeadlineTestSession(t, time.Now())
		gs.handleAnswer(context.Background(), answerData("sd-1", "skip", ""), qTimer, tTimer)
		if gs.Session.SkippedCount != 0 || gs.endReason != EndReasonTimeout {
			t.Fatalf("late skip bypassed timeout: %+v reason=%q", gs.Session, gs.endReason)
		}
	})
}

func TestDeadlineBoundary_AddTime(t *testing.T) {
	t.Run("add_time before deadline extends exactly five seconds", func(t *testing.T) {
		original := time.Now().Add(time.Second)
		gs, qTimer, tTimer := newDeadlineTestSession(t, original)
		gs.handleUsePowerUp(context.Background(), powerUpData("sd-1"), qTimer, tTimer)
		if delta := gs.questionDeadline.Sub(original); delta != addTimeDuration {
			t.Fatalf("expected exact %v extension, got %v", addTimeDuration, delta)
		}
		if !gs.addTimeUsed || gs.state != stateQuestionActive {
			t.Fatalf("successful add_time state not retained: used=%v state=%d", gs.addTimeUsed, gs.state)
		}
		if gs.expireIfDeadlineReached(context.Background(), original, qTimer, tTimer) {
			t.Fatal("original deadline still triggered timeout after add_time")
		}
		if !gs.expireIfDeadlineReached(context.Background(), original.Add(addTimeDuration), qTimer, tTimer) {
			t.Fatal("extended deadline did not trigger timeout")
		}
		if gs.endReason != EndReasonTimeout || gs.state != stateEnded {
			t.Fatalf("extended deadline did not end as timeout: reason=%q state=%d", gs.endReason, gs.state)
		}
	})

	t.Run("add_time at deadline loses to timeout", func(t *testing.T) {
		gs, qTimer, tTimer := newDeadlineTestSession(t, time.Now())
		gs.handleUsePowerUp(context.Background(), powerUpData("sd-1"), qTimer, tTimer)
		if gs.addTimeUsed || gs.endReason != EndReasonTimeout || gs.state != stateEnded {
			t.Fatalf("late add_time bypassed timeout: used=%v reason=%q state=%d", gs.addTimeUsed, gs.endReason, gs.state)
		}
	})
}

func TestGameEndIsIdempotent(t *testing.T) {
	gs, _, _ := newDeadlineTestSession(t, time.Now().Add(time.Second))
	gs.endReason = EndReasonWrongAnswer
	gs.handleGameEnd(context.Background())
	endedAt := gs.Session.EndedAt
	gs.handleGameEnd(context.Background())
	if gs.state != stateEnded || gs.Session.EndedAt != endedAt || gs.Session.EndReason != EndReasonWrongAnswer {
		t.Fatalf("second termination mutated final state: %+v", gs.Session)
	}
}
