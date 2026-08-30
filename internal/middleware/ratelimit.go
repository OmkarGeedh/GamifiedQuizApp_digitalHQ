package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// RateLimit provides general route rate limiting (60 requests per minute by IP)
func RateLimit() gin.HandlerFunc {
	return RateLimitWithLimit("global", 60, 1*time.Minute)
}

// RateLimitWithLimit creates a Redis-backed rate limiter for specific endpoint policies
func RateLimitWithLimit(keyPrefix string, max int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.RedisClient == nil {
			c.Next()
			return
		}

		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}

		key := fmt.Sprintf("ratelimit:%s:%s", keyPrefix, ip)
		ctx := context.Background()

		count, err := config.RedisClient.Incr(ctx, key).Result()
		if err != nil {
			// Allow request if Redis is unreachable to avoid breaking service
			c.Next()
			return
		}

		if count == 1 {
			config.RedisClient.Expire(ctx, key, window)
		}

		if count > int64(max) {
			response.Error(c, http.StatusTooManyRequests, fmt.Sprintf("Too many requests. Please try again in %v", window))
			return
		}

		c.Next()
	}
}
