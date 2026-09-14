package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

func ConnectRedis(addr, password string, db int) (*redis.Client, error) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = os.Getenv("REDIS_PRIVATE_URL")
	}

	var opts *redis.Options
	if redisURL != "" {
		parsedOpts, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, fmt.Errorf("redis.ParseURL(): %w", err)
		}
		opts = parsedOpts
	} else {
		opts = &redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}
	}

	rdb := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis.Ping(): %w", err)
	}

	log.Println("Redis connection established successfully")
	return rdb, nil
}
