package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Manager is the shared session manager for active WebSocket games.
// Initialized once in RegisterGameRoutes and shared across handlers.
var Manager *ws.SessionManager

// wsUpgrader configures the gorilla/websocket upgrader.
// CheckOrigin is permissive for development; restrict in production.
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// --- REST Handlers ---

// GetTopicQuestionsHandler returns randomized MCQ questions for a topic.
// GET /api/v1/topics/:topic_id/questions
func GetTopicQuestionsHandler(c *gin.Context) {
	topicID := strings.TrimSpace(c.Param("topic_id"))
	if topicID == "" {
		response.Error(c, http.StatusBadRequest, "topic_id is required")
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	}

	res, status, err := services.GetTopicQuestions(c.Request.Context(), topicID, limit)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Questions fetched successfully", res)
}

// CreateQuizSessionHandler creates a new quiz session and returns questions.
// POST /api/v1/quiz/sessions/create
func CreateQuizSessionHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.CreateSessionRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.CreateSession(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Quiz session created successfully", res)
}

// EvaluateAnswerHandler evaluates a submitted answer.
// POST /api/v1/quiz/answers/evaluate
func EvaluateAnswerHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.SubmitAnswerRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.EvaluateAnswer(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Answer evaluated successfully", res)
}

// FiftyFiftyHandler applies the 50:50 power-up.
// POST /api/v1/quiz/power-ups/fifty-fifty
func FiftyFiftyHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.FiftyFiftyRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.ApplyFiftyFifty(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "50:50 power-up applied", res)
}

// CompleteSessionHandler finalizes a session and awards rewards.
// POST /api/v1/quiz/sessions/complete
func CompleteSessionHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.CompleteSessionRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.CompleteSession(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Quiz completed", res)
}

// AbandonSessionHandler abandons an active session.
// POST /api/v1/quiz/sessions/abandon
func AbandonSessionHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.CompleteSessionRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.AbandonSession(c.Request.Context(), clientID, req.Session)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Session abandoned", nil)
}

// GetGameHistoryHandler returns a player's game session history.
// GET /api/v1/quiz/history
func GetGameHistoryHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	sessions, total, err := repo.GetClientGameHistory(c.Request.Context(), clientID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to fetch game history")
		return
	}

	response.Success(c, http.StatusOK, "Game history fetched successfully", gin.H{
		"sessions": sessions,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// --- WebSocket Handler ---

// WebSocketGameHandler upgrades an HTTP connection to WebSocket and binds
// the client to a live game session for real-time Sudden Death gameplay.
// GET /ws/game
func WebSocketGameHandler(c *gin.Context) {
	// Authenticate: try cookie first, then query param token
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, "session_id query parameter is required")
		return
	}

	// Validate session exists and belongs to this client
	session, err := repo.GetSessionByID(c.Request.Context(), sessionID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "Session not found")
		return
	}
	if session.ClientID != clientID {
		response.Error(c, http.StatusForbidden, "Session does not belong to this user")
		return
	}
	if session.Status != "in_progress" {
		response.Error(c, http.StatusConflict, "Session is not active")
		return
	}
	if services.CheckAndAbandonIfExpired(c.Request.Context(), session) {
		response.Error(c, http.StatusConflict, "quiz session expired due to 5 minutes of inactivity and has been abandoned")
		return
	}

	// Fetch session questions from session state
	questions, err := session.GetQuestions()
	if err != nil || len(questions) == 0 {
		// Fallback to DB questions if session has no persisted state
		dbQuestions, err := repo.GetQuestionsByTopic(c.Request.Context(), session.TopicID, session.TotalQuestions)
		if err != nil || len(dbQuestions) == 0 {
			response.Error(c, http.StatusInternalServerError, "Failed to load session questions")
			return
		}
		questions = make([]models.SessionQuestion, 0, len(dbQuestions))
		for _, q := range dbQuestions {
			questions = append(questions, services.ShuffleQuestion(q))
		}
	}

	// Upgrade to WebSocket
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed for client %d: %v", clientID, err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Get or create live session
	ctx := context.Background()
	gs, _ := Manager.GetOrCreate(ctx, sessionID, clientID, session, questions)
	gs.AttachPlayer(conn, clientID)

	log.Printf("[ws] client %d connected to session %s", clientID, sessionID)

	// Ping ticker to keep connection alive
	pingTicker := time.NewTicker(54 * time.Second)
	defer pingTicker.Stop()

	// Read loop: decode incoming messages and push to session inbox
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, rawMsg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("[ws] unexpected close for client %d: %v", clientID, err)
				}
				return
			}

			var msg ws.InboundMessage
			if err := json.Unmarshal(rawMsg, &msg); err != nil {
				log.Printf("[ws] invalid JSON from client %d: %v", clientID, err)
				continue
			}

			switch msg.Type {
			case ws.MsgTypeSubmitAnswer:
				gs.PushEvent(ws.MsgTypeSubmitAnswer, msg.Data)
			default:
				log.Printf("[ws] unknown message type from client %d: %s", clientID, msg.Type)
			}
		}
	}()

	// Keep alive loop
	for {
		select {
		case <-done:
			log.Printf("[ws] client %d disconnected from session %s", clientID, sessionID)
			return
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws] ping failed for client %d: %v", clientID, err)
				return
			}
		}
	}
}
