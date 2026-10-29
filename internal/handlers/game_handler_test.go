package handlers_test

import (
	"bytes"
	"context"
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
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
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
	content := newContentFixture(t)
	for n := 1; n <= 6; n++ {
		content.MCQ(n, fmt.Sprintf("Question %d?", n), "Alpha", "Beta", "Gamma", "Delta", "a")
	}

	// 1. Unauthorized request check
	t.Run("Unauthorized request rejected with 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/topics/"+content.Topic+"/questions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})

	// 2. Fetch Topic Questions
	t.Run("Fetch accounting questions", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/topics/"+content.Topic+"/questions?limit=5", nil)
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
	_ = repo.ClearClientActiveSession(context.Background(), 1)

	// 3. Create Quiz Session
	t.Run("Create new quiz session", func(t *testing.T) {
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:       content.Topic,
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
			TopicID:       content.Topic,
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
		if resp.Data.PointsEarned != 10 {
			t.Fatalf("Expected exactly 10 points for a correct MCQ answer, got %d", resp.Data.PointsEarned)
		}
		if resp.Data.CoinsEarned != 0 {
			t.Fatalf("Expected no per-answer coins, got %d", resp.Data.CoinsEarned)
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
		if resp.Data.CoinsEarned != 0 {
			t.Fatalf("Expected 0 coins for skip, got %d", resp.Data.CoinsEarned)
		}
	})

	// 9. Complete Session and Claim Rewards
	t.Run("Complete session awards wallet ledger and profile updates", func(t *testing.T) {
		var profileBefore models.Profile
		if err := config.DB.Where("client_id = ?", 1).First(&profileBefore).Error; err != nil {
			t.Fatalf("Failed to load profile before completion: %v", err)
		}

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

		if resp.Data.FinalScore != 10 || resp.Data.MaxScore != 50 {
			t.Fatalf("Expected score 10/50, got %d/%d", resp.Data.FinalScore, resp.Data.MaxScore)
		}
		if resp.Data.CorrectCount != 1 || resp.Data.TotalQuestions != 5 {
			t.Fatalf("Expected 1/5 correct, got %d/%d", resp.Data.CorrectCount, resp.Data.TotalQuestions)
		}
		if resp.Data.AccuracyPercentage != 20 {
			t.Fatalf("Expected 20%% accuracy, got %.2f%%", resp.Data.AccuracyPercentage)
		}
		if resp.Data.XPAwarded != 15 {
			t.Fatalf("Expected 15 base XP, got %d", resp.Data.XPAwarded)
		}
		if resp.Data.CoinsAwarded != 7 {
			t.Fatalf("Expected 7 base coins, got %d", resp.Data.CoinsAwarded)
		}
		if totalBreakdown(resp.Data.ScoreBreakdown) != resp.Data.FinalScore {
			t.Fatalf("Score breakdown does not match final score: %+v", resp.Data.ScoreBreakdown)
		}
		if totalBreakdown(resp.Data.XPBreakdown) != resp.Data.XPAwarded {
			t.Fatalf("XP breakdown does not match XP awarded: %+v", resp.Data.XPBreakdown)
		}
		if totalBreakdown(resp.Data.CoinBreakdown) != resp.Data.CoinsAwarded {
			t.Fatalf("Coin breakdown does not match coins awarded: %+v", resp.Data.CoinBreakdown)
		}
		if resp.Data.XPBreakdown["perfect_bonus"] != 0 || resp.Data.CoinBreakdown["perfect_bonus"] != 0 {
			t.Fatalf("Perfect bonuses must be zero for a partial quiz: xp=%d, coins=%d",
				resp.Data.XPBreakdown["perfect_bonus"], resp.Data.CoinBreakdown["perfect_bonus"])
		}

		// Verify immutable answer history contains the deterministic awards.
		var correctHistory models.UserQuestionHistory
		if err := config.DB.Where("session_id = ? AND question_code = ?", activeSessionID, firstQuestionCode).
			First(&correctHistory).Error; err != nil {
			t.Fatalf("Failed to load correct-answer history: %v", err)
		}
		if !correctHistory.IsCorrect || correctHistory.PointsAwarded != 10 || correctHistory.CoinsAwarded != 0 {
			t.Fatalf("Unexpected correct-answer history: %+v", correctHistory)
		}

		var skippedHistory models.UserQuestionHistory
		if err := config.DB.Where("session_id = ? AND question_code = ?", activeSessionID, secondQuestionCode).
			First(&skippedHistory).Error; err != nil {
			t.Fatalf("Failed to load skipped-answer history: %v", err)
		}
		if skippedHistory.IsCorrect || skippedHistory.SelectedOption != "skip" || skippedHistory.PointsAwarded != 0 || skippedHistory.CoinsAwarded != 0 {
			t.Fatalf("Unexpected skipped-answer history: %+v", skippedHistory)
		}

		// Verify the completed session persists the authoritative score and count.
		dbSession, err := repo.GetSessionByID(context.Background(), activeSessionID)
		if err != nil {
			t.Fatalf("Failed to load completed session: %v", err)
		}
		if dbSession.Status != models.SessionStatusFinished {
			t.Fatalf("Expected session status finished, got %s", dbSession.Status)
		}
		if dbSession.Score != 10 || dbSession.CorrectCount != 1 {
			t.Fatalf("Expected persisted score=10 and correct_count=1, got score=%d correct_count=%d",
				dbSession.Score, dbSession.CorrectCount)
		}

		// Verify ledger and profile totals, including any separately reported level-up event.
		expectedCoins := resp.Data.CoinsAwarded
		expectedXP := resp.Data.XPAwarded
		if resp.Data.LevelUpReward != nil {
			expectedCoins += resp.Data.LevelUpReward.Coins
			expectedXP += resp.Data.LevelUpReward.XP
		}

		var ledgerEntries []models.WalletLedger
		if err := config.DB.Where("client_id = ? AND reference_id = ?", 1, activeSessionID).
			Find(&ledgerEntries).Error; err != nil {
			t.Fatalf("Failed to load wallet ledger entries: %v", err)
		}
		ledgerTotals := make(map[string]int)
		for _, entry := range ledgerEntries {
			ledgerTotals[entry.Currency] += entry.Amount
		}
		if ledgerTotals[models.CurrencyCoins] != expectedCoins {
			t.Fatalf("Expected %d persisted coins, got %d", expectedCoins, ledgerTotals[models.CurrencyCoins])
		}
		if ledgerTotals[models.CurrencyExperience] != expectedXP {
			t.Fatalf("Expected %d persisted XP, got %d", expectedXP, ledgerTotals[models.CurrencyExperience])
		}

		var profileAfter models.Profile
		if err := config.DB.Where("client_id = ?", 1).First(&profileAfter).Error; err != nil {
			t.Fatalf("Failed to load profile after completion: %v", err)
		}
		if profileAfter.Coins-profileBefore.Coins != expectedCoins {
			t.Fatalf("Expected profile coin delta %d, got %d", expectedCoins, profileAfter.Coins-profileBefore.Coins)
		}
		if profileAfter.Experience-profileBefore.Experience != expectedXP {
			t.Fatalf("Expected profile XP delta %d, got %d", expectedXP, profileAfter.Experience-profileBefore.Experience)
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
			TopicID:       content.Topic,
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

		dbSession, err := repo.GetSessionByID(context.Background(), newSessionID)
		if err != nil {
			t.Fatalf("Failed to load abandoned session: %v", err)
		}
		if dbSession.Status != models.SessionStatusAbandoned {
			t.Fatalf("Expected status abandoned, got %s", dbSession.Status)
		}
	})
}

func totalBreakdown(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func TestE2E_WebSocket_LiveGame(t *testing.T) {
	r, token := setupTestApp(t)
	content := newContentFixture(t)
	for n := 1; n <= 3; n++ {
		content.MCQ(n, fmt.Sprintf("Question %d?", n), "Alpha", "Beta", "Gamma", "Delta", "a")
	}

	// Clean up any stale sessions
	_ = repo.ClearClientActiveSession(context.Background(), 1)

	// Create live HTTP server for WebSocket tests
	server := httptest.NewServer(r)
	defer server.Close()

	// 1. Create a session for WebSocket play
	body, _ := json.Marshal(dto.CreateSessionRequestDTO{
		TopicID:       content.Topic,
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

func TestWebSocket_EchoDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	routes.RegisterGameRoutes(r)

	server := httptest.NewServer(r)
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	wsURL := fmt.Sprintf("ws://%s/ws/echo", serverURL.Host)

	wsConn, httpResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket echo dial failed: %v (HTTP Status: %v)", err, httpResp)
	}
	defer wsConn.Close()

	// Read initial connected diagnostics message
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var welcomeMsg struct {
		Type string `json:"type"`
		Data struct {
			Status       string `json:"status"`
			Platform     string `json:"platform"`
			PingInterval string `json:"ping_interval"`
		} `json:"data"`
	}
	if err := wsConn.ReadJSON(&welcomeMsg); err != nil {
		t.Fatalf("Failed to read echo welcome message: %v", err)
	}
	if welcomeMsg.Type != "connected" || welcomeMsg.Data.Status != "ok" {
		t.Fatalf("Unexpected welcome payload: %+v", welcomeMsg)
	}

	// Send message and expect echo back
	if err := wsConn.WriteMessage(websocket.TextMessage, []byte("hello catalyst")); err != nil {
		t.Fatalf("Failed to write to echo socket: %v", err)
	}

	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var echoResp struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := wsConn.ReadJSON(&echoResp); err != nil {
		t.Fatalf("Failed to read echo response: %v", err)
	}
	if echoResp.Type != "echo" || echoResp.Data != "hello catalyst" {
		t.Fatalf("Unexpected echo response: %+v", echoResp)
	}
}

func TestWebSocket_StructuredErrorOnMissingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "test_jwt_secret_structured_error"
	t.Setenv("JWT_SECRET", secret)
	if config.AppConfig != nil && config.AppConfig.Server.JWTSecret != "" {
		secret = config.AppConfig.Server.JWTSecret
	}

	tokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": 1,
		"exp":       time.Now().Add(1 * time.Hour).Unix(),
	})
	token, _ := tokenObj.SignedString([]byte(secret))

	r := gin.New()
	routes.RegisterGameRoutes(r)

	server := httptest.NewServer(r)
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)

	missingSessionURL := fmt.Sprintf("ws://%s/ws/game?token=%s", serverURL.Host, token)
	wsConn, _, err := websocket.DefaultDialer.Dial(missingSessionURL, nil)
	if err != nil {
		t.Fatalf("Expected WS dial to succeed with structured error frame, got err: %v", err)
	}
	defer wsConn.Close()

	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var errFrame struct {
		Type string `json:"type"`
		Data struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := wsConn.ReadJSON(&errFrame); err != nil {
		t.Fatalf("Failed to read error frame: %v", err)
	}
	if errFrame.Type != "error" || errFrame.Data.Code != "SESSION_REQUIRED" {
		t.Fatalf("Expected SESSION_REQUIRED error, got %+v", errFrame)
	}
}

func TestWebSocket_SessionParamFlexibilityAndStructuredErrors(t *testing.T) {
	r, token := setupTestApp(t)
	server := httptest.NewServer(r)
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)

	// 1. Missing session on WS dial returns structured SESSION_REQUIRED frame
	missingSessionURL := fmt.Sprintf("ws://%s/ws/game?token=%s", serverURL.Host, token)
	wsConn, _, err := websocket.DefaultDialer.Dial(missingSessionURL, nil)
	if err != nil {
		t.Fatalf("Expected WS dial to succeed with structured error frame, got err: %v", err)
	}
	defer wsConn.Close()

	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var errFrame struct {
		Type string `json:"type"`
		Data struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := wsConn.ReadJSON(&errFrame); err != nil {
		t.Fatalf("Failed to read error frame: %v", err)
	}
	if errFrame.Type != "error" || errFrame.Data.Code != "SESSION_REQUIRED" {
		t.Fatalf("Expected SESSION_REQUIRED error, got %+v", errFrame)
	}

	// 2. Create session and connect using ?sessionId= (camelCase)
	content := newContentFixture(t)
	content.MCQ(1, "What is Go?", "Language", "Animal", "Car", "Food", "a")
	topic := content.Topic
	_ = repo.ClearClientActiveSession(context.Background(), 1)

	createBody, _ := json.Marshal(dto.CreateSessionRequestDTO{
		TopicID:       topic,
		QuestionCount: 1,
	})
	createReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/quiz/sessions/create", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("failed create session: %v", err)
	}
	defer res.Body.Close()

	var createResp struct {
		Data dto.SessionCreatedResponseDTO `json:"data"`
	}
	_ = json.NewDecoder(res.Body).Decode(&createResp)
	createdSession := createResp.Data.Session

	// Connect using ?sessionId=
	camelURL := fmt.Sprintf("ws://%s/ws/game?sessionId=%s&token=%s", serverURL.Host, createdSession, token)
	camelConn, _, err := websocket.DefaultDialer.Dial(camelURL, nil)
	if err != nil {
		t.Fatalf("WS dial with ?sessionId= failed: %v", err)
	}
	defer camelConn.Close()

	_ = camelConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var qMsg ws.OutboundMessage
	if err := camelConn.ReadJSON(&qMsg); err != nil {
		t.Fatalf("Failed to read question on ?sessionId= connection: %v", err)
	}
	if qMsg.Type != ws.MsgTypeQuestion {
		t.Fatalf("Expected 'question' type, got %s", qMsg.Type)
	}
}

func TestSuddenDeathSessionContractAndFiltering(t *testing.T) {
	r, token := setupTestApp(t)
	content := newContentFixture(t)
	recovery := newContentFixture(t)
	topic, recoveryTopic := content.Topic, recovery.Topic
	sdCodeA := content.SuddenDeath(1, "Sudden Death A", "Correct", "Wrong", "a")
	content.SuddenDeath(2, "Sudden Death B", "Wrong", "Correct", "b")
	mcqCode := content.MCQ(1, "MCQ", "A", "B", "C", "D", "a")
	recovery.MCQ(1, "Recovery MCQ", "A", "B", "C", "D", "a")

	abandonActive := func() {
		_ = repo.ClearClientActiveSession(context.Background(), 1)
	}
	abandonActive()

	t.Run("creation hides answers and returns exactly two options", func(t *testing.T) {
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:       topic,
			QuestionCount: 2,
			GameMode:      models.GameModeSuddenDeath,
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data dto.SessionCreatedResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode creation response: %v", err)
		}
		if len(resp.Data.Questions) != 2 {
			t.Fatalf("expected two questions, got %d", len(resp.Data.Questions))
		}
		for _, q := range resp.Data.Questions {
			if len(q.Options) != 2 || q.CorrectOption != "" {
				t.Fatalf("Sudden Death response leaked or malformed question: %+v", q)
			}
		}
		stored, err := repo.GetSessionByID(context.Background(), resp.Data.Session)
		if err != nil {
			t.Fatalf("load stored session: %v", err)
		}
		storedQuestions, err := stored.GetQuestions()
		if err != nil || len(storedQuestions) != 2 || storedQuestions[0].CorrectOption == "" {
			t.Fatalf("backend did not retain authoritative answers: questions=%+v err=%v", storedQuestions, err)
		}

		powerUpBody, _ := json.Marshal(dto.FiftyFiftyRequestDTO{
			Session:  resp.Data.Session,
			Question: resp.Data.Questions[0].Question,
		})
		powerUpReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/power-ups/fifty-fifty", bytes.NewReader(powerUpBody))
		powerUpReq.Header.Set("Authorization", "Bearer "+token)
		powerUpReq.Header.Set("Content-Type", "application/json")
		powerUpResponse := httptest.NewRecorder()
		r.ServeHTTP(powerUpResponse, powerUpReq)
		if powerUpResponse.Code != http.StatusBadRequest {
			t.Fatalf("expected Sudden Death REST 50:50 to return 400, got %d: %s", powerUpResponse.Code, powerUpResponse.Body.String())
		}
		abandonActive()
	})

	t.Run("mixed explicit question codes are rejected", func(t *testing.T) {
		body, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:       topic,
			GameMode:      models.GameModeSuddenDeath,
			QuestionCodes: []string{sdCodeA, mcqCode},
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected mixed-mode request to return 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("WebSocket recovery never falls back to MCQ", func(t *testing.T) {
		abandonActive()
		session := models.GameSession{
			ID:             uuid.NewString(),
			ClientID:       1,
			TopicID:        recoveryTopic,
			GameMode:       models.GameModeSuddenDeath,
			TotalQuestions: 1,
			Status:         models.SessionStatusInProgress,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := repo.CreateSession(context.Background(), &session); err != nil {
			t.Fatalf("create recovery session: %v", err)
		}
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/ws/game?session_id="+url.QueryEscape(session.ID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected recovery without Sudden Death questions to fail safely, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestCompleteSortItOutHandler_Unit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Unauthorized when clientID not in context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader([]byte(`{}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		handlers.CompleteSortItOutHandler(c)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Bad Request on malformed JSON payload", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("client_id", 1)
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader([]byte(`invalid-json`)))
		c.Request.Header.Set("Content-Type", "application/json")

		handlers.CompleteSortItOutHandler(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("Bad Request on invalid validation payload (empty answers)", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("client_id", 1)
		payload := dto.CompleteSortItOutRequestDTO{
			Session: "sess-1",
			Answers: []dto.SortItOutAnswerSubmissionDTO{},
		}
		data, _ := json.Marshal(payload)
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader(data))
		c.Request.Header.Set("Content-Type", "application/json")

		handlers.CompleteSortItOutHandler(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
	})
}

func TestCompleteSortItOutHandler(t *testing.T) {
	r, token := setupTestApp(t)
	if r == nil {
		return
	}

	t.Run("Unauthorized request returns 401", func(t *testing.T) {
		body, _ := json.Marshal(dto.CompleteSortItOutRequestDTO{
			Session: "any-session",
			Answers: []dto.SortItOutAnswerSubmissionDTO{
				{Question: "q1", Option: "a"},
			},
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Invalid input returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader([]byte(`invalid-json`)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("Empty answers returns 400", func(t *testing.T) {
		body, _ := json.Marshal(dto.CompleteSortItOutRequestDTO{
			Session: "sess-123",
			Answers: []dto.SortItOutAnswerSubmissionDTO{},
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for empty answers, got %d", w.Code)
		}
	})

	content := newContentFixture(t)
	sortSet := content.SortSet(1, "Debit", "Credit", map[string]string{
		"Cash received": "a", "Capital introduced": "b", "Rent paid": "a", "Loan taken": "b", "Machinery bought": "a",
	})

	t.Run("Full Sort It Out Game Flow: Create, Upfront Answers, Batch Submit, and Audit Review", func(t *testing.T) {
		// Clean up any stale in-progress session for client 1
		_ = repo.ClearClientActiveSession(context.Background(), 1)

		// 1. Create sort_it_out session
		createBody, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:                 content.Topic,
			GameMode:                models.GameModeSortItOut,
			PairType:                sortSet,
			QuestionCount:           5,
			TimeLimitPerQuestionSec: 7,
		})
		createReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(createBody))
		createReq.Header.Set("Authorization", "Bearer "+token)
		createReq.Header.Set("Content-Type", "application/json")
		createW := httptest.NewRecorder()
		r.ServeHTTP(createW, createReq)

		if createW.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for sort_it_out session, got %d: %s", createW.Code, createW.Body.String())
		}

		var createResp struct {
			Success bool                          `json:"success"`
			Data    dto.SessionCreatedResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(createW.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("failed to decode create response: %v", err)
		}

		if createResp.Data.GameMode != models.GameModeSortItOut {
			t.Fatalf("expected game_mode sort_it_out, got %s", createResp.Data.GameMode)
		}
		if createResp.Data.PairType != sortSet {
			t.Fatalf("expected pair_type %s, got %s", sortSet, createResp.Data.PairType)
		}
		if createResp.Data.TimeLimitPerQuestionSec != 7 {
			t.Fatalf("expected TimeLimitPerQuestionSec=7, got %d", createResp.Data.TimeLimitPerQuestionSec)
		}
		if len(createResp.Data.Questions) == 0 {
			t.Fatalf("expected questions in create response, got 0")
		}

		sessionID := createResp.Data.Session
		var answers []dto.SortItOutAnswerSubmissionDTO

		// Verify all questions have upfront correct_option and pair_type
		for i, q := range createResp.Data.Questions {
			if q.PairType != sortSet {
				t.Fatalf("question %s: expected pair_type %s, got %s", q.Question, sortSet, q.PairType)
			}
			if q.CorrectOption == "" {
				t.Fatalf("question %s: expected upfront correct_option, got empty", q.Question)
			}
			if len(q.Options) != 2 || q.Options[0].Text != "Debit" || q.Options[1].Text != "Credit" {
				t.Fatalf("question %s: expected binary Debit/Credit options, got %+v", q.Question, q.Options)
			}

			// Simulate fast swipe answer
			answers = append(answers, dto.SortItOutAnswerSubmissionDTO{
				Question:    q.Question,
				Option:      q.CorrectOption,
				TimeTakenMs: 1200 + i*100, // fast swipes < 2s
			})
		}

		// 2. Batch Complete the Sort It Out session
		completeBody, _ := json.Marshal(dto.CompleteSortItOutRequestDTO{
			Session: sessionID,
			Answers: answers,
		})
		completeReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader(completeBody))
		completeReq.Header.Set("Authorization", "Bearer "+token)
		completeReq.Header.Set("Content-Type", "application/json")
		completeW := httptest.NewRecorder()
		r.ServeHTTP(completeW, completeReq)

		if completeW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from sort-it-out/complete, got %d: %s", completeW.Code, completeW.Body.String())
		}

		var completeResp struct {
			Success bool                             `json:"success"`
			Data    dto.CompleteSortItOutResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(completeW.Body.Bytes(), &completeResp); err != nil {
			t.Fatalf("failed to decode complete response: %v", err)
		}

		data := completeResp.Data
		if data.Session != sessionID {
			t.Fatalf("expected session ID %s, got %s", sessionID, data.Session)
		}
		if data.PairType != sortSet {
			t.Fatalf("expected pair_type %s, got %s", sortSet, data.PairType)
		}
		if data.CorrectCount != len(answers) {
			t.Fatalf("expected %d correct answers, got %d", len(answers), data.CorrectCount)
		}
		if data.BestStreak != len(answers) {
			t.Fatalf("expected best streak %d, got %d", len(answers), data.BestStreak)
		}
		if data.FinalScore <= 0 {
			t.Fatalf("expected positive final score, got %d", data.FinalScore)
		}
		if len(data.CardsReview) != len(answers) {
			t.Fatalf("expected %d card reviews, got %d", len(answers), len(data.CardsReview))
		}
		for _, card := range data.CardsReview {
			if !card.IsCorrect {
				t.Fatalf("card %s should be correct", card.Question)
			}
			if card.BasePoints != 10 {
				t.Fatalf("card %s base points should be 10, got %d", card.Question, card.BasePoints)
			}
			if card.SpeedBonus != 5 {
				t.Fatalf("card %s speed bonus (<2s) should be 5, got %d", card.Question, card.SpeedBonus)
			}
		}

		// Verify score breakdown contains expected keys
		if _, ok := data.ScoreBreakdown["base_points"]; !ok {
			t.Fatal("missing base_points in score breakdown")
		}
		if _, ok := data.ScoreBreakdown["speed_bonus"]; !ok {
			t.Fatal("missing speed_bonus in score breakdown")
		}
		if _, ok := data.ScoreBreakdown["combo_bonus"]; !ok {
			t.Fatal("missing combo_bonus in score breakdown")
		}

		// 3. Second submission should return 409 Conflict
		repeatReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sort-it-out/complete", bytes.NewReader(completeBody))
		repeatReq.Header.Set("Authorization", "Bearer "+token)
		repeatReq.Header.Set("Content-Type", "application/json")
		repeatW := httptest.NewRecorder()
		r.ServeHTTP(repeatW, repeatReq)

		if repeatW.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict on re-completion, got %d", repeatW.Code)
		}
	})
}

func TestCompleteMatchTheFollowingHandler(t *testing.T) {
	r, token := setupTestApp(t)
	if r == nil {
		return
	}

	t.Run("Unauthorized request returns 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/match-the-following/complete", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Invalid input returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/match-the-following/complete", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for malformed JSON, got %d", w.Code)
		}
	})

	t.Run("Empty boards returns 400", func(t *testing.T) {
		body, _ := json.Marshal(dto.CompleteMatchTheFollowingRequestDTO{
			Session: "sess-abc",
			Boards:  []dto.MatchBoardAnswerSubmissionDTO{},
		})
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/match-the-following/complete", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for empty boards, got %d", w.Code)
		}
	})

	t.Run("Full Match the Following Game Flow: Create, Solve Boards, Batch Submit, and Audit Review", func(t *testing.T) {
		// Clean up any stale in-progress session for client 1
		_ = repo.ClearClientActiveSession(context.Background(), 1)

		content := newContentFixture(t)
		code1 := content.Board(1, "Board 1", 4)
		code2 := content.Board(2, "Board 2", 4)
		code3 := content.Board(3, "Board 3", 4)

		// 1. Create match_the_following session
		createBody, _ := json.Marshal(dto.CreateSessionRequestDTO{
			TopicID:                 content.Topic,
			GameMode:                models.GameModeMatchTheFollowing,
			QuestionCodes:           []string{code1, code2, code3},
			QuestionCount:           3,
			TimeLimitPerQuestionSec: 25,
		})
		createReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/create", bytes.NewReader(createBody))
		createReq.Header.Set("Authorization", "Bearer "+token)
		createReq.Header.Set("Content-Type", "application/json")
		createW := httptest.NewRecorder()
		r.ServeHTTP(createW, createReq)

		if createW.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for match_the_following session, got %d: %s", createW.Code, createW.Body.String())
		}

		var createResp struct {
			Success bool                          `json:"success"`
			Data    dto.SessionCreatedResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(createW.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("failed to decode create response: %v", err)
		}

		if createResp.Data.GameMode != models.GameModeMatchTheFollowing {
			t.Fatalf("expected game_mode match_the_following, got %s", createResp.Data.GameMode)
		}
		if createResp.Data.TotalQuestions != 3 {
			t.Fatalf("expected TotalQuestions=3, got %d", createResp.Data.TotalQuestions)
		}
		if createResp.Data.TimeLimitPerQuestionSec != 25 {
			t.Fatalf("expected TimeLimitPerQuestionSec=25, got %d", createResp.Data.TimeLimitPerQuestionSec)
		}
		if len(createResp.Data.Boards) != 3 {
			t.Fatalf("expected 3 boards in create response, got %d", len(createResp.Data.Boards))
		}

		sessionID := createResp.Data.Session
		var boardSubmissions []dto.MatchBoardAnswerSubmissionDTO

		// Verify each board has 4 left and 4 right items and solution_map
		for idx, board := range createResp.Data.Boards {
			if len(board.LeftItems) != 4 || len(board.RightItems) != 4 {
				t.Fatalf("board %s: expected 4 left and 4 right items, got left=%d, right=%d", board.QuestionCode, len(board.LeftItems), len(board.RightItems))
			}
			if len(board.SolutionMap) != 4 {
				t.Fatalf("board %s: expected 4 solution pairs in solution_map, got %d", board.QuestionCode, len(board.SolutionMap))
			}

			switch idx {
			case 0:
				// Board 1: Flawless clear (all 4 matched, 0 mistakes, 15s)
				matches := make(map[string]string)
				for lID, rID := range board.SolutionMap {
					matches[lID] = rID
				}
				boardSubmissions = append(boardSubmissions, dto.MatchBoardAnswerSubmissionDTO{
					QuestionCode: board.QuestionCode,
					Matches:      matches,
					Mistakes:     0,
					TimeTakenMs:  15000,
				})
			case 1:
				// Board 2: Cleared with 2 mistakes (all 4 matched, 2 mistakes, 20s)
				matches := make(map[string]string)
				for lID, rID := range board.SolutionMap {
					matches[lID] = rID
				}
				boardSubmissions = append(boardSubmissions, dto.MatchBoardAnswerSubmissionDTO{
					QuestionCode: board.QuestionCode,
					Matches:      matches,
					Mistakes:     2,
					TimeTakenMs:  20000,
				})
			case 2:
				// Board 3: Partial timeout (2 matched out of 4, 1 mistake, 25s)
				matches := make(map[string]string)
				count := 0
				for lID, rID := range board.SolutionMap {
					matches[lID] = rID
					count++
					if count == 2 {
						break
					}
				}
				boardSubmissions = append(boardSubmissions, dto.MatchBoardAnswerSubmissionDTO{
					QuestionCode: board.QuestionCode,
					Matches:      matches,
					Mistakes:     1,
					TimeTakenMs:  25000,
					IsTimeout:    true,
				})
			}
		}

		// 2. Batch Complete the Match the Following session
		completeBody, _ := json.Marshal(dto.CompleteMatchTheFollowingRequestDTO{
			Session: sessionID,
			Boards:  boardSubmissions,
		})
		completeReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/match-the-following/complete", bytes.NewReader(completeBody))
		completeReq.Header.Set("Authorization", "Bearer "+token)
		completeReq.Header.Set("Content-Type", "application/json")
		completeW := httptest.NewRecorder()
		r.ServeHTTP(completeW, completeReq)

		if completeW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from match-the-following/complete, got %d: %s", completeW.Code, completeW.Body.String())
		}

		var completeResp struct {
			Success bool                                     `json:"success"`
			Data    dto.CompleteMatchTheFollowingResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(completeW.Body.Bytes(), &completeResp); err != nil {
			t.Fatalf("failed to decode complete response: %v", err)
		}

		data := completeResp.Data
		if data.Session != sessionID {
			t.Fatalf("expected session ID %s, got %s", sessionID, data.Session)
		}
		if data.TotalBoards != 3 {
			t.Fatalf("expected 3 total boards, got %d", data.TotalBoards)
		}
		if data.ClearedBoards != 2 {
			t.Fatalf("expected 2 cleared boards, got %d", data.ClearedBoards)
		}
		if data.TotalPairsMatched != 10 {
			t.Fatalf("expected 10 total pairs matched (4+4+2), got %d", data.TotalPairsMatched)
		}
		if data.TotalPairsCount != 12 {
			t.Fatalf("expected 12 total pairs count (4*3), got %d", data.TotalPairsCount)
		}
		if data.TotalMistakes != 3 {
			t.Fatalf("expected 3 total mistakes (0+2+1), got %d", data.TotalMistakes)
		}

		// Board 1 review: 40 base + 10 flawless + 10 speed - 0 mistake = 60
		b1 := data.BoardsReview[0]
		if !b1.IsFlawless || b1.TotalBoardScore != 60 {
			t.Fatalf("board 1 review unexpected: %+v (expected score 60)", b1)
		}

		// Board 2 review: 40 base + 0 flawless + 5 speed - 4 mistake = 41
		b2 := data.BoardsReview[1]
		if b2.IsFlawless || b2.TotalBoardScore != 41 {
			t.Fatalf("board 2 review unexpected: %+v (expected score 41)", b2)
		}

		// Board 3 review: 20 base + 0 flawless + 0 speed - 2 mistake = 18
		b3 := data.BoardsReview[2]
		if b3.TotalBoardScore != 18 {
			t.Fatalf("board 3 review unexpected: %+v (expected score 18)", b3)
		}

		expectedFinalScore := 60 + 41 + 18
		if data.FinalScore != expectedFinalScore {
			t.Fatalf("expected final score %d, got %d", expectedFinalScore, data.FinalScore)
		}

		// Verify score breakdown contains expected keys
		if data.ScoreBreakdown["final_score"] != expectedFinalScore {
			t.Fatalf("score breakdown final_score expected %d, got %d", expectedFinalScore, data.ScoreBreakdown["final_score"])
		}
		if _, ok := data.ScoreBreakdown["base_points"]; !ok {
			t.Fatal("missing base_points in score breakdown")
		}
		if _, ok := data.ScoreBreakdown["flawless_bonus"]; !ok {
			t.Fatal("missing flawless_bonus in score breakdown")
		}
		if _, ok := data.ScoreBreakdown["speed_bonus"]; !ok {
			t.Fatal("missing speed_bonus in score breakdown")
		}
		if _, ok := data.ScoreBreakdown["mistake_penalty"]; !ok {
			t.Fatal("missing mistake_penalty in score breakdown")
		}

		// 3. Second submission should return 409 Conflict
		repeatReq, _ := http.NewRequest(http.MethodPost, "/api/v1/quiz/match-the-following/complete", bytes.NewReader(completeBody))
		repeatReq.Header.Set("Authorization", "Bearer "+token)
		repeatReq.Header.Set("Content-Type", "application/json")
		repeatW := httptest.NewRecorder()
		r.ServeHTTP(repeatW, repeatReq)

		if repeatW.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict on re-completion, got %d", repeatW.Code)
		}
	})
}
