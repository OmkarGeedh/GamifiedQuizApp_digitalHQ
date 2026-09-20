package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterAuthRoutes registers all authentication, session, and password management routes
// under both root paths (/auth, /session) and standard REST v1 paths (/api/v1/auth, /api/v1/session).
func RegisterAuthRoutes(router *gin.Engine) {
	registerAuthWithPrefix := func(base *gin.RouterGroup) {
		// 1. Core Authentication Routes under /auth
		authGrp := base.Group("/auth", middleware.RateLimit())
		{
			authGrp.POST("/login", handlers.LoginHandler)
			authGrp.POST("/refresh", handlers.ClientRefreshTokenHandler)
			authGrp.POST("/logout", middleware.ClientAuthMiddleware, handlers.LogoutClientHandler)

			// Password recovery (rate limited)
			forgotRateLimit := middleware.RateLimitWithLimit("forgot-pwd", 5, 10*time.Minute)
			authGrp.POST("/forgot-password", forgotRateLimit, handlers.ForgotPasswordRequestHandler)
			authGrp.POST("/forgot-password/verify", handlers.ForgotPasswordVerifyTokenHandler)
			authGrp.POST("/forgot-password/reset", handlers.ResetPasswordVerifyHandler)
		}

		// 2. Dedicated Signup Flow Routes (/auth/signup/send-code, /resend-code, /verify)
		signupGrp := base.Group("/auth/signup", middleware.RateLimit())
		{
			// Step 1: Submit details, enforce 60s cooldown & rate limit, dispatch 6-digit OTP
			signupGrp.POST("/send-code", handlers.SignupSendCodeHandler)

			// Resend verification code (cooldown-guarded)
			signupGrp.POST("/resend-code", handlers.SignupResendCodeHandler)

			// Step 2: Verify 6-digit OTP, create DB user, issue JWTs & cookies, log in user
			signupGrp.POST("/verify", handlers.SignupVerifyHandler)
		}

		// 3. Authenticated Session & Password Change Routes
		authRequired := base.Group("", middleware.ClientAuthMiddleware)
		{
			authRequired.GET("/verify-session", handlers.ClientVerifySessionHandler)
			authRequired.GET("/session", handlers.ClientSessionHandler)
			authRequired.POST("/change-password-request", handlers.ChangePasswordRequestHandler)
			authRequired.POST("/change-password-verify", handlers.ChangePasswordVerifyHandler)
		}
	}

	// 1. Register on root group (/auth/..., /session, etc.)
	rootGrp := router.Group("")
	registerAuthWithPrefix(rootGrp)

	// 2. Register on standard REST v1 group (/api/v1/auth/..., /api/v1/session, etc.)
	v1Grp := router.Group("/api/v1")
	registerAuthWithPrefix(v1Grp)
}

// RegisterClientRoutes is an alias for RegisterAuthRoutes for backward compatibility.
var RegisterClientRoutes = RegisterAuthRoutes
