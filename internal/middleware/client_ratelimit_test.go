package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWindowLimiter(t *testing.T) {
	l := newWindowLimiter()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	for i := 1; i <= 3; i++ {
		if !l.allow("a", 3, time.Minute, now) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.allow("a", 3, time.Minute, now) {
		t.Error("the 4th request in the window should be refused")
	}
	// Someone else is not affected.
	if !l.allow("b", 3, time.Minute, now) {
		t.Error("another key must have its own count")
	}
	// A new window starts after the old one ends.
	if !l.allow("a", 3, time.Minute, now.Add(time.Minute)) {
		t.Error("the count should reset when the window ends")
	}
}

// serve sends one POST through a router that signs the request in as clientID
// (the real route runs the auth middleware first).
func serve(r *gin.Engine, clientID int) int {
	req := httptest.NewRequest(http.MethodPost, "/credit", nil)
	req.Header.Set("X-Test-Client", string(rune('0'+clientID)))
	req.RemoteAddr = "10.0.0.1:1234" // everyone shares one address, like a school
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRateLimitPerClient_LimitsEachPlayerSeparately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	memoryLimiter = newWindowLimiter() // a clean slate

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-Client"); v != "" {
			c.Set("client_id", int(v[0]-'0'))
		}
	})
	r.POST("/credit", RateLimitPerClient("test-wallet", 2, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Player 1 uses their two requests; the third is refused.
	if got := serve(r, 1); got != http.StatusOK {
		t.Fatalf("first request: got %d", got)
	}
	if got := serve(r, 1); got != http.StatusOK {
		t.Fatalf("second request: got %d", got)
	}
	if got := serve(r, 1); got != http.StatusTooManyRequests {
		t.Errorf("third request: got %d, want 429", got)
	}
	// Player 2 shares the IP address but has their own allowance.
	if got := serve(r, 2); got != http.StatusOK {
		t.Errorf("another player on the same network was blocked: got %d", got)
	}
}
