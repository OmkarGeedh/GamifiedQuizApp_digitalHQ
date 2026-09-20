package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/gin-gonic/gin"
)

func setupAuthTestApp(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	if err := config.LoadEnv(); err != nil {
		t.Logf("Notice loading .env: %v", err)
	}

	dbURL := config.AppConfig.Database.ConnectionString()
	pgDB, err := db.Connect(dbURL)
	if err != nil {
		t.Skipf("Skipping auth test: test database unreachable at %s: %v", dbURL, err)
		return nil
	}
	config.DB = pgDB

	redisAddr := config.AppConfig.Redis.Endpoint()
	redisClient, _ := db.ConnectRedis(redisAddr, config.AppConfig.Redis.Password, config.AppConfig.Redis.DB)
	config.RedisClient = redisClient

	router := gin.New()
	router.Use(gin.Recovery())

	routes.RegisterAuthRoutes(router)
	routes.RegisterProfileRoutes(router)

	return router
}

func TestLogin_Success(t *testing.T) {
	router := setupAuthTestApp(t)

	body := map[string]string{
		"email":    "player@example.com",
		"password": "secretpassword123",
	}
	payload, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			TokenType    string `json:"tokenType"`
			ExpiresIn    int    `json:"expiresIn"`
			Client       struct {
				ID    int    `json:"id"`
				Email string `json:"email"`
			} `json:"client"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Fatalf("Expected success to be true")
	}
	if resp.Data.AccessToken == "" {
		t.Fatalf("Expected accessToken to be non-empty")
	}
	if resp.Data.RefreshToken == "" {
		t.Fatalf("Expected refreshToken to be non-empty")
	}
	if resp.Data.TokenType != "Bearer" {
		t.Errorf("Expected tokenType 'Bearer', got %s", resp.Data.TokenType)
	}
	if resp.Data.Client.Email != "player@example.com" {
		t.Errorf("Expected client email 'player@example.com', got %s", resp.Data.Client.Email)
	}

	// Verify cookies were set
	cookies := w.Result().Cookies()
	var foundAccessCookie, foundRefreshCookie bool
	for _, c := range cookies {
		if c.Name == "clientAccessToken" && c.Value != "" {
			foundAccessCookie = true
		}
		if c.Name == "clientRefreshToken" && c.Value != "" {
			foundRefreshCookie = true
		}
	}
	if !foundAccessCookie {
		t.Errorf("Expected clientAccessToken cookie to be set")
	}
	if !foundRefreshCookie {
		t.Errorf("Expected clientRefreshToken cookie to be set")
	}
}

func TestLogin_InvalidPassword(t *testing.T) {
	router := setupAuthTestApp(t)

	body := map[string]string{
		"email":    "player@example.com",
		"password": "incorrect_password",
	}
	payload, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_NonExistentEmail(t *testing.T) {
	router := setupAuthTestApp(t)

	body := map[string]string{
		"email":    "nonexistent_user_999@example.com",
		"password": "some_password",
	}
	payload, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_MissingFields(t *testing.T) {
	router := setupAuthTestApp(t)

	body := map[string]string{
		"email": "",
	}
	payload, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_OTPRouteIsRemoved(t *testing.T) {
	router := setupAuthTestApp(t)

	body := map[string]string{
		"email":    "player@example.com",
		"password": "secretpassword123",
	}
	payload, _ := json.Marshal(body)

	// Verify /auth/login/otp is completely removed and returns 404 Not Found
	req, _ := http.NewRequest(http.MethodPost, "/auth/login/otp", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404 on /auth/login/otp, got %d", w.Code)
	}

	// Verify /auth/login/verify-otp is completely removed and returns 404 Not Found
	reqVerify, _ := http.NewRequest(http.MethodPost, "/auth/login/verify-otp", bytes.NewBuffer(payload))
	reqVerify.Header.Set("Content-Type", "application/json")
	wVerify := httptest.NewRecorder()
	router.ServeHTTP(wVerify, reqVerify)

	if wVerify.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404 on /auth/login/verify-otp, got %d", wVerify.Code)
	}
}

func TestLogin_ProtectedRoutesAndRefreshToken(t *testing.T) {
	router := setupAuthTestApp(t)

	// 1. Direct Login
	body := map[string]string{
		"email":    "player@example.com",
		"password": "secretpassword123",
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Login failed: %d", w.Code)
	}

	var loginResp struct {
		Data struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp.Data.AccessToken
	refreshToken := loginResp.Data.RefreshToken

	// 2. Call GET /verify-session with token
	reqSession, _ := http.NewRequest(http.MethodGet, "/verify-session", nil)
	reqSession.Header.Set("Authorization", "Bearer "+token)
	wSession := httptest.NewRecorder()
	router.ServeHTTP(wSession, reqSession)

	if wSession.Code != http.StatusOK {
		t.Fatalf("Expected 200 from /verify-session, got %d: %s", wSession.Code, wSession.Body.String())
	}

	// 3. Call GET /profile with token
	reqProfile, _ := http.NewRequest(http.MethodGet, "/profile", nil)
	reqProfile.Header.Set("Authorization", "Bearer "+token)
	wProfile := httptest.NewRecorder()
	router.ServeHTTP(wProfile, reqProfile)

	if wProfile.Code != http.StatusOK {
		t.Fatalf("Expected 200 from /profile, got %d: %s", wProfile.Code, wProfile.Body.String())
	}

	// 4. Call POST /auth/refresh with refresh token
	refreshBody := map[string]string{
		"refreshToken": refreshToken,
	}
	rPayload, _ := json.Marshal(refreshBody)
	reqRefresh, _ := http.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewBuffer(rPayload))
	reqRefresh.Header.Set("Content-Type", "application/json")
	wRefresh := httptest.NewRecorder()
	router.ServeHTTP(wRefresh, reqRefresh)

	if wRefresh.Code != http.StatusOK {
		t.Fatalf("Expected 200 from /auth/refresh, got %d: %s", wRefresh.Code, wRefresh.Body.String())
	}

	// 5. Call POST /auth/logout
	reqLogout, _ := http.NewRequest(http.MethodPost, "/auth/logout", nil)
	reqLogout.Header.Set("Authorization", "Bearer "+token)
	wLogout := httptest.NewRecorder()
	router.ServeHTTP(wLogout, reqLogout)

	if wLogout.Code != http.StatusOK {
		t.Fatalf("Expected 200 from /auth/logout, got %d: %s", wLogout.Code, wLogout.Body.String())
	}

	// 6. Verify token is rejected after logout
	reqRevoked, _ := http.NewRequest(http.MethodGet, "/verify-session", nil)
	reqRevoked.Header.Set("Authorization", "Bearer "+token)
	wRevoked := httptest.NewRecorder()
	router.ServeHTTP(wRevoked, reqRevoked)

	if wRevoked.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 after logout, got %d", wRevoked.Code)
	}
}
