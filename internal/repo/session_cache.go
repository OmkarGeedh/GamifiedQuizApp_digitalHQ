package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

// SessionCacheTTL is the sliding inactivity TTL for cached game sessions in Redis (5 minutes).
const SessionCacheTTL = 5 * time.Minute

func cacheKey(sessionID string) string {
	return fmt.Sprintf("quiz:session:cache:%s", sessionID)
}

func clientActiveKey(clientID int) string {
	return fmt.Sprintf("quiz:client:active:%d", clientID)
}

// ActiveSessionInfo tracks an active game session for a client to prevent multi-session abuse.
type ActiveSessionInfo struct {
	SessionID string `json:"session_id"`
	GameMode  string `json:"game_mode"`
}

// In-memory fallbacks when Redis is not available (e.g. offline unit testing)
var (
	memSessionStore sync.Map // map[string]*models.GameSession
	memActiveStore  sync.Map // map[int]*ActiveSessionInfo
)

// cachedSessionWrapper explicitly preserves QuestionsState across JSON serialization
// because GameSession tags QuestionsState with json:"-".
type cachedSessionWrapper struct {
	Session        models.GameSession `json:"session"`
	QuestionsState string             `json:"questions_state"`
}

// CacheSession stores the active game session in Redis (or in-memory fallback) with 5m sliding TTL.
func CacheSession(ctx context.Context, session *models.GameSession) error {
	if session == nil || session.ID == "" {
		return nil
	}

	if config.RedisClient == nil {
		// Deep clone or store directly in memory
		cloned := *session
		memSessionStore.Store(session.ID, &cloned)
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

// GetCachedSession retrieves an active session from Redis (or in-memory fallback) and slides TTL.
func GetCachedSession(ctx context.Context, sessionID string) (*models.GameSession, bool) {
	if sessionID == "" {
		return nil, false
	}

	if config.RedisClient == nil {
		val, ok := memSessionStore.Load(sessionID)
		if !ok || val == nil {
			return nil, false
		}
		sess := val.(*models.GameSession)
		cloned := *sess
		return &cloned, true
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

// RefreshSessionCacheTTL explicitly extends the session cache TTL.
func RefreshSessionCacheTTL(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if config.RedisClient == nil {
		return nil
	}
	return config.RedisClient.Expire(ctx, cacheKey(sessionID), SessionCacheTTL).Err()
}

// InvalidateCachedSession deletes the cached session.
func InvalidateCachedSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	memSessionStore.Delete(sessionID)
	if config.RedisClient == nil {
		return nil
	}
	return config.RedisClient.Del(ctx, cacheKey(sessionID)).Err()
}

// SetClientActiveSession stores the active game pointer for a client with 5-minute TTL.
func SetClientActiveSession(ctx context.Context, clientID int, sessionID, gameMode string) error {
	info := &ActiveSessionInfo{SessionID: sessionID, GameMode: gameMode}
	if config.RedisClient == nil {
		memActiveStore.Store(clientID, info)
		return nil
	}

	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return config.RedisClient.Set(ctx, clientActiveKey(clientID), data, SessionCacheTTL).Err()
}

// GetClientActiveSession checks if a client has an active game in < 0.2ms.
func GetClientActiveSession(ctx context.Context, clientID int) (*ActiveSessionInfo, bool) {
	if config.RedisClient == nil {
		val, ok := memActiveStore.Load(clientID)
		if !ok || val == nil {
			return nil, false
		}
		info := val.(*ActiveSessionInfo)
		return info, true
	}

	data, err := config.RedisClient.Get(ctx, clientActiveKey(clientID)).Bytes()
	if err != nil {
		return nil, false
	}

	var info ActiveSessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, false
	}
	return &info, true
}

// ClearClientActiveSession clears the active game pointer for a client.
func ClearClientActiveSession(ctx context.Context, clientID int) error {
	memActiveStore.Delete(clientID)
	if config.RedisClient == nil {
		return nil
	}
	return config.RedisClient.Del(ctx, clientActiveKey(clientID)).Err()
}
