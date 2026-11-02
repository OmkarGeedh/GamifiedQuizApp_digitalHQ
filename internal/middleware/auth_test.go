package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

func TestClientAuthMiddleware_AlternativeHeadersAndParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "test_jwt_secret_for_middleware_testing"
	if config.AppConfig != nil && config.AppConfig.Server.JWTSecret != "" {
		secret = config.AppConfig.Server.JWTSecret
	} else {
		t.Setenv("JWT_SECRET", secret)
	}

	createToken := func() string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"client_id": 101,
			"email":     "player@example.com",
			"exp":       time.Now().Add(1 * time.Hour).Unix(),
		})
		s, _ := token.SignedString([]byte(secret))
		return s
	}

	validToken := createToken()

	tests := []struct {
		name       string
		setupReq   func(r *http.Request)
		wantStatus int
		wantID     int
	}{
		{
			name: "auth_token query parameter",
			setupReq: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("auth_token", validToken)
				r.URL.RawQuery = q.Encode()
			},
			wantStatus: http.StatusOK,
			wantID:     101,
		},
		{
			name: "jwt query parameter",
			setupReq: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("jwt", validToken)
				r.URL.RawQuery = q.Encode()
			},
			wantStatus: http.StatusOK,
			wantID:     101,
		},
		{
			name: "Sec-WebSocket-Protocol single token",
			setupReq: func(r *http.Request) {
				r.Header.Set("Sec-WebSocket-Protocol", validToken)
			},
			wantStatus: http.StatusOK,
			wantID:     101,
		},
		{
			name: "Sec-WebSocket-Protocol with bearer prefix",
			setupReq: func(r *http.Request) {
				r.Header.Set("Sec-WebSocket-Protocol", "bearer, "+validToken)
			},
			wantStatus: http.StatusOK,
			wantID:     101,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/ws/game", nil)
			tt.setupReq(req)
			c.Request = req

			ClientAuthMiddleware(c)

			if c.IsAborted() {
				t.Fatalf("expected request not to abort, got status %d, body: %s", w.Code, w.Body.String())
			}

			id, ok := GetAuthenticatedClientID(c)
			if !ok || id != tt.wantID {
				t.Fatalf("expected client_id %d, got %d (ok=%v)", tt.wantID, id, ok)
			}
		})
	}
}
