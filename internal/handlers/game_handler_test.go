package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var (
	gameTestDBOnce sync.Once
	gameTestDB     *gorm.DB
	gameTestDBErr  error
)

func setupTestApp(t *testing.T) (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)

	// Load configuration
	if err := config.LoadEnv(); err != nil {
		t.Logf("Notice loading .env: %v", err)
	}

	gameTestDBOnce.Do(func() {
		dbURL := config.AppConfig.Database.ConnectionString()
		gameTestDB, gameTestDBErr = db.Connect(dbURL)
	})

	if gameTestDBErr != nil {
		t.Skipf("Skipping game test: test database unreachable: %v", gameTestDBErr)
		return nil, ""
	}
	config.DB = gameTestDB

	// Connect Redis
	redisAddr := config.AppConfig.Redis.Endpoint()
	redisClient, _ := db.ConnectRedis(redisAddr, config.AppConfig.Redis.Password, config.AppConfig.Redis.DB)
	config.RedisClient = redisClient

	// Build router
	r := gin.New()
	r.Use(gin.Recovery())

	routes.RegisterProfileRoutes(r)
	routes.RegisterGameRoutes(r)

	// Generate valid test JWT token for demo user (clientID = 1)
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

func TestE2E_MCQ_REST_Workflow(t *testing.T) {
	r, token := setupTestApp(t)

	// 1. Unauthorized request check
	t.Run("Unauthorized request rejected with 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/topics/accounting/questions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})

	// 2. Fetch Topic Questions
	t.Run("Fetch accounting questions", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/topics/accounting/questions?limit=5", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                          `json:"success"`
			Data    dto.TopicQuestionsResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success || len(resp.Data.Questions) != 5 {
			t.Fatalf("Expected 5 questions, got %d", len(resp.Data.Questions))
		}

		q1 := resp.Data.Questions[0]
		if q1.Question == "" || q1.Prompt == "" || len(q1.Options) != 4 {
			t.Fatalf("Malformed question structure: %+v", q1)
		}
		// Verify options formatting
		if q1.Options[0].Option != "a" || q1.Options[1].Option != "b" {
			t.Fatalf("Unexpected option codes: %+v", q1.Options)
		}
	})

	var activeSessionID string
	var firstQuestionCode string
	var firstCorrectOption string
	var secondQuestionCode string

	// Ensure any stale active session is cleaned up before starting test
	_ = config.DB.Model(&models.GameSession{}).
		Where("client_id = ? AND status = ?", 1, models.SessionStatusInProgress).
		Update("status", models.SessionStatusAbandoned).Error

	// 3. Create Quiz Session
	t.Run("Create new quiz session", func(t *testing.T) {
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:       "accounting",
			QuestionCount: 5,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                          `json:"success"`
			Data    dto.SessionCreatedResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Data.Session == "" {
			t.Fatalf("Session ID is empty")
		}
		if len(resp.Data.Questions) != 5 {
			t.Fatalf("Expected 5 session questions, got %d", len(resp.Data.Questions))
		}

		activeSessionID = resp.Data.Session
		firstQuestionCode = resp.Data.Questions[0].Question
		firstCorrectOption = resp.Data.Questions[0].CorrectOption
		secondQuestionCode = resp.Data.Questions[1].Question
	})

	// 4. Duplicate Session Creation Rejected
	t.Run("Reject concurrent active session creation", func(t *testing.T) {
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:       "accounting",
			QuestionCount: 5,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("Expected 409 Conflict for concurrent session, got %d", w.Code)
		}
	})

	// 5. Apply 50:50 Power-Up
	t.Run("Apply 50:50 power-up returns 2 wrong options", func(t *testing.T) {
		body, _ := json.Marshal(dto.FiftyFiftyRequestDTO{
			Session:  activeSessionID,
			Question: firstQuestionCode,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/power-ups/fifty-fifty", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                      `json:"success"`
			Data    dto.FiftyFiftyResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(resp.Data.HiddenOptions) != 2 {
			t.Fatalf("Expected exactly 2 hidden options, got %d: %+v", len(resp.Data.HiddenOptions), resp.Data.HiddenOptions)
		}

		// Verify hidden options do not include the session's shuffled correct option
		for _, h := range resp.Data.HiddenOptions {
			if strings.EqualFold(h, firstCorrectOption) {
				t.Fatalf("50:50 hid the correct option %s!", firstCorrectOption)
			}
		}
	})

	// 6. Evaluate Answer (Submit answer)
	t.Run("Evaluate correct answer", func(t *testing.T) {
		body, _ := json.Marshal(dto.SubmitAnswerRequestDTO{
			Session:     activeSessionID,
			Question:    firstQuestionCode,
			Option:      strings.ToLower(firstCorrectOption),
			TimeTakenMs: 3000,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/answers/evaluate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                `json:"success"`
			Data    dto.AnswerResultDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Data.IsCorrect {
			t.Fatalf("Expected answer to be correct for session correct_option=%s, got correct_option=%s", firstCorrectOption, resp.Data.CorrectOption)
		}
		if resp.Data.PointsEarned <= 0 || resp.Data.CoinsEarned <= 0 {
			t.Fatalf("Expected positive points and coins, got pts=%d, coins=%d", resp.Data.PointsEarned, resp.Data.CoinsEarned)
		}
		if resp.Data.ComboStreak != 1 {
			t.Fatalf("Expected combo streak 1, got %d", resp.Data.ComboStreak)
		}
	})

	// 7. Duplicate Answer Rejected
	t.Run("Reject duplicate answer submission for same question", func(t *testing.T) {
		body, _ := json.Marshal(dto.SubmitAnswerRequestDTO{
			Session:     activeSessionID,
			Question:    firstQuestionCode,
			Option:      "a",
			TimeTakenMs: 2000,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/answers/evaluate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("Expected 409 Conflict for duplicate answer, got %d", w.Code)
		}
	})

	// 8. Evaluate Skip Question
	t.Run("Evaluate skip power-up answer", func(t *testing.T) {
		body, _ := json.Marshal(dto.SubmitAnswerRequestDTO{
			Session:     activeSessionID,
			Question:    secondQuestionCode,
			Option:      "skip",
			TimeTakenMs: 1500,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/answers/evaluate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                `json:"success"`
			Data    dto.AnswerResultDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Data.IsSkipped {
			t.Fatalf("Expected is_skipped=true")
		}
		if resp.Data.PointsEarned != 0 {
			t.Fatalf("Expected 0 points for skip, got %d", resp.Data.PointsEarned)
		}
	})

	// 9. Complete Session and Claim Rewards
	t.Run("Complete session awards wallet ledger and profile updates", func(t *testing.T) {
		body, _ := json.Marshal(dto.CompleteSessionRequestDTO{
			Session: activeSessionID,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/complete", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                           `json:"success"`
			Data    dto.SessionCompleteResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Data.CoinsAwarded <= 0 || resp.Data.XPAwarded <= 0 {
			t.Fatalf("Expected positive coins and XP, got coins=%d, xp=%d", resp.Data.CoinsAwarded, resp.Data.XPAwarded)
		}

		// Verify Wallet Ledger database entries
		var ledgerCount int64
		config.DB.Model(&models.WalletLedger{}).
			Where("client_id = ? AND reference_id = ?", 1, activeSessionID).
			Count(&ledgerCount)

		if ledgerCount == 0 {
			t.Fatalf("Expected wallet_ledger entries for session %s, found 0", activeSessionID)
		}

		// Verify Session is finished in DB
		var dbSession models.GameSession
		config.DB.Where("id = ?", activeSessionID).First(&dbSession)
		if dbSession.Status != models.SessionStatusFinished {
			t.Fatalf("Expected session status finished, got %s", dbSession.Status)
		}
	})

	// 10. Fetch Game History
	t.Run("Fetch player quiz history", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/quiz/history?limit=10&offset=0", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool `json:"success"`
			Data    struct {
				Total    int                  `json:"total"`
				Sessions []models.GameSession `json:"sessions"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Data.Total == 0 || len(resp.Data.Sessions) == 0 {
			t.Fatalf("Expected history sessions, got 0")
		}
	})

	// 11. Abandon Session
	t.Run("Abandon active session", func(t *testing.T) {
		// Create a new session to abandon
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:       "accounting",
			QuestionCount: 5,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var createResp struct {
			Data dto.SessionCreatedResponseDTO `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &createResp)
		newSessionID := createResp.Data.Session

		// Abandon it
		abandonBody, _ := json.Marshal(dto.CompleteSessionRequestDTO{Session: newSessionID})
		abandonReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/abandon", bytes.NewReader(abandonBody))
		abandonReq.Header.Set("Authorization", "Bearer "+token)
		abandonReq.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, abandonReq)

		if w2.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK on abandon, got %d: %s", w2.Code, w2.Body.String())
		}

		var dbSession models.GameSession
		config.DB.Where("id = ?", newSessionID).First(&dbSession)
		if dbSession.Status != models.SessionStatusAbandoned {
			t.Fatalf("Expected status abandoned, got %s", dbSession.Status)
		}
	})
}

func TestE2E_WebSocket_LiveGame(t *testing.T) {
	r, token := setupTestApp(t)

	// Clean up any stale sessions
	_ = config.DB.Model(&models.GameSession{}).
		Where("client_id = ? AND status = ?", 1, models.SessionStatusInProgress).
		Update("status", models.SessionStatusAbandoned).Error

	// Create live HTTP server for WebSocket tests
	server := httptest.NewServer(r)
	defer server.Close()

	// 1. Create a session for WebSocket play
	body, _ := json.Marshal(dto.CreateSessionRequestDTO{
		TopicID:       "accounting",
		QuestionCount: 3,
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/quiz/sessions/create", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer res.Body.Close()

	var createResp struct {
		Data dto.SessionCreatedResponseDTO `json:"data"`
	}
	_ = json.NewDecoder(res.Body).Decode(&createResp)
	wsSessionID := createResp.Data.Session
	if wsSessionID == "" {
		t.Fatalf("Session ID is empty")
	}

	// 2. Connect via Gorilla WebSocket using ?token= query parameter
	serverURL, _ := url.Parse(server.URL)
	wsURL := fmt.Sprintf("ws://%s/ws/game?session_id=%s&token=%s", serverURL.Host, wsSessionID, token)

	wsConn, httpResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v (HTTP Status: %v)", err, httpResp)
	}
	defer wsConn.Close()

	// 3. Receive first question over WebSocket
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var qMsg ws.OutboundMessage
	if err := wsConn.ReadJSON(&qMsg); err != nil {
		t.Fatalf("Failed to read first question message: %v", err)
	}

	if qMsg.Type != ws.MsgTypeQuestion {
		t.Fatalf("Expected message type 'question', got '%s'", qMsg.Type)
	}

	rawQ, _ := json.Marshal(qMsg.Data)
	var qPayload ws.QuestionPayload
	_ = json.Unmarshal(rawQ, &qPayload)

	if qPayload.Question == "" || len(qPayload.Options) != 4 {
		t.Fatalf("Malformed question payload: %+v", qPayload)
	}

	// 4. Submit answer over WebSocket using session's shuffled correct option
	wsCorrectOption := createResp.Data.Questions[0].CorrectOption
	submitMsg := ws.InboundMessage{
		Type: ws.MsgTypeSubmitAnswer,
	}
	submitData, _ := json.Marshal(ws.SubmitAnswerData{
		Question:    qPayload.Question,
		Option:      wsCorrectOption,
		TimeTakenMs: 2500,
	})
	submitMsg.Data = submitData

	if err := wsConn.WriteJSON(submitMsg); err != nil {
		t.Fatalf("Failed to send submit_answer: %v", err)
	}

	// 5. Receive answer_result over WebSocket
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resultMsg ws.OutboundMessage
	if err := wsConn.ReadJSON(&resultMsg); err != nil {
		t.Fatalf("Failed to read answer_result: %v", err)
	}

	if resultMsg.Type != ws.MsgTypeAnswerResult {
		t.Fatalf("Expected message type 'answer_result', got '%s'", resultMsg.Type)
	}

	rawResult, _ := json.Marshal(resultMsg.Data)
	var resultPayload ws.AnswerResultPayload
	_ = json.Unmarshal(rawResult, &resultPayload)

	if resultPayload.Question != qPayload.Question {
		t.Fatalf("Expected result for question %s, got %s", qPayload.Question, resultPayload.Question)
	}
	if resultPayload.Option != wsCorrectOption {
		t.Fatalf("Expected option '%s', got '%s'", wsCorrectOption, resultPayload.Option)
	}
	if !resultPayload.IsCorrect {
		t.Fatalf("Expected answer to be marked correct for option '%s', got is_correct=false", wsCorrectOption)
	}

	t.Logf("WebSocket live test passed: Question=%s, Result=%+v", qPayload.Question, resultPayload)
}
