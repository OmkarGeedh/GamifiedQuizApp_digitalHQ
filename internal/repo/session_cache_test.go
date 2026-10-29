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
		ID:             "test-session-id-nil-redis",
		ClientID:       42,
		GameMode:       models.GameModeSortItOut,
		QuestionsState: `[{"question_code":"q1","prompt":"P1"}]`,
	}

	if err := CacheSession(ctx, session); err != nil {
		t.Fatalf("expected nil error when Redis is nil, got %v", err)
	}

	cached, ok := GetCachedSession(ctx, session.ID)
	if !ok || cached == nil {
		t.Fatalf("expected ok=true and cached!=nil from in-memory fallback when Redis is nil")
	}
	if cached.ClientID != 42 {
		t.Errorf("expected clientID 42, got %d", cached.ClientID)
	}

	// Active exclusion lock test
	if err := SetClientActiveSession(ctx, 42, session.ID, models.GameModeSortItOut); err != nil {
		t.Fatalf("expected nil error setting active pointer: %v", err)
	}
	active, found := GetClientActiveSession(ctx, 42)
	if !found || active == nil || active.SessionID != session.ID {
		t.Fatalf("expected active session pointer found with session ID %s", session.ID)
	}
	_ = ClearClientActiveSession(ctx, 42)
	_, foundAfterClear := GetClientActiveSession(ctx, 42)
	if foundAfterClear {
		t.Errorf("expected active pointer to be cleared")
	}

	if err := RefreshSessionCacheTTL(ctx, session.ID); err != nil {
		t.Fatalf("expected nil error on refresh when Redis is nil, got %v", err)
	}

	if err := InvalidateCachedSession(ctx, session.ID); err != nil {
		t.Fatalf("expected nil error on invalidate when Redis is nil, got %v", err)
	}

	_, okAfterDelete := GetCachedSession(ctx, session.ID)
	if okAfterDelete {
		t.Errorf("expected ok=false after invalidation")
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

func TestCachedSessionWrapper_MatchPairsPreserved(t *testing.T) {
	now := time.Now().UTC()
	sq := models.SessionQuestion{
		QuestionCode: "mtf_acc_001",
		Prompt:       "Match Accounts",
		MatchPairs: []models.MatchPair{
			{ID: "acc11-01-mf-0001", LeftText: "Cash", RightText: "Real Account"},
			{ID: "acc11-01-mf-0002", LeftText: "Salary", RightText: "Nominal Account"},
		},
	}
	encoded, err := json.Marshal([]models.SessionQuestion{sq})
	if err != nil {
		t.Fatalf("failed to encode questions: %v", err)
	}

	session := models.GameSession{
		ID:             "sess-mtf-001",
		ClientID:       123,
		TopicID:        "accounting",
		GameMode:       models.GameModeMatchTheFollowing,
		TotalQuestions: 1,
		Status:         models.SessionStatusInProgress,
		QuestionsState: string(encoded),
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

	if !restoredSession.IsMatchTheFollowing() {
		t.Fatal("expected IsMatchTheFollowing() to be true")
	}

	questions, err := restoredSession.GetQuestions()
	if err != nil || len(questions) != 1 {
		t.Fatalf("failed to decode questions: %v", err)
	}

	if len(questions[0].MatchPairs) != 2 {
		t.Fatalf("expected 2 match pairs, got %d", len(questions[0].MatchPairs))
	}
	if questions[0].MatchPairs[0].LeftText != "Cash" || questions[0].MatchPairs[1].RightText != "Nominal Account" {
		t.Errorf("match pair content corrupted: %+v", questions[0].MatchPairs)
	}
}
