package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/handler"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterClientRoutes(router *gin.Engine) {
	forgotRateLimit := middleware.RateLimitWithLimit("forgot-pwd", 5, 10*time.Minute)
	emailReqRateLimit := middleware.RateLimitWithLimit("register-email", 5, 10*time.Minute)
	phoneReqRateLimit := middleware.RateLimitWithLimit("register-phone", 5, 10*time.Minute)

	// Register routes on a group helper
	registerGroup := func(grp *gin.RouterGroup) {
		grp.POST("/login", handler.LoginHandler)
		grp.POST("/refresh", handler.ClientRefreshTokenHandler)
		grp.POST("/logout", middleware.ClientAuthMiddleware, handler.LogoutClientHandler)
		grp.POST("/forgot-password", forgotRateLimit, handler.ForgotPasswordRequestHandler)
		grp.POST("/forgot-password/verify", handler.ForgotPasswordVerifyTokenHandler)
		grp.POST("/forgot-password/reset", handler.ResetPasswordVerifyHandler)
		grp.POST("/register/email-request", emailReqRateLimit, handler.RegisterEmailRequestHandler)
		grp.POST("/register/email-verify", handler.RegisterEmailVerifyHandler)
		grp.POST("/register/phone-request", phoneReqRateLimit, handler.RegisterPhoneOtpRequestHandler)
		grp.POST("/register/phone-verify", handler.RegisterPhoneOtpVerifyHandler)
		grp.POST("/register/validate-basic", handler.RegisterValidateBasicHandler)
		grp.GET("/register/email-check", handler.CheckEmailVerifyHandler)
		grp.GET("/session", middleware.ClientAuthMiddleware, handler.ClientSessionHandler)
		grp.GET("/verify-session", middleware.ClientAuthMiddleware, handler.ClientVerifySessionHandler)
	}

	// 1. Root /auth routes
	authgrp := router.Group("/auth", middleware.RateLimit())
	registerGroup(authgrp)

	// 2. /api/v1/auth routes
	v1AuthGrp := router.Group("/api/v1/auth", middleware.RateLimit())
	registerGroup(v1AuthGrp)

	// Session Routes (convenience root aliases)
	router.GET("/verify-session", middleware.ClientAuthMiddleware, handler.ClientVerifySessionHandler)
	router.GET("/session", middleware.ClientAuthMiddleware, handler.ClientSessionHandler)
	router.GET("/api/v1/session", middleware.ClientAuthMiddleware, handler.ClientSessionHandler)

	// Password Routes
	router.POST("/change-password-request", middleware.ClientAuthMiddleware, handler.ChangePasswordRequestHandler)
	router.POST("/change-password-verify", middleware.ClientAuthMiddleware, handler.ChangePasswordVerifyHandler)
	router.POST("/api/v1/change-password-request", middleware.ClientAuthMiddleware, handler.ChangePasswordRequestHandler)
	router.POST("/api/v1/change-password-verify", middleware.ClientAuthMiddleware, handler.ChangePasswordVerifyHandler)
}
