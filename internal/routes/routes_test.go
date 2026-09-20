package routes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/gin-gonic/gin"
)

func setupTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Robust CORS Middleware matching main.go
	r.Use(func(c *gin.Context) {
		if len(c.Request.URL.Path) > 1 && strings.HasSuffix(c.Request.URL.Path, "/") {
			c.Request.URL.Path = strings.TrimRight(c.Request.URL.Path, "/")
		}

		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")

		reqHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
		if reqHeaders != "" {
			c.Writer.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With, Sec-Ch-Ua, Sec-Ch-Ua-Mobile, Sec-Ch-Ua-Platform, Sec-Fetch-Dest, Sec-Fetch-Mode, Sec-Fetch-Site, User-Agent")
		}

		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Authorization, X-Total-Count, Link")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		c.Writer.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	})

	healthHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	}
	r.GET("/health", healthHandler)
	r.GET("/api/v1/health", healthHandler)

	routes.RegisterAuthRoutes(r)
	routes.RegisterProfileRoutes(r)
	routes.RegisterGameRoutes(r)
	routes.RegisterWalletRoutes(r)

	return r
}

func TestCORSPreflightOptions(t *testing.T) {
	router := setupTestEngine()

	req, _ := http.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,authorization,custom-header")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if allowOrigin := w.Header().Get("Access-Control-Allow-Origin"); allowOrigin != "http://localhost:3000" {
		t.Errorf("expected Access-Control-Allow-Origin 'http://localhost:3000', got '%s'", allowOrigin)
	}

	if allowCreds := w.Header().Get("Access-Control-Allow-Credentials"); allowCreds != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials 'true', got '%s'", allowCreds)
	}

	if allowHeaders := w.Header().Get("Access-Control-Allow-Headers"); allowHeaders != "content-type,authorization,custom-header" {
		t.Errorf("expected echoed Access-Control-Allow-Headers, got '%s'", allowHeaders)
	}
}

func TestRouteRegistrationBothPrefixes(t *testing.T) {
	router := setupTestEngine()

	// Test health endpoints
	for _, path := range []string{"/health", "/api/v1/health"} {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "http://localhost:3000")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200 for %s, got %d", path, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Errorf("expected CORS origin header for %s", path)
		}
	}

	// Test auth endpoints match under both /auth and /api/v1/auth (not 404)
	for _, path := range []string{"/auth/login", "/api/v1/auth/login", "/auth/login/"} {
		req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusNotFound {
			t.Errorf("expected route %s to exist (not 404), got 404", path)
		}
	}
}
