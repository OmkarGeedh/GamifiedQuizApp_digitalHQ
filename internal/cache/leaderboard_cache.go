package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/redis/go-redis/v9"
)

// LeaderboardEntry represents a ranked player on the leaderboard
type LeaderboardEntry struct {
	ClientID int     `json:"client_id"`
	Score    float64 `json:"score"`
	Rank     int     `json:"rank"`
}

// UpdateWeeklyLeaderboard increments a client's score in the Redis Sorted Set in < 0.2ms.
func UpdateWeeklyLeaderboard(ctx context.Context, clientID int, scoreDelta int) error {
	if config.RedisClient == nil || clientID <= 0 || scoreDelta == 0 {
		return nil
	}
	return config.RedisClient.ZIncrBy(ctx, WeeklyLeaderboardKey, float64(scoreDelta), strconv.Itoa(clientID)).Err()
}

// GetWeeklyRank retrieves the 1-based weekly rank of a player in < 0.2ms (O(log N)).
// Returns 0 and redis.Nil if client is not yet ranked on this week's leaderboard.
func GetWeeklyRank(ctx context.Context, clientID int) (int, error) {
	if config.RedisClient == nil || clientID <= 0 {
		return 0, redis.Nil
	}

	rank0, err := config.RedisClient.ZRevRank(ctx, WeeklyLeaderboardKey, strconv.Itoa(clientID)).Result()
	if err != nil {
		return 0, err
	}
	return int(rank0) + 1, nil
}

// GetTopLeaderboard fetches the top N players and their scores in < 0.5ms.
func GetTopLeaderboard(ctx context.Context, limit int64) ([]LeaderboardEntry, error) {
	if config.RedisClient == nil || limit <= 0 {
		return nil, nil
	}

	results, err := config.RedisClient.ZRevRangeWithScores(ctx, WeeklyLeaderboardKey, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}

	entries := make([]LeaderboardEntry, len(results))
	for i, z := range results {
		cID, _ := strconv.Atoi(fmt.Sprint(z.Member))
		entries[i] = LeaderboardEntry{
			ClientID: cID,
			Score:    z.Score,
			Rank:     i + 1,
		}
	}
	return entries, nil
}

// RebuildWeeklyLeaderboard atomically replaces or repopulates the weekly leaderboard in Redis.
func RebuildWeeklyLeaderboard(ctx context.Context, entries []LeaderboardEntry) error {
	if config.RedisClient == nil || len(entries) == 0 {
		return nil
	}

	members := make([]redis.Z, len(entries))
	for i, e := range entries {
		members[i] = redis.Z{
			Score:  e.Score,
			Member: strconv.Itoa(e.ClientID),
		}
	}

	pipe := config.RedisClient.TxPipeline()
	pipe.Del(ctx, WeeklyLeaderboardKey)
	pipe.ZAdd(ctx, WeeklyLeaderboardKey, members...)
	pipe.Expire(ctx, WeeklyLeaderboardKey, WeeklyLeaderboardTTL)
	_, err := pipe.Exec(ctx)
	return err
}
