package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

const (
	// Verifications per player per minute. A real purchase needs one (a few
	// with retries); this stops token-guessing and hammering Google's API.
	billingVerifiesPerMinute = 20
	billingVerifyMaxBody     = 4 << 10  // 4 KB: a product id and a token
	billingRTDNMaxBody       = 64 << 10 // Pub/Sub messages are small
)

// RegisterBillingRoutes registers the Google Play Billing endpoints.
func RegisterBillingRoutes(router *gin.Engine) {
	// One limiter for both prefixes so a player's count is shared.
	verifyLimit := middleware.RateLimitPerClient("billing_verify", billingVerifiesPerMinute, time.Minute)

	registerRoutes := func(grp *gin.RouterGroup) {
		player := grp.Group("", middleware.ClientAuthMiddleware)
		player.GET("/products", handlers.GetBillingProductsHandler)
		player.GET("/account-id", handlers.GetBillingAccountHandler)
		player.GET("/subscription", handlers.GetSubscriptionStatusHandler)
		player.POST("/verify", middleware.MaxBodyBytes(billingVerifyMaxBody), verifyLimit, handlers.VerifyPurchaseHandler)

		// Google Pub/Sub push: no player session; authenticated in the handler.
		grp.POST("/rtdn", middleware.MaxBodyBytes(billingRTDNMaxBody), handlers.BillingRTDNHandler)
	}

	// 1. Standard REST v1 path: /api/v1/billing/...
	registerRoutes(router.Group("/api/v1/billing"))

	// 2. Client-root alias path: /billing/...
	registerRoutes(router.Group("/billing"))
}
