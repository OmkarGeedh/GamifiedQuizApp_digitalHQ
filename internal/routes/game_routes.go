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

	// REST API v1 routes (all authenticated)
	v1 := router.Group("/api/v1", middleware.ClientAuthMiddleware)
	{
		// Questions
		v1.GET("/topics/:topic_id/questions", handlers.GetTopicQuestionsHandler)

		// Quiz sessions
		quiz := v1.Group("/quiz")
		{
			quiz.POST("/sessions/create", handlers.CreateQuizSessionHandler)
			quiz.POST("/sessions/complete", handlers.CompleteSessionHandler)
			quiz.POST("/sessions/abandon", handlers.AbandonSessionHandler)
			quiz.POST("/answers/evaluate", handlers.EvaluateAnswerHandler)
			quiz.POST("/power-ups/fifty-fifty", handlers.FiftyFiftyHandler)
			quiz.GET("/history", handlers.GetGameHistoryHandler)
		}
	}

	// WebSocket route (authentication handled inside the handler)
	router.GET("/ws/game", middleware.ClientAuthMiddleware, handlers.WebSocketGameHandler)
}
