package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func TestCacheKey(t *testing.T) {
	key := cacheKey("sess-abc-123")
	expected := "quiz:session:cache:sess-abc-123"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

func TestSessionCache_RedisNilGraceful(t *testing.T) {
	// Ensure config.RedisClient is nil
	origClient := config.RedisClient
	config.RedisClient = nil
	defer func() { config.RedisClient = origClient }()

	ctx := context.Background()
	session := &models.GameSession{
		ID:             "test-session-id",
		ClientID:       42,
		GameMode:       models.GameModeSortItOut,
		QuestionsState: `[{"question_code":"q1","prompt":"P1"}]`,
	}

	if err := CacheSession(ctx, session); err != nil {
		t.Fatalf("expected nil error when Redis is nil, got %v", err)
	}

	cached, ok := GetCachedSession(ctx, session.ID)
	if ok || cached != nil {
		t.Fatalf("expected ok=false and cached=nil when Redis is nil")
	}

	if err := RefreshSessionCacheTTL(ctx, session.ID); err != nil {
		t.Fatalf("expected nil error on refresh when Redis is nil, got %v", err)
	}

	if err := InvalidateCachedSession(ctx, session.ID); err != nil {
		t.Fatalf("expected nil error on invalidate when Redis is nil, got %v", err)
	}
}

func TestCachedSessionWrapper_QuestionsStatePreserved(t *testing.T) {
	now := time.Now().UTC()
	session := models.GameSession{
		ID:             "sess-xyz",
		ClientID:       10,
		TopicID:        "accounting",
		GameMode:       models.GameModeSortItOut,
		PairType:       "credit_debit",
		TotalQuestions: 5,
		Status:         models.SessionStatusInProgress,
		QuestionsState: `[{"question_code":"sio_cd_001","prompt":"Prompt 1","pair_type":"credit_debit"}]`,
		StartedAt:      now,
	}

	wrapper := cachedSessionWrapper{
		Session:        session,
		QuestionsState: session.QuestionsState,
	}

	data, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored cachedSessionWrapper
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	restoredSession := restored.Session
	restoredSession.QuestionsState = restored.QuestionsState

	if restoredSession.ID != session.ID {
		t.Errorf("expected ID %s, got %s", session.ID, restoredSession.ID)
	}
	if restoredSession.PairType != "credit_debit" {
		t.Errorf("expected PairType credit_debit, got %s", restoredSession.PairType)
	}
	if restoredSession.QuestionsState != session.QuestionsState {
		t.Errorf("QuestionsState lost in serialization! Expected: %s, Got: %s", session.QuestionsState, restoredSession.QuestionsState)
	}

	questions, err := restoredSession.GetQuestions()
	if err != nil || len(questions) != 1 {
		t.Fatalf("failed to decode questions from restored session: %v", err)
	}
	if questions[0].QuestionCode != "sio_cd_001" {
		t.Errorf("expected question code sio_cd_001, got %s", questions[0].QuestionCode)
	}
}
