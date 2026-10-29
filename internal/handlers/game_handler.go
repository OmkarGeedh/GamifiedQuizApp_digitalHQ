package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
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
	ReadBufferSize:    4096,
	WriteBufferSize:   4096,
	EnableCompression: false,
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

// GetSubjectsHandler lists subjects with chapters and per-game question counts.
// GET /api/v1/subjects
func GetSubjectsHandler(c *gin.Context) {
	res, status, err := services.GetSubjects(c.Request.Context())
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Subjects fetched successfully", res)
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

// CompleteSortItOutHandler finalizes a Sort It Out swipe mini-game session.
// POST /api/v1/quiz/sort-it-out/complete
func CompleteSortItOutHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.CompleteSortItOutRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.CompleteSortItOutSession(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Sort It Out completed successfully", res)
}

// CompleteMatchTheFollowingHandler finalizes a Match the Following puzzle mini-game session.
// POST /api/v1/quiz/match-the-following/complete
func CompleteMatchTheFollowingHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.CompleteMatchTheFollowingRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.CompleteMatchTheFollowingSession(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Match the Following completed successfully", res)
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

// isWebSocketRequest checks if an HTTP request is attempting a WebSocket upgrade.
func isWebSocketRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		r.Header.Get("Sec-WebSocket-Key") != "" ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// WebSocketEchoHandler provides an unauthenticated WebSocket echo and diagnostic endpoint.
// It verifies edge proxy upgrade forwarding (HTTP 101 Switching Protocols), platform detection,
// and periodic keep-alive pings on Zoho Catalyst AppSail without requiring a game session.
// GET /ws/echo or GET /api/v1/ws/echo
func WebSocketEchoHandler(c *gin.Context) {
	if isWebSocketRequest(c.Request) {
		c.Request.Header.Set("Connection", "Upgrade")
		c.Request.Header.Set("Upgrade", "websocket")
		if c.Request.Header.Get("Sec-Websocket-Version") == "" {
			c.Request.Header.Set("Sec-Websocket-Version", "13")
		}
	}

	responseHeader := make(http.Header)
	if clientProtocols := websocket.Subprotocols(c.Request); len(clientProtocols) > 0 {
		responseHeader.Set("Sec-WebSocket-Protocol", clientProtocols[0])
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, responseHeader)
	if err != nil {
		log.Printf("[ws-echo] upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	if tcpConn, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(ws.TCPKeepAlive)
	}

	conn.SetReadLimit(ws.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
		return nil
	})

	// Send initial diagnostic info frame
	_ = conn.WriteJSON(gin.H{
		"type": "connected",
		"data": gin.H{
			"status":        "ok",
			"platform":      string(ws.CurrentPlatform),
			"ping_interval": ws.PingInterval.String(),
			"pong_wait":     ws.PongWait.String(),
			"message":       "WebSocket connection established successfully on Zoho Catalyst AppSail",
		},
	})

	ticker := time.NewTicker(ws.PingInterval)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(ws.WriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()
	defer close(done)

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
		_ = conn.SetWriteDeadline(time.Now().Add(ws.WriteWait))
		if msgType == websocket.TextMessage {
			_ = conn.WriteJSON(gin.H{
				"type": "echo",
				"data": string(msg),
			})
		}
	}
}

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
		sessionID = strings.TrimSpace(c.Query("sessionId"))
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(c.Query("session"))
	}
	if sessionID == "" {
		if isWebSocketRequest(c.Request) {
			responseHeader := make(http.Header)
			if clientProtocols := websocket.Subprotocols(c.Request); len(clientProtocols) > 0 {
				responseHeader.Set("Sec-WebSocket-Protocol", clientProtocols[0])
			}
			conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, responseHeader)
			if err == nil {
				_ = conn.WriteJSON(ws.OutboundMessage{
					Type: ws.MsgTypeError,
					Data: map[string]interface{}{
						"code":    "SESSION_REQUIRED",
						"message": "Missing session identifier. Supply ?session_id=<UUID> (or ?sessionId=). Initialize a game session first via POST /api/v1/quiz/sessions/create.",
					},
				})
				_ = conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session_id required"),
					time.Now().Add(2*time.Second),
				)
				_ = conn.Close()
				return
			}
		}
		response.Error(c, http.StatusBadRequest, "session_id query parameter is required")
		return
	}

	// Memory-first check: if active in SessionManager, attach in < 0.1ms without touching Redis/DB
	var session *models.GameSession
	var questions []models.SessionQuestion
	if liveSession, exists := Manager.Get(sessionID); exists && liveSession != nil {
		if liveSession.ClientID != clientID {
			response.Error(c, http.StatusForbidden, "Session does not belong to this user")
			return
		}
		if liveSession.Session.Status != "in_progress" {
			response.Error(c, http.StatusConflict, "Session is not active")
			return
		}
		session = liveSession.Session
		questions = liveSession.Questions
	} else {
		var err error
		session, err = repo.GetSessionByID(c.Request.Context(), sessionID)
		if err != nil {
			if isWebSocketRequest(c.Request) {
				responseHeader := make(http.Header)
				if clientProtocols := websocket.Subprotocols(c.Request); len(clientProtocols) > 0 {
					responseHeader.Set("Sec-WebSocket-Protocol", clientProtocols[0])
				}
				conn, upErr := wsUpgrader.Upgrade(c.Writer, c.Request, responseHeader)
				if upErr == nil {
					_ = conn.WriteJSON(ws.OutboundMessage{
						Type: ws.MsgTypeError,
						Data: map[string]interface{}{
							"code":    "SESSION_NOT_FOUND",
							"message": fmt.Sprintf("Session '%s' not found. It may have expired or been abandoned.", sessionID),
						},
					})
					_ = conn.WriteControl(
						websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Session not found"),
						time.Now().Add(2*time.Second),
					)
					_ = conn.Close()
					return
				}
			}
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
		questions, err = session.GetQuestions()
		if err != nil || len(questions) == 0 {
			// Recover from the same mode-specific pool. Sudden Death must never
			// silently resume with MCQ questions.
			questionType := models.GameModeMCQ
			if session.IsSuddenDeath() {
				questionType = models.GameModeSuddenDeath
			}
			dbQuestions, err := repo.GetQuestionsByTopicAndMode(c.Request.Context(), session.TopicID, session.ChapterNo, questionType, session.TotalQuestions)
			if err != nil || len(dbQuestions) == 0 {
				response.Error(c, http.StatusInternalServerError, "Failed to load session questions")
				return
			}
			if session.IsSuddenDeath() {
				if err := services.ValidateSuddenDeathQuestions(dbQuestions); err != nil {
					response.Error(c, http.StatusInternalServerError, "Failed to load valid Sudden Death questions")
					return
				}
			}
			questions = make([]models.SessionQuestion, 0, len(dbQuestions))
			for _, q := range dbQuestions {
				questions = append(questions, services.ShuffleQuestion(q))
			}
		}
	}

	// Normalize WebSocket handshake headers for edge proxies / gateways (e.g. Zoho Catalyst AppSail,
	// AWS CloudFront, or load balancers that sanitize hop-by-hop Connection headers)
	if isWebSocketRequest(c.Request) {
		c.Request.Header.Set("Connection", "Upgrade")
		c.Request.Header.Set("Upgrade", "websocket")
		if c.Request.Header.Get("Sec-Websocket-Version") == "" {
			c.Request.Header.Set("Sec-Websocket-Version", "13")
		}
	}

	responseHeader := make(http.Header)
	if clientProtocols := websocket.Subprotocols(c.Request); len(clientProtocols) > 0 {
		responseHeader.Set("Sec-WebSocket-Protocol", clientProtocols[0])
	}

	// Upgrade to WebSocket
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, responseHeader)
	if err != nil {
		log.Printf("[ws] upgrade failed for client %d (Connection=%q, Upgrade=%q, Sec-WS-Key=%q): %v",
			clientID, c.Request.Header.Get("Connection"), c.Request.Header.Get("Upgrade"), c.Request.Header.Get("Sec-WebSocket-Key"), err)
		return
	}
	defer conn.Close()

	// Low-latency socket tuning: bypass Nagle's algorithm and enable keepalive
	if tcpConn, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(ws.TCPKeepAlive)
	}

	conn.SetReadLimit(ws.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
		return nil
	})

	// Get or create live session
	ctx := context.Background()
	gs, _ := Manager.GetOrCreate(ctx, sessionID, clientID, session, questions)
	gs.AttachPlayer(conn, clientID)
	defer gs.DetachPlayer(conn)

	log.Printf("[ws] client %d connected to session %s", clientID, sessionID)

	// Read loop: decode incoming messages and push to session inbox.
	// PlayerConn's dedicated writePump safely handles outgoing frames and keep-alive pings.
	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] unexpected close for client %d: %v", clientID, err)
			}
			log.Printf("[ws] client %d disconnected from session %s", clientID, sessionID)
			return
		}

		var msg ws.InboundMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			log.Printf("[ws] invalid JSON from client %d: %v", clientID, err)
			gs.SendError(ws.ErrCodeInvalidPayload, "frame could not be unmarshalled")
			continue
		}

		switch msg.Type {
		case ws.MsgTypeJoinGame:
			gs.PushEvent(ws.MsgTypeJoinGame, msg.Data)
		case ws.MsgTypeSubmitAnswer:
			gs.PushEvent(ws.MsgTypeSubmitAnswer, msg.Data)
		case ws.MsgTypeUsePowerUp:
			gs.PushEvent(ws.MsgTypeUsePowerUp, msg.Data)
		default:
			log.Printf("[ws] unknown message type from client %d: %s", clientID, msg.Type)
			gs.SendError(ws.ErrCodeUnsupportedType, fmt.Sprintf("unsupported message type: %s", msg.Type))
		}
	}
}
