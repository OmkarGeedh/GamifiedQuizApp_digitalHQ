package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/handler"
	"github.com/gin-gonic/gin"
)

func RegisterProfileRoutes(router *gin.Engine) {
	profileGrp := router.Group("/profile", middleware.ClientAuthMiddleware)
	{
		profileGrp.GET("", handler.GetProfileHandler)
		profileGrp.POST("", handler.CreateProfileHandler)
		profileGrp.PUT("", handler.UpdateProfileHandler)
	}
}
