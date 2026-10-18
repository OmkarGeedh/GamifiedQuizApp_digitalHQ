package cache

import (
	"context"
	"encoding/json"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

// GetCachedProfile retrieves the cached user profile from Redis.
func GetCachedProfile(ctx context.Context, clientID int) (*models.Profile, bool) {
	if config.RedisClient == nil || clientID <= 0 {
		return nil, false
	}

	key := ProfileKey(clientID)
	data, err := config.RedisClient.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}

	var profile models.Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, false
	}
	return &profile, true
}

// SetCachedProfile stores a user profile in Redis with 15-minute TTL.
func SetCachedProfile(ctx context.Context, profile *models.Profile) error {
	if config.RedisClient == nil || profile == nil || profile.ClientID <= 0 {
		return nil
	}

	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}

	return config.RedisClient.Set(ctx, ProfileKey(profile.ClientID), data, ProfileCacheTTL).Err()
}

// InvalidateCachedProfile evicts a user's profile from the cache.
func InvalidateCachedProfile(ctx context.Context, clientID int) error {
	if config.RedisClient == nil || clientID <= 0 {
		return nil
	}
	return config.RedisClient.Del(ctx, ProfileKey(clientID)).Err()
}
