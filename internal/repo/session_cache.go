package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

// SessionCacheTTL is the inactivity TTL for cached game sessions in Redis (5 minutes).
const SessionCacheTTL = 5 * time.Minute

func cacheKey(sessionID string) string {
	return fmt.Sprintf("quiz:session:cache:%s", sessionID)
}

// cachedSessionWrapper explicitly preserves QuestionsState across JSON serialization
// because GameSession tags QuestionsState with json:"-".
type cachedSessionWrapper struct {
	Session        models.GameSession `json:"session"`
	QuestionsState string             `json:"questions_state"`
}

// CacheSession stores the active game session in Redis with the 5-minute inactivity TTL.
func CacheSession(ctx context.Context, session *models.GameSession) error {
	if config.RedisClient == nil || session == nil || session.ID == "" {
		return nil
	}

	wrapper := cachedSessionWrapper{
		Session:        *session,
		QuestionsState: session.QuestionsState,
	}

	data, err := json.Marshal(wrapper)
	if err != nil {
		return fmt.Errorf("failed to marshal session for cache: %w", err)
	}

	return config.RedisClient.Set(ctx, cacheKey(session.ID), data, SessionCacheTTL).Err()
}

// GetCachedSession retrieves an active session from Redis and slides its expiration TTL.
// Returns the deserialized session and true if found; otherwise nil and false.
func GetCachedSession(ctx context.Context, sessionID string) (*models.GameSession, bool) {
	if config.RedisClient == nil || sessionID == "" {
		return nil, false
	}

	key := cacheKey(sessionID)
	data, err := config.RedisClient.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}

	var wrapper cachedSessionWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, false
	}

	session := wrapper.Session
	session.QuestionsState = wrapper.QuestionsState

	// Refresh TTL on cache hit to slide the activity window
	_ = config.RedisClient.Expire(ctx, key, SessionCacheTTL).Err()

	return &session, true
}

// RefreshSessionCacheTTL explicitly extends the Redis session cache TTL.
func RefreshSessionCacheTTL(ctx context.Context, sessionID string) error {
	if config.RedisClient == nil || sessionID == "" {
		return nil
	}
	return config.RedisClient.Expire(ctx, cacheKey(sessionID), SessionCacheTTL).Err()
}

// InvalidateCachedSession deletes the cached session from Redis.
func InvalidateCachedSession(ctx context.Context, sessionID string) error {
	if config.RedisClient == nil || sessionID == "" {
		return nil
	}
	return config.RedisClient.Del(ctx, cacheKey(sessionID)).Err()
}
