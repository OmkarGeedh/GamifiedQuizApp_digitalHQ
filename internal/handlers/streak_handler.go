package handlers

import (
	"fmt"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// GetStreakStatusHandler returns the player's streak and milestone rewards.
// GET /api/v1/streak
func GetStreakStatusHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.GetStreakStatus(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Streak fetched successfully", res)
}

// BuyStreakFreezeHandler sells the player one streak freeze for gems.
// POST /api/v1/streak/freeze
func BuyStreakFreezeHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.BuyStreakFreeze(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Streak freeze bought", res)
}

// ClaimStreakMilestoneHandler pays the reward for a reached streak milestone.
// POST /api/v1/streak/claim
func ClaimStreakMilestoneHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req dto.StreakClaimRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	res, status, err := services.ClaimStreakMilestone(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Streak reward claimed", res)
}
