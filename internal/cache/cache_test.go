package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func TestCache_RedisNilResilience(t *testing.T) {
	// Temporarily ensure RedisClient is nil
	origClient := config.RedisClient
	config.RedisClient = nil
	defer func() { config.RedisClient = origClient }()

	ctx := context.Background()

	// 1. Leaderboard cache functions should not panic when Redis is nil
	if err := UpdateWeeklyLeaderboard(ctx, 10, 50); err != nil {
		t.Fatalf("expected nil error from UpdateWeeklyLeaderboard when Redis is nil, got %v", err)
	}
	rank, err := GetWeeklyRank(ctx, 10)
	if err == nil || rank != 0 {
		t.Fatalf("expected error and rank 0 when Redis is nil, got rank=%d, err=%v", rank, err)
	}
	entries, err := GetTopLeaderboard(ctx, 10)
	if err != nil || entries != nil {
		t.Fatalf("expected nil entries and nil error when Redis is nil")
	}
	if err := RebuildWeeklyLeaderboard(ctx, []LeaderboardEntry{{ClientID: 10, Score: 100, Rank: 1}}); err != nil {
		t.Fatalf("expected nil error from RebuildWeeklyLeaderboard when Redis is nil, got %v", err)
	}

	// 2. Profile cache functions should return false/nil gracefully
	profile, ok := GetCachedProfile(ctx, 10)
	if ok || profile != nil {
		t.Fatalf("expected ok=false and profile=nil when Redis is nil")
	}
	if err := SetCachedProfile(ctx, &models.Profile{ClientID: 10}); err != nil {
		t.Fatalf("expected nil error from SetCachedProfile when Redis is nil, got %v", err)
	}
	if err := InvalidateCachedProfile(ctx, 10); err != nil {
		t.Fatalf("expected nil error from InvalidateCachedProfile when Redis is nil, got %v", err)
	}

	// 3. Catalog cache functions should return false/nil gracefully
	topics, ok := GetCachedTopics(ctx)
	if ok || topics != nil {
		t.Fatalf("expected ok=false and topics=nil when Redis is nil")
	}
	if err := SetCachedTopics(ctx, []map[string]interface{}{{"topic_id": "accounting"}}); err != nil {
		t.Fatalf("expected nil error from SetCachedTopics when Redis is nil, got %v", err)
	}
	if err := InvalidateCachedTopics(ctx); err != nil {
		t.Fatalf("expected nil error from InvalidateCachedTopics when Redis is nil, got %v", err)
	}

	// 4. Answer lock allows request when Redis is nil
	locked := AcquireAnswerLock(ctx, "session-1", "q-1")
	if !locked {
		t.Fatalf("expected true from AcquireAnswerLock when Redis is nil")
	}
}

func TestSingleFlight_ThunderingHerdMitigation(t *testing.T) {
	const goroutines = 50
	var dbQueryCount int32
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = RequestGroup.Do("test_topic_coalesce", func() (interface{}, error) {
				atomic.AddInt32(&dbQueryCount, 1)
				time.Sleep(10 * time.Millisecond) // Simulate DB I/O
				return "data", nil
			})
		}()
	}

	wg.Wait()

	count := atomic.LoadInt32(&dbQueryCount)
	if count > 2 {
		t.Fatalf("SingleFlight failed to coalesce concurrent queries: fired %d times (expected <= 2)", count)
	}
}
