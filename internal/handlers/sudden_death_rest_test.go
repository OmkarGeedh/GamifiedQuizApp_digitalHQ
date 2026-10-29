package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/gin-gonic/gin"
)

type turnEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func postTurn(t *testing.T, r *gin.Engine, token, path string, body any) (int, []turnEvent) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sudden-death/"+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp struct {
		Data struct {
			Events []turnEvent `json:"events"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp.Data.Events
}

func eventTypes(events []turnEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

func expectTypes(t *testing.T, events []turnEvent, want ...string) {
	t.Helper()
	got := eventTypes(events)
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func field[T any](t *testing.T, e turnEvent) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		t.Fatalf("decode %s: %v", e.Type, err)
	}
	return v
}

type sdQuestion struct {
	Question        string `json:"question"`
	RemainingTimeMs int    `json:"remaining_time_ms"`
	QuestionNumber  int    `json:"question_number"`
}

func TestSuddenDeathREST(t *testing.T) {
	r, token := setupTestApp(t)
	content := newContentFixture(t)
	for i := 1; i <= 4; i++ {
		content.SuddenDeath(i, "Question", "Right", "Wrong", "a")
	}

	start := func(t *testing.T) (string, map[string]string) {
		t.Helper()
		_ = repo.ClearClientActiveSession(context.Background(), 1)
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{TopicID: content.Topic, QuestionCount: 4, GameMode: models.GameModeSuddenDeath, AbandonStale: true})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data dto.SessionCreatedResponseDTO `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		s, _ := repo.GetSessionByID(context.Background(), resp.Data.Session)
		qs, _ := s.GetQuestions()
		correct := map[string]string{}
		for _, q := range qs {
			correct[q.QuestionCode] = q.CorrectOption
		}
		return resp.Data.Session, correct
	}
	wrongOf := func(c string) string {
		if c == "a" {
			return "b"
		}
		return "a"
	}

	t.Run("full run: resync, correct, skip, add_time, stale, wrong answer ends", func(t *testing.T) {
		session, correct := start(t)

		_, ev := postTurn(t, r, token, "question", map[string]string{"session": session})
		expectTypes(t, ev, "question")
		q1 := field[sdQuestion](t, ev[0])
		if q1.QuestionNumber != 1 || q1.RemainingTimeMs <= 14000 {
			t.Fatalf("unexpected first question %+v", q1)
		}
		_, ev = postTurn(t, r, token, "question", map[string]string{"session": session})
		if again := field[sdQuestion](t, ev[0]); again.Question != q1.Question || again.RemainingTimeMs > q1.RemainingTimeMs {
			t.Fatalf("resync must return the same running question, got %+v after %+v", again, q1)
		}

		_, ev = postTurn(t, r, token, "power-up", map[string]string{"session": session, "question": q1.Question, "power_up": "add_time"})
		expectTypes(t, ev, "power_up_result")
		if p := field[struct {
			AddedTimeMs     int `json:"added_time_ms"`
			RemainingTimeMs int `json:"remaining_time_ms"`
		}](t, ev[0]); p.AddedTimeMs != 5000 || p.RemainingTimeMs <= 15000 {
			t.Fatalf("add_time payload %+v", p)
		}
		_, ev = postTurn(t, r, token, "power-up", map[string]string{"session": session, "question": q1.Question, "power_up": "add_time"})
		expectTypes(t, ev, "error")
		if e := field[struct{ Code string }](t, ev[0]); e.Code != "power_up_already_used" {
			t.Fatalf("second add_time code = %s", e.Code)
		}

		_, ev = postTurn(t, r, token, "answer", map[string]any{"session": session, "question": q1.Question, "option": correct[q1.Question], "time_taken_ms": 1200})
		expectTypes(t, ev, "answer_result", "question")
		q2 := field[sdQuestion](t, ev[1])
		if q2.QuestionNumber != 2 {
			t.Fatalf("next question number = %d", q2.QuestionNumber)
		}

		_, ev = postTurn(t, r, token, "answer", map[string]any{"session": session, "question": q1.Question, "option": "a"})
		expectTypes(t, ev, "error")
		if e := field[struct{ Code string }](t, ev[0]); e.Code != "stale_answer" {
			t.Fatalf("stale code = %s", e.Code)
		}

		_, ev = postTurn(t, r, token, "answer", map[string]any{"session": session, "question": q2.Question, "option": "skip"})
		expectTypes(t, ev, "answer_result", "question")
		q3 := field[sdQuestion](t, ev[1])

		_, ev = postTurn(t, r, token, "answer", map[string]any{"session": session, "question": q3.Question, "option": wrongOf(correct[q3.Question])})
		expectTypes(t, ev, "answer_result", "game_over")
		over := field[struct {
			EndReason    string `json:"end_reason"`
			CorrectCount int    `json:"correct_count"`
			SkippedCount int    `json:"skipped_count"`
			FinalScore   int    `json:"final_score"`
		}](t, ev[1])
		if over.EndReason != "wrong_answer" || over.CorrectCount != 1 || over.SkippedCount != 1 || over.FinalScore != 10 {
			t.Fatalf("game over %+v", over)
		}

		_, ev = postTurn(t, r, token, "answer", map[string]any{"session": session, "question": q3.Question, "option": "a"})
		expectTypes(t, ev, "error")
		if e := field[struct{ Code string }](t, ev[0]); e.Code != "session_not_active" {
			t.Fatalf("after game over code = %s", e.Code)
		}
	})

	t.Run("expired deadline resolves as timeout on next request", func(t *testing.T) {
		session, _ := start(t)
		_, ev := postTurn(t, r, token, "question", map[string]string{"session": session})
		q1 := field[sdQuestion](t, ev[0])

		s, _ := repo.GetSessionByID(context.Background(), session)
		past := time.Now().Add(-10 * time.Second)
		s.QuestionDeadline = &past
		_ = repo.UpdateSession(context.Background(), s)

		_, ev = postTurn(t, r, token, "answer", map[string]any{"session": session, "question": q1.Question, "option": "a"})
		expectTypes(t, ev, "answer_result", "game_over")
		if res := field[struct {
			IsTimeout bool `json:"is_timeout"`
		}](t, ev[0]); !res.IsTimeout {
			t.Fatal("late answer must be a timeout")
		}
		if over := field[struct {
			EndReason string `json:"end_reason"`
		}](t, ev[1]); over.EndReason != "timeout" {
			t.Fatalf("end reason = %s", over.EndReason)
		}
	})

	t.Run("transport errors", func(t *testing.T) {
		if code, _ := postTurn(t, r, token, "question", map[string]string{"session": "00000000-0000-0000-0000-000000000000"}); code != http.StatusNotFound {
			t.Fatalf("unknown session status = %d", code)
		}
		session, _ := start(t)
		s, _ := repo.GetSessionByID(context.Background(), session)
		s.ClientID = 999999
		_ = repo.UpdateSession(context.Background(), s)
		if code, _ := postTurn(t, r, token, "question", map[string]string{"session": session}); code != http.StatusForbidden {
			t.Fatalf("foreign session status = %d", code)
		}
		_ = repo.ClearClientActiveSession(context.Background(), 1)
	})
}
