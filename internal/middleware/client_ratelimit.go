package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// windowLimiter is a small fixed-window counter held in memory. It backs
// RateLimitPerClient when Redis is not configured, so the limit still works.
type windowLimiter struct {
	mu      sync.Mutex
	windows map[string]*limitWindow
}

type limitWindow struct {
	count   int
	resetAt time.Time
}

func newWindowLimiter() *windowLimiter {
	return &windowLimiter{windows: make(map[string]*limitWindow)}
}

// allow counts one request for key and reports whether it is within max for
// the current window.
func (l *windowLimiter) allow(key string, max int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Keep the map from growing without bound: drop finished windows now and then.
	if len(l.windows) > 10000 {
		for k, w := range l.windows {
			if !now.Before(w.resetAt) {
				delete(l.windows, k)
			}
		}
	}

	w, ok := l.windows[key]
	if !ok || !now.Before(w.resetAt) {
		l.windows[key] = &limitWindow{count: 1, resetAt: now.Add(window)}
		return true
	}
	w.count++
	return w.count <= max
}

// One store for every limiter, so the root and /api/v1 copies of a route share
// the same count for a player.
var memoryLimiter = newWindowLimiter()

// RateLimitPerClient limits a route per signed-in player instead of per IP
// address. Many students share one school network address, so an IP limit would
// block honest players; a player's own id does not. Put it after the auth
// middleware. Without a player id (not signed in) it falls back to the IP.
//
// It uses Redis when configured (the count is shared across servers) and an
// in-memory counter otherwise. If Redis is unreachable it lets the request
// through, like the IP limiter does.
func RateLimitPerClient(keyPrefix string, max int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		who := c.ClientIP()
		if who == "" {
			who = "unknown"
		}
		if id, ok := GetAuthenticatedClientID(c); ok {
			who = fmt.Sprintf("client:%d", id)
		}
		key := fmt.Sprintf("ratelimit:%s:%s", keyPrefix, who)

		allowed := true
		if config.RedisClient != nil {
			ctx := context.Background()
			count, err := config.RedisClient.Incr(ctx, key).Result()
			if err == nil {
				if count == 1 {
					config.RedisClient.Expire(ctx, key, window)
				}
				allowed = count <= int64(max)
			}
		} else {
			allowed = memoryLimiter.allow(key, max, window, time.Now())
		}

		if !allowed {
			response.Error(c, http.StatusTooManyRequests, fmt.Sprintf("Too many requests. Please try again in %v", window))
			c.Abort()
			return
		}
		c.Next()
	}
}
