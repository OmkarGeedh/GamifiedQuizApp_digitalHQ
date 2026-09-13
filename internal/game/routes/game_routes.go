package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/handler"
	gameWs "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/ws"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterGameRoutes registers all MCQ game REST and WebSocket routes.
func RegisterGameRoutes(router *gin.Engine) {
	// Initialize the shared WebSocket session manager
	handler.Manager = gameWs.NewSessionManager()

	// REST API v1 routes (all authenticated)
	v1 := router.Group("/api/v1", middleware.ClientAuthMiddleware)
	{
		// Questions
		v1.GET("/topics/:topic_id/questions", handler.GetTopicQuestionsHandler)

		// Quiz sessions
		quiz := v1.Group("/quiz")
		{
			quiz.POST("/sessions/create", handler.CreateQuizSessionHandler)
			quiz.POST("/sessions/complete", handler.CompleteSessionHandler)
			quiz.POST("/sessions/abandon", handler.AbandonSessionHandler)
			quiz.POST("/answers/evaluate", handler.EvaluateAnswerHandler)
			quiz.POST("/power-ups/fifty-fifty", handler.FiftyFiftyHandler)
			quiz.GET("/history", handler.GetGameHistoryHandler)
		}
	}

	// WebSocket route (authentication handled inside the handler)
	router.GET("/ws/game", middleware.ClientAuthMiddleware, handler.WebSocketGameHandler)
}
