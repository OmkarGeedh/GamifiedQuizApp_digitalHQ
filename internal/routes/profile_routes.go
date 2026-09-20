package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterProfileRoutes registers all profile and onboarding routes.
func RegisterProfileRoutes(router *gin.Engine) {
	profileGrp := router.Group("/profile", middleware.ClientAuthMiddleware)
	{
		// Dynamic profile setup options (avatars, classes, boards, subjects)
		profileGrp.GET("/options", handlers.GetProfileOptionsHandler)
		profileGrp.GET("/setup-options", handlers.GetProfileOptionsHandler)

		// 4-step onboarding submission
		profileGrp.POST("/setup", handlers.SetupProfileHandler)

		// Core profile management
		profileGrp.GET("", handlers.GetProfileHandler)
		profileGrp.POST("", handlers.CreateProfileHandler)
		profileGrp.PUT("", handlers.UpdateProfileHandler)
	}

	// Public access alias for onboarding options if frontend needs it before session initialization
	router.GET("/api/v1/profile/options", handlers.GetProfileOptionsHandler)
}
