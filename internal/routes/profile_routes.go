package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterProfileRoutes registers all profile and onboarding routes.
func RegisterProfileRoutes(router *gin.Engine) {
	// Public options endpoints (for onboarding before login or without token)
	router.GET("/profile/options", handlers.GetProfileOptionsHandler)
	router.GET("/profile/setup-options", handlers.GetProfileOptionsHandler)
	router.GET("/api/v1/profile/options", handlers.GetProfileOptionsHandler)
	router.GET("/api/v1/profile/setup-options", handlers.GetProfileOptionsHandler)

	registerAuthRoutes := func(grp *gin.RouterGroup) {
		// 4-step onboarding submission
		grp.POST("/setup", handlers.SetupProfileHandler)

		// Core profile management
		grp.GET("", handlers.GetProfileHandler)
		grp.POST("", handlers.CreateProfileHandler)
		grp.PUT("", handlers.UpdateProfileHandler)
	}

	// 1. Client-root path: /profile/...
	profileGrp := router.Group("/profile", middleware.ClientAuthMiddleware)
	registerAuthRoutes(profileGrp)

	// 2. Standard REST v1 path: /api/v1/profile/...
	v1ProfileGrp := router.Group("/api/v1/profile", middleware.ClientAuthMiddleware)
	registerAuthRoutes(v1ProfileGrp)
}
