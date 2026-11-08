package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterDailyGoalsRoutes registers the daily goals status and claim endpoints.
func RegisterDailyGoalsRoutes(router *gin.Engine) {
	// One limiter for both copies of the routes, so a player's count is shared.
	claimLimit := middleware.RateLimitPerClient("daily-goals", 30, time.Minute)

	registerRoutes := func(grp *gin.RouterGroup) {
		grp.GET("", handlers.GetDailyGoalsHandler)
		grp.POST("/claim", claimLimit, handlers.ClaimDailyGoalHandler)
	}

	registerRoutes(router.Group("/api/v1/daily-goals", middleware.ClientAuthMiddleware))
	registerRoutes(router.Group("/daily-goals", middleware.ClientAuthMiddleware))
}
