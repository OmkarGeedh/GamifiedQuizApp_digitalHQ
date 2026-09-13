package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetAuthenticatedClientID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Context contains client_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/", nil)
		c.Set("client_id", 42)

		id, ok := GetAuthenticatedClientID(c)
		if !ok {
			t.Fatalf("expected ok=true, got false")
		}
		if id != 42 {
			t.Errorf("expected client_id=42, got %d", id)
		}
	})

	t.Run("Context does not contain client_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/", nil)

		id, ok := GetAuthenticatedClientID(c)
		if ok {
			t.Fatalf("expected ok=false, got true")
		}
		if id != 0 {
			t.Errorf("expected id=0, got %d", id)
		}
	})
}
