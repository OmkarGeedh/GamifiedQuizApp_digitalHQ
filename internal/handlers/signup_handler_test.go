package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/gin-gonic/gin"
)

func TestSignupSendCode_ValidationFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes.RegisterAuthRoutes(router)

	// Test with invalid body
	body := map[string]string{
		"displayName": "",
		"email":       "not-an-email",
		"password":    "123",
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/auth/signup/send-code", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid signup input, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignupFlow_SuccessAndCooldown(t *testing.T) {
	router := setupAuthTestApp(t)
	if router == nil {
		return
	}

	testEmail := "omkartest@example.com"
	testUser := "OmkarChampion"

	// 1. Send verification code
	body := map[string]string{
		"displayName": testUser,
		"email":       testEmail,
		"password":    "SecurePass2026!",
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/auth/signup/send-code", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 from send-code, got %d: %s", w.Code, w.Body.String())
	}

	var sendResp struct {
		Success bool `json:"success"`
		Data    struct {
			Email           string `json:"email"`
			CooldownSeconds int    `json:"cooldownSeconds"`
			ExpiresIn       int    `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("Failed to parse send-code response: %v", err)
	}
	if !sendResp.Success || sendResp.Data.CooldownSeconds != 60 {
		t.Errorf("Expected cooldownSeconds=60, got %d", sendResp.Data.CooldownSeconds)
	}

	// 2. Immediate second request should be blocked by Cooldown (HTTP 429)
	req2, _ := http.NewRequest(http.MethodPost, "/auth/signup/send-code", bytes.NewBuffer(payload))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected status 429 (cooldown active), got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Resend code during cooldown should also be blocked (HTTP 429)
	resendBody := map[string]string{"email": testEmail}
	rPayload, _ := json.Marshal(resendBody)
	reqResend, _ := http.NewRequest(http.MethodPost, "/auth/signup/resend-code", bytes.NewBuffer(rPayload))
	reqResend.Header.Set("Content-Type", "application/json")
	wResend := httptest.NewRecorder()
	router.ServeHTTP(wResend, reqResend)

	if wResend.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected status 429 from resend-code during cooldown, got %d: %s", wResend.Code, wResend.Body.String())
	}

	// 4. Verify with wrong code should fail (HTTP 400)
	verifyBadBody := map[string]string{
		"email": testEmail,
		"code":  "000000",
	}
	vBadPayload, _ := json.Marshal(verifyBadBody)
	reqVBad, _ := http.NewRequest(http.MethodPost, "/auth/signup/verify", bytes.NewBuffer(vBadPayload))
	reqVBad.Header.Set("Content-Type", "application/json")
	wVBad := httptest.NewRecorder()
	router.ServeHTTP(wVBad, reqVBad)

	if wVBad.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400 for incorrect code, got %d: %s", wVBad.Code, wVBad.Body.String())
	}
}
