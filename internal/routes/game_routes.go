package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"github.com/gin-gonic/gin"
)

// RegisterGameRoutes registers all MCQ game REST and WebSocket routes.
func RegisterGameRoutes(router *gin.Engine) {
	// Initialize the shared WebSocket session manager
	handlers.Manager = ws.NewSessionManager()

	registerQuizRoutes := func(grp *gin.RouterGroup) {
		grp.POST("/sessions/create", handlers.CreateQuizSessionHandler)
		grp.POST("/sessions/complete", handlers.CompleteSessionHandler)
		grp.POST("/sessions/abandon", handlers.AbandonSessionHandler)
		grp.POST("/answers/evaluate", handlers.EvaluateAnswerHandler)
		grp.POST("/sessions/answer", handlers.EvaluateAnswerHandler)
		grp.POST("/answer", handlers.EvaluateAnswerHandler)
		grp.POST("/power-ups/fifty-fifty", handlers.FiftyFiftyHandler)
		grp.POST("/sessions/fifty-fifty", handlers.FiftyFiftyHandler)
		grp.POST("/fifty-fifty", handlers.FiftyFiftyHandler)
		grp.POST("/sort-it-out/complete", handlers.CompleteSortItOutHandler)
		grp.POST("/match-the-following/complete", handlers.CompleteMatchTheFollowingHandler)
		grp.GET("/history", handlers.GetGameHistoryHandler)
	}

	// 1. REST API v1 routes (all authenticated)
	v1 := router.Group("/api/v1", middleware.ClientAuthMiddleware)
	{
		v1.GET("/topics/:topic_id/questions", handlers.GetTopicQuestionsHandler)
		v1.GET("/subjects", handlers.GetSubjectsHandler)
		v1Quiz := v1.Group("/quiz")
		registerQuizRoutes(v1Quiz)
	}

	// 2. Client-root aliases (all authenticated)
	root := router.Group("", middleware.ClientAuthMiddleware)
	{
		root.GET("/topics/:topic_id/questions", handlers.GetTopicQuestionsHandler)
		root.GET("/subjects", handlers.GetSubjectsHandler)
		rootQuiz := root.Group("/quiz")
		registerQuizRoutes(rootQuiz)
	}

	// WebSocket game routes (authenticated, supporting /ws/game, /ws, and /api/v1 variants)
	wsRoutes := []string{
		"/ws/game",
		"/ws/game/",
		"/api/v1/ws/game",
		"/api/v1/ws/game/",
		"/ws",
		"/ws/",
		"/api/v1/ws",
		"/api/v1/ws/",
	}
	for _, p := range wsRoutes {
		router.GET(p, middleware.ClientAuthMiddleware, handlers.WebSocketGameHandler)
	}

	// WebSocket diagnostic echo probe routes (unauthenticated for instant connectivity verification)
	echoRoutes := []string{
		"/ws/echo",
		"/ws/echo/",
		"/api/v1/ws/echo",
		"/api/v1/ws/echo/",
		"/ws/ping",
		"/api/v1/ws/ping",
	}
	for _, p := range echoRoutes {
		router.GET(p, handlers.WebSocketEchoHandler)
	}
}
