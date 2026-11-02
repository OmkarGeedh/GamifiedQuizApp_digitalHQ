package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterSpinWheelRoutes registers the spin wheel segments, status and spin endpoints.
func RegisterSpinWheelRoutes(router *gin.Engine) {
	registerRoutes := func(grp *gin.RouterGroup) {
		grp.GET("/segments", handlers.GetSpinWheelSegmentsHandler)
		grp.GET("/status", handlers.GetSpinStatusHandler)
		grp.POST("/spin", middleware.RateLimitWithLimit("spin", 30, time.Minute), handlers.SpinHandler)
	}

	registerRoutes(router.Group("/api/v1/spin-wheel", middleware.ClientAuthMiddleware))
	registerRoutes(router.Group("/spin-wheel", middleware.ClientAuthMiddleware))
}
