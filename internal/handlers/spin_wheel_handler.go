package handlers

import (
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// GetSpinWheelSegmentsHandler lists the wheel segments and their odds.
// GET /api/v1/spin-wheel/segments
func GetSpinWheelSegmentsHandler(c *gin.Context) {
	res, status, err := services.GetSpinWheelSegments(c.Request.Context())
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Spin wheel segments fetched successfully", res)
}

// GetSpinStatusHandler returns how many spins the player has.
// GET /api/v1/spin-wheel/status
func GetSpinStatusHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.GetSpinStatus(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Spin status fetched successfully", res)
}

// SpinHandler spends one spin and returns the prize. Requires an Idempotency-Key header.
// POST /api/v1/spin-wheel/spin
func SpinHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	req := dto.SpinRequestDTO{IdempotencyKey: c.GetHeader("Idempotency-Key")}
	res, status, err := services.Spin(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Spin completed", res)
}
