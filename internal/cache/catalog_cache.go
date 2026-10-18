package cache

import (
	"context"
	"encoding/json"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
)

// GetCachedTopics retrieves the cached available topics list from Redis.
func GetCachedTopics(ctx context.Context) ([]map[string]interface{}, bool) {
	if config.RedisClient == nil {
		return nil, false
	}

	data, err := config.RedisClient.Get(ctx, AvailableTopicsCatalogKey).Bytes()
	if err != nil {
		return nil, false
	}

	var topics []map[string]interface{}
	if err := json.Unmarshal(data, &topics); err != nil {
		return nil, false
	}
	return topics, true
}

// SetCachedTopics stores the available topics list in Redis with 1-hour TTL.
func SetCachedTopics(ctx context.Context, topics []map[string]interface{}) error {
	if config.RedisClient == nil || len(topics) == 0 {
		return nil
	}

	data, err := json.Marshal(topics)
	if err != nil {
		return err
	}
	return config.RedisClient.Set(ctx, AvailableTopicsCatalogKey, data, CatalogCacheTTL).Err()
}

// InvalidateCachedTopics clears the cached available topics list.
func InvalidateCachedTopics(ctx context.Context) error {
	if config.RedisClient == nil {
		return nil
	}
	return config.RedisClient.Del(ctx, AvailableTopicsCatalogKey).Err()
}
