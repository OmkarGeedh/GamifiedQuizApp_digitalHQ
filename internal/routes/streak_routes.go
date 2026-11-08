package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterStreakRoutes registers the streak status and milestone claim endpoints.
func RegisterStreakRoutes(router *gin.Engine) {
	// One limiter for both copies of the routes, so a player's count is shared.
	claimLimit := middleware.RateLimitPerClient("streak", 30, time.Minute)

	registerRoutes := func(grp *gin.RouterGroup) {
		grp.GET("", handlers.GetStreakStatusHandler)
		grp.POST("/claim", claimLimit, handlers.ClaimStreakMilestoneHandler)
		grp.POST("/freeze", claimLimit, handlers.BuyStreakFreezeHandler)
	}

	registerRoutes(router.Group("/api/v1/streak", middleware.ClientAuthMiddleware))
	registerRoutes(router.Group("/streak", middleware.ClientAuthMiddleware))
}
