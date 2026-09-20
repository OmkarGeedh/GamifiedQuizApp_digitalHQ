package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterAuthRoutes(router *gin.Engine) {
	forgotRateLimit := middleware.RateLimitWithLimit("forgot-pwd", 5, 10*time.Minute)
	emailReqRateLimit := middleware.RateLimitWithLimit("register-email", 5, 10*time.Minute)
	phoneReqRateLimit := middleware.RateLimitWithLimit("register-phone", 5, 10*time.Minute)

	// Register routes on a group helper
	registerGroup := func(grp *gin.RouterGroup) {
		grp.POST("/login", handlers.LoginHandler)
		grp.POST("/refresh", handlers.ClientRefreshTokenHandler)
		grp.POST("/logout", middleware.ClientAuthMiddleware, handlers.LogoutClientHandler)
		grp.POST("/forgot-password", forgotRateLimit, handlers.ForgotPasswordRequestHandler)
		grp.POST("/forgot-password/verify", handlers.ForgotPasswordVerifyTokenHandler)
		grp.POST("/forgot-password/reset", handlers.ResetPasswordVerifyHandler)
		grp.POST("/register/email-request", emailReqRateLimit, handlers.RegisterEmailRequestHandler)
		grp.POST("/register/email-verify", handlers.RegisterEmailVerifyHandler)
		grp.POST("/register/phone-request", phoneReqRateLimit, handlers.RegisterPhoneOtpRequestHandler)
		grp.POST("/register/phone-verify", handlers.RegisterPhoneOtpVerifyHandler)
		grp.POST("/register/validate-basic", handlers.RegisterValidateBasicHandler)
		grp.GET("/register/email-check", handlers.CheckEmailVerifyHandler)
		grp.GET("/session", middleware.ClientAuthMiddleware, handlers.ClientSessionHandler)
		grp.GET("/verify-session", middleware.ClientAuthMiddleware, handlers.ClientVerifySessionHandler)
	}

	// 1. Root /auth routes
	authgrp := router.Group("/auth", middleware.RateLimit())
	registerGroup(authgrp)

	// 2. /api/v1/auth routes
	v1AuthGrp := router.Group("/api/v1/auth", middleware.RateLimit())
	registerGroup(v1AuthGrp)

	// Session Routes (convenience root aliases)
	router.GET("/verify-session", middleware.ClientAuthMiddleware, handlers.ClientVerifySessionHandler)
	router.GET("/session", middleware.ClientAuthMiddleware, handlers.ClientSessionHandler)
	router.GET("/api/v1/session", middleware.ClientAuthMiddleware, handlers.ClientSessionHandler)

	// Password Routes
	router.POST("/change-password-request", middleware.ClientAuthMiddleware, handlers.ChangePasswordRequestHandler)
	router.POST("/change-password-verify", middleware.ClientAuthMiddleware, handlers.ChangePasswordVerifyHandler)
	router.POST("/api/v1/change-password-request", middleware.ClientAuthMiddleware, handlers.ChangePasswordRequestHandler)
	router.POST("/api/v1/change-password-verify", middleware.ClientAuthMiddleware, handlers.ChangePasswordVerifyHandler)
}

// RegisterClientRoutes is an alias for RegisterAuthRoutes for backward compatibility.
var RegisterClientRoutes = RegisterAuthRoutes
