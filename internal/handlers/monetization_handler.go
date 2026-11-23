package handlers

import (
	"net/http"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// withClient runs fn for a signed-in player and writes its result.
func withClient[T any](c *gin.Context, okMessage string, fn func(clientID int) (T, int, error)) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := fn(clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, okMessage, res)
}

// GetAdsConfigHandler tells the app how and whether to show rewarded ads.
// GET /api/v1/ads/config
func GetAdsConfigHandler(c *gin.Context) {
	withClient(c, "Ads config fetched", func(id int) (*dto.AdsConfigDTO, int, error) {
		return services.GetAdsConfig(c.Request.Context(), id)
	})
}

// CreateAdRewardHandler starts a rewarded-ad reward (paid only after Google's
// verified callback), or pays it at once for Quiz Pass holders.
// POST /api/v1/ads/rewards
func CreateAdRewardHandler(c *gin.Context) {
	var req dto.CreateAdRewardRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid input")
		return
	}
	withClient(c, "Ad reward ready", func(id int) (*dto.AdRewardDTO, int, error) {
		return services.CreateAdReward(c.Request.Context(), id, &req)
	})
}

// GetAdRewardHandler returns the state of one ad reward (the app polls it).
// GET /api/v1/ads/rewards/:id
func GetAdRewardHandler(c *gin.Context) {
	withClient(c, "Ad reward fetched", func(id int) (*dto.AdRewardDTO, int, error) {
		return services.GetAdRewardStatus(c.Request.Context(), id, strings.TrimSpace(c.Param("id")))
	})
}

// DevCompleteAdRewardHandler completes an ad reward without Google (local
// testing only; 404 unless ADS_DEV_AUTO_FULFILL=true outside production).
// POST /api/v1/ads/rewards/:id/dev-complete
func DevCompleteAdRewardHandler(c *gin.Context) {
	withClient(c, "Ad reward completed", func(id int) (*dto.AdRewardDTO, int, error) {
		return services.DevCompleteAdReward(c.Request.Context(), id, strings.TrimSpace(c.Param("id")))
	})
}

// AdMobSSVHandler receives AdMob's server-side verification callback. It has
// no player session: the request is trusted only if Google's signature checks out.
// GET /api/v1/ads/ssv
func AdMobSSVHandler(c *gin.Context) {
	status, err := services.HandleAdMobSSV(c.Request.Context(), c.Request.URL.RawQuery)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "ok", nil)
}

// GetPowerUpsHandler returns power-up prices and the player's tokens.
// GET /api/v1/power-ups
func GetPowerUpsHandler(c *gin.Context) {
	withClient(c, "Power-ups fetched", func(id int) (*dto.PowerUpsStatusDTO, int, error) {
		return services.GetPowerUpsStatus(c.Request.Context(), id)
	})
}

// GetEntitlementsHandler returns the Quiz Pass state, owned topic packs and perks.
// GET /api/v1/billing/entitlements
func GetEntitlementsHandler(c *gin.Context) {
	withClient(c, "Entitlements fetched", func(id int) (*dto.EntitlementsDTO, int, error) {
		return services.GetEntitlements(c.Request.Context(), id)
	})
}

// ClaimPassMonthlyGemsHandler pays the Quiz Pass monthly gems (once a month).
// POST /api/v1/pass/monthly-gems
func ClaimPassMonthlyGemsHandler(c *gin.Context) {
	withClient(c, "Monthly gems claimed", func(id int) (*dto.PassClaimResponseDTO, int, error) {
		return services.ClaimPassMonthlyGems(c.Request.Context(), id)
	})
}
