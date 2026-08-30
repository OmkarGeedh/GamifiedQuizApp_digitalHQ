package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/handler"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterClientRoutes(router *gin.Engine) {
	// Custom rate limit policies for high-risk endpoints (5 requests per 10 minutes)
	otpRateLimit := middleware.RateLimitWithLimit("login-otp-gen", 5, 10*time.Minute)
	forgotRateLimit := middleware.RateLimitWithLimit("forgot-pwd", 5, 10*time.Minute)
	emailReqRateLimit := middleware.RateLimitWithLimit("register-email", 5, 10*time.Minute)
	phoneReqRateLimit := middleware.RateLimitWithLimit("register-phone", 5, 10*time.Minute)

	// Auth Routes
	authgrp := router.Group("/auth", middleware.RateLimit())
	{
		authgrp.POST("/login/otp", otpRateLimit, handler.LoginGenOTPHandler)
		authgrp.POST("/login/otp/resend", otpRateLimit, handler.LoginResendOTPHandler)
		authgrp.POST("/login/verify-otp", handler.LoginOTPVerifyHandler)
		authgrp.POST("/refresh", handler.ClientRefreshTokenHandler)
		authgrp.POST("/logout", middleware.ClientAuthMiddleware, handler.LogoutClientHandler)
		authgrp.POST("/forgot-password", forgotRateLimit, handler.ForgotPasswordRequestHandler)
		authgrp.POST("/forgot-password/verify", handler.ForgotPasswordVerifyTokenHandler)
		authgrp.POST("/forgot-password/reset", handler.ResetPasswordVerifyHandler)
		authgrp.POST("/register/email-request", emailReqRateLimit, handler.RegisterEmailRequestHandler)
		authgrp.POST("/register/email-verify", handler.RegisterEmailVerifyHandler)
		authgrp.POST("/register/phone-request", phoneReqRateLimit, handler.RegisterPhoneOtpRequestHandler)
		authgrp.POST("/register/phone-verify", handler.RegisterPhoneOtpVerifyHandler)
		authgrp.POST("/register/validate-basic", handler.RegisterValidateBasicHandler)
		authgrp.GET("/register/email-check", handler.CheckEmailVerifyHandler)
	}

	// Session Routes
	router.GET("/verify-session", middleware.ClientAuthMiddleware, handler.ClientVerifySessionHandler)
	router.GET("/session", middleware.ClientAuthMiddleware, handler.ClientSessionHandler)

	// Password Routes
	router.POST("/change-password-request", middleware.ClientAuthMiddleware, handler.ChangePasswordRequestHandler)
	router.POST("/change-password-verify", middleware.ClientAuthMiddleware, handler.ChangePasswordVerifyHandler)
}
