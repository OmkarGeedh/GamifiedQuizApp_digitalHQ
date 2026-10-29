package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// The client grades MCQ locally and sends every answer with complete: one request.
func TestCompleteSession_BatchedAnswers(t *testing.T) {
	r, token := setupTestApp(t)
	_ = repo.ClearClientActiveSession(context.Background(), 1)
	content := newContentFixture(t)
	for n := 1; n <= 3; n++ {
		content.MCQ(n, fmt.Sprintf("Question %d?", n), "Alpha", "Beta", "Gamma", "Delta", "a")
	}

	post := func(path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := post("/api/v1/quiz/sessions/create", dto.CreateSessionRequestDTO{TopicID: content.Topic, QuestionCount: 3, AbandonStale: true})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data dto.SessionCreatedResponseDTO `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	qs := created.Data.Questions

	// First answer goes through /evaluate (old client path); the batch repeats it and
	// adds two more, one of them wrong. The repeat must not count twice.
	if w := post("/api/v1/quiz/answers/evaluate", dto.SubmitAnswerRequestDTO{Session: created.Data.Session, Question: qs[0].Question, Option: qs[0].CorrectOption}); w.Code != http.StatusOK {
		t.Fatalf("evaluate: %d %s", w.Code, w.Body.String())
	}
	wrong := "a"
	if qs[2].CorrectOption == "a" {
		wrong = "b"
	}
	answers := []dto.SessionAnswerDTO{
		{Question: qs[0].Question, Option: qs[0].CorrectOption, TimeTakenMs: 900},
		{Question: qs[1].Question, Option: qs[1].CorrectOption, TimeTakenMs: 1200},
		{Question: qs[2].Question, Option: wrong, TimeTakenMs: 1500},
	}
	w = post("/api/v1/quiz/sessions/complete", dto.CompleteSessionRequestDTO{Session: created.Data.Session, Answers: answers})
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
	var done struct {
		Data dto.SessionCompleteResponseDTO `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &done)
	if done.Data.CorrectCount != 2 || done.Data.FinalScore != 20 {
		t.Fatalf("expected 2 correct / score 20, got %d / %d", done.Data.CorrectCount, done.Data.FinalScore)
	}

	var history []models.UserQuestionHistory
	config.DB.Where("session_id = ?", created.Data.Session).Find(&history)
	if len(history) != 3 {
		t.Fatalf("expected 3 history rows, got %d", len(history))
	}
	var results int64
	config.DB.Model(&models.LeaderboardGameResult{}).Where("session_id = ?", created.Data.Session).Count(&results)
	if results != 1 {
		t.Fatalf("expected 1 leaderboard row, got %d", results)
	}
}
