package cache

import (
	"context"
	"encoding/json"
	"time"

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

// GetCachedQuestionCodes retrieves cached question codes for a topic and mode in < 0.2ms.
func GetCachedQuestionCodes(ctx context.Context, topicID, gameMode string) ([]string, bool) {
	if config.RedisClient == nil {
		return nil, false
	}
	key := QuestionCatalogKey(topicID, gameMode)
	members, err := config.RedisClient.SMembers(ctx, key).Result()
	if err != nil || len(members) == 0 {
		return nil, false
	}
	return members, true
}

// SetCachedQuestionCodes caches question codes in a Redis Set with 2-hour TTL.
func SetCachedQuestionCodes(ctx context.Context, topicID, gameMode string, codes []string) error {
	if config.RedisClient == nil || len(codes) == 0 {
		return nil
	}
	key := QuestionCatalogKey(topicID, gameMode)
	pipe := config.RedisClient.Pipeline()
	pipe.Del(ctx, key)
	interfaceCodes := make([]interface{}, len(codes))
	for i, c := range codes {
		interfaceCodes[i] = c
	}
	pipe.SAdd(ctx, key, interfaceCodes...)
	pipe.Expire(ctx, key, 2*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}
