package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"golang.org/x/sync/singleflight"
)

// Global TTL Constants for Multi-Tier Caching
const (
	SessionCacheTTL           = 5 * time.Minute
	ProfileCacheTTL           = 15 * time.Minute
	CatalogCacheTTL           = 1 * time.Hour
	QuestionSamplingTTL       = 24 * time.Hour
	AnswerLockTTL             = 5 * time.Minute
	WeeklyLeaderboardKey      = "quiz:leaderboard:weekly"
	WeeklyLeaderboardPrevKey  = "quiz:leaderboard:weekly:prev"
	WeeklyLeaderboardTTL      = 7 * 24 * time.Hour
	AvailableTopicsCatalogKey = "quiz:catalog:available_topics"
)

// SingleFlight group to coalesce duplicate concurrent cache misses (thundering herd mitigation)
var RequestGroup singleflight.Group

var memAnswerLocks sync.Map

// ProfileKey returns the Redis key for a user's cached profile
func ProfileKey(clientID int) string {
	return fmt.Sprintf("quiz:profile:%d", clientID)
}

// AnswerLockKey returns the Redis mutex key for answer deduplication
func AnswerLockKey(sessionID, questionCode string) string {
	return fmt.Sprintf("quiz:lock:answer:%s:%s", sessionID, questionCode)
}

// QuestionCatalogKey returns the Redis set key for cached question codes per topic and mode
func QuestionCatalogKey(topicID, gameMode string) string {
	return fmt.Sprintf("quiz:catalog:questions:%s:%s:codes", topicID, gameMode)
}

// IsRedisAvailable returns true if the global Redis client is connected
func IsRedisAvailable() bool {
	return config.RedisClient != nil
}

// AcquireAnswerLock attempts to acquire an atomic lock to prevent duplicate answer submissions in < 0.1ms.
// Returns true if lock was acquired; false if duplicate detected.
func AcquireAnswerLock(ctx context.Context, sessionID, questionCode string) bool {
	key := AnswerLockKey(sessionID, questionCode)
	if config.RedisClient == nil {
		if _, loaded := memAnswerLocks.LoadOrStore(key, time.Now()); loaded {
			return false
		}
		return true
	}
	success, err := config.RedisClient.SetNX(ctx, key, "1", AnswerLockTTL).Result()
	if err != nil {
		return true // Fail open on Redis network error so user request isn't blocked
	}
	return success
}
