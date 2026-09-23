package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func setupWalletTestApp(t *testing.T) (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)

	if err := config.LoadEnv(); err != nil {
		t.Logf("Notice loading .env: %v", err)
	}

	gameTestDBOnce.Do(func() {
		dbURL := config.AppConfig.Database.ConnectionString()
		gameTestDB, gameTestDBErr = db.Connect(dbURL)
		if gameTestDBErr == nil {
			_ = db.AutoMigrate(gameTestDB)
		}
	})

	if gameTestDBErr != nil {
		t.Skipf("Skipping wallet test: test database unreachable: %v", gameTestDBErr)
		return nil, ""
	}
	config.DB = gameTestDB

	r := gin.New()
	r.Use(gin.Recovery())

	routes.RegisterProfileRoutes(r)
	routes.RegisterWalletRoutes(r)

	secret := config.AppConfig.Server.JWTSecret
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": 1,
		"email":     "player@example.com",
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
		"jti":       uuid.New().String(),
	})
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to sign test JWT: %v", err)
	}

	return r, tokenStr
}

func TestWallet_GetBalance(t *testing.T) {
	r, token := setupWalletTestApp(t)
	if r == nil {
		return
	}

	// 1. Test /api/v1/wallet/balance
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallet/balance", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Success bool                 `json:"success"`
		Data    dto.WalletBalanceDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse balance response: %v", err)
	}
	if !res.Success {
		t.Errorf("Expected success = true")
	}

	// 2. Test root alias /wallet/balance
	reqRoot, _ := http.NewRequest(http.MethodGet, "/wallet/balance", nil)
	reqRoot.Header.Set("Authorization", "Bearer "+token)
	wRoot := httptest.NewRecorder()
	r.ServeHTTP(wRoot, reqRoot)

	if wRoot.Code != http.StatusOK {
		t.Fatalf("Expected root alias 200, got %d: %s", wRoot.Code, wRoot.Body.String())
	}
}

func TestWallet_DebitAndCreditFlow(t *testing.T) {
	r, token := setupWalletTestApp(t)
	if r == nil {
		return
	}

	// 1. Credit 50 coins to ensure enough funds
	creditBody, _ := json.Marshal(dto.WalletCreditRequestDTO{
		Currency: "coins",
		Amount:   50,
		Reason:   "Test bonus top-up",
	})
	reqCredit, _ := http.NewRequest(http.MethodPost, "/api/v1/wallet/credit", bytes.NewReader(creditBody))
	reqCredit.Header.Set("Authorization", "Bearer "+token)
	reqCredit.Header.Set("Content-Type", "application/json")
	wCredit := httptest.NewRecorder()
	r.ServeHTTP(wCredit, reqCredit)

	if wCredit.Code != http.StatusOK {
		t.Fatalf("Expected credit 200, got %d: %s", wCredit.Code, wCredit.Body.String())
	}

	var creditResp struct {
		Success bool                     `json:"success"`
		Data    dto.WalletTransactionDTO `json:"data"`
	}
	_ = json.Unmarshal(wCredit.Body.Bytes(), &creditResp)
	balanceBeforeDebit := creditResp.Data.BalanceAfter

	// 2. Debit 20 coins (e.g. 50:50 power-up)
	debitBody, _ := json.Marshal(dto.WalletDebitRequestDTO{
		Currency: "coins",
		Amount:   20,
		Reason:   "In-game power-up: 50:50",
	})
	reqDebit, _ := http.NewRequest(http.MethodPost, "/api/v1/wallet/debit", bytes.NewReader(debitBody))
	reqDebit.Header.Set("Authorization", "Bearer "+token)
	reqDebit.Header.Set("Content-Type", "application/json")
	wDebit := httptest.NewRecorder()
	r.ServeHTTP(wDebit, reqDebit)

	if wDebit.Code != http.StatusOK {
		t.Fatalf("Expected debit 200, got %d: %s", wDebit.Code, wDebit.Body.String())
	}

	var debitResp struct {
		Success bool                     `json:"success"`
		Data    dto.WalletTransactionDTO `json:"data"`
	}
	_ = json.Unmarshal(wDebit.Body.Bytes(), &debitResp)
	if debitResp.Data.BalanceAfter != balanceBeforeDebit-20 {
		t.Errorf("Expected balance after debit %d, got %d", balanceBeforeDebit-20, debitResp.Data.BalanceAfter)
	}
	if debitResp.Data.Direction != "debit" {
		t.Errorf("Expected direction 'debit', got '%s'", debitResp.Data.Direction)
	}

	// 3. Test Insufficient Funds: attempt to debit 999,999 coins
	insufficientBody, _ := json.Marshal(dto.WalletDebitRequestDTO{
		Currency: "coins",
		Amount:   999999,
		Reason:   "Impossible purchase",
	})
	reqExcess, _ := http.NewRequest(http.MethodPost, "/wallet/debit", bytes.NewReader(insufficientBody))
	reqExcess.Header.Set("Authorization", "Bearer "+token)
	reqExcess.Header.Set("Content-Type", "application/json")
	wExcess := httptest.NewRecorder()
	r.ServeHTTP(wExcess, reqExcess)

	if wExcess.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for insufficient coins, got %d: %s", wExcess.Code, wExcess.Body.String())
	}

	var errResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(wExcess.Body.Bytes(), &errResp)
	if !strings.Contains(strings.ToLower(errResp.Message), "not enough coins") {
		t.Errorf("Expected message to contain 'not enough coins', got '%s'", errResp.Message)
	}

	// 4. Test Transaction History
	reqHist, _ := http.NewRequest(http.MethodGet, "/api/v1/wallet/transactions?limit=5", nil)
	reqHist.Header.Set("Authorization", "Bearer "+token)
	wHist := httptest.NewRecorder()
	r.ServeHTTP(wHist, reqHist)

	if wHist.Code != http.StatusOK {
		t.Fatalf("Expected history 200, got %d: %s", wHist.Code, wHist.Body.String())
	}

	var histResp struct {
		Success bool                         `json:"success"`
		Data    dto.WalletHistoryResponseDTO `json:"data"`
	}
	_ = json.Unmarshal(wHist.Body.Bytes(), &histResp)
	if len(histResp.Data.Transactions) == 0 {
		t.Errorf("Expected at least 1 transaction in history")
	}
}

func TestWallet_Unauthorized(t *testing.T) {
	r, _ := setupWalletTestApp(t)
	if r == nil {
		return
	}

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallet/balance", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized without token, got %d", w.Code)
	}
}
