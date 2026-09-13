package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetAndClearClientAuthCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/", nil)

	SetClientAuthCookies(c, "mock-access-token", "mock-refresh-token")

	cookies := w.Result().Cookies()
	foundAccess := false
	foundRefresh := false

	for _, cookie := range cookies {
		if cookie.Name == "clientAccessToken" {
			foundAccess = true
			if cookie.Value != "mock-access-token" {
				t.Errorf("expected access token 'mock-access-token', got '%s'", cookie.Value)
			}
		}
		if cookie.Name == "clientRefreshToken" {
			foundRefresh = true
			if cookie.Value != "mock-refresh-token" {
				t.Errorf("expected refresh token 'mock-refresh-token', got '%s'", cookie.Value)
			}
		}
	}

	if !foundAccess || !foundRefresh {
		t.Errorf("expected both cookies to be set; foundAccess=%t, foundRefresh=%t", foundAccess, foundRefresh)
	}

	// Test clearing cookies
	wClear := httptest.NewRecorder()
	cClear, _ := gin.CreateTestContext(wClear)
	cClear.Request, _ = http.NewRequest("GET", "/", nil)

	ClearClientAuthCookies(cClear)
	clearCookies := wClear.Result().Cookies()

	for _, cookie := range clearCookies {
		if (cookie.Name == "clientAccessToken" || cookie.Name == "clientRefreshToken") && cookie.MaxAge != -1 {
			t.Errorf("expected cookie %s MaxAge to be -1, got %d", cookie.Name, cookie.MaxAge)
		}
	}
}
