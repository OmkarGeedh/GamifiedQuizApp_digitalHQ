package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterProfileRoutes(router *gin.Engine) {
	profileGrp := router.Group("/profile", middleware.ClientAuthMiddleware)
	{
		profileGrp.GET("", handlers.GetProfileHandler)
		profileGrp.POST("", handlers.CreateProfileHandler)
		profileGrp.PUT("", handlers.UpdateProfileHandler)
	}
}
