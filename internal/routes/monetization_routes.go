package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

const (
	// Ad reward requests per player per minute: a real player starts one every
	// 30 s at most (server cooldown); this only stops scripts.
	adRewardRequestsPerMinute = 12
	// Polls of an ad reward per player per minute (the app polls ~1/s briefly).
	adRewardPollsPerMinute = 90
	passClaimsPerMinute    = 10
	adRequestMaxBody       = 1 << 10
)

// RegisterMonetizationRoutes registers rewarded ads, power-up status and Quiz
// Pass perks under both the root and /api/v1.
func RegisterMonetizationRoutes(router *gin.Engine) {
	// One limiter per route family, shared by both prefixes.
	createLimit := middleware.RateLimitPerClient("ads_create", adRewardRequestsPerMinute, time.Minute)
	pollLimit := middleware.RateLimitPerClient("ads_poll", adRewardPollsPerMinute, time.Minute)
	passLimit := middleware.RateLimitPerClient("pass_claim", passClaimsPerMinute, time.Minute)

	registerRoutes := func(grp *gin.RouterGroup) {
		ads := grp.Group("/ads")
		// AdMob calls this from Google's servers: no player session, verified by signature.
		ads.GET("/ssv", handlers.AdMobSSVHandler)
		player := ads.Group("", middleware.ClientAuthMiddleware)
		player.GET("/config", handlers.GetAdsConfigHandler)
		player.POST("/rewards", middleware.MaxBodyBytes(adRequestMaxBody), createLimit, handlers.CreateAdRewardHandler)
		player.GET("/rewards/:id", pollLimit, handlers.GetAdRewardHandler)
		player.POST("/rewards/:id/dev-complete", createLimit, handlers.DevCompleteAdRewardHandler)

		grp.GET("/power-ups", middleware.ClientAuthMiddleware, handlers.GetPowerUpsHandler)
		grp.POST("/pass/monthly-gems", middleware.ClientAuthMiddleware, passLimit, handlers.ClaimPassMonthlyGemsHandler)
	}

	registerRoutes(router.Group("/api/v1"))
	registerRoutes(router.Group(""))
}
