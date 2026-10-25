package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterLeaderboardRoutes registers authenticated leaderboard endpoints under root and /api/v1.
func RegisterLeaderboardRoutes(router *gin.Engine) {
	registerRoutes := func(grp *gin.RouterGroup) {
		grp.GET("/leaderboard", handlers.GetLeaderboardHandler)
	}

	// 1. Client-root alias (all authenticated): /leaderboard
	root := router.Group("", middleware.ClientAuthMiddleware)
	registerRoutes(root)

	// 2. Standard REST API v1 route (all authenticated): /api/v1/leaderboard
	v1 := router.Group("/api/v1", middleware.ClientAuthMiddleware)
	registerRoutes(v1)
}
