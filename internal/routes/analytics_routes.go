package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterAnalyticsRoutes registers the endpoint the app sends usage events to.
func RegisterAnalyticsRoutes(router *gin.Engine) {
	// One limiter for both copies of the routes, so a player's count is shared.
	eventsLimit := middleware.RateLimitPerClient("analytics", 30, time.Minute)

	registerRoutes := func(grp *gin.RouterGroup) {
		grp.POST("/events", eventsLimit, handlers.RecordAnalyticsEventsHandler)
	}

	registerRoutes(router.Group("/api/v1/analytics", middleware.ClientAuthMiddleware))
	registerRoutes(router.Group("/analytics", middleware.ClientAuthMiddleware))
}
