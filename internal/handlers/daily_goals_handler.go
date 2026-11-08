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

// GetDailyGoalsHandler returns today's goals and the player's progress.
// GET /api/v1/daily-goals
func GetDailyGoalsHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.GetDailyGoals(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Daily goals fetched successfully", res)
}

// ClaimDailyGoalHandler pays the reward for a completed goal (once per day).
// POST /api/v1/daily-goals/claim
func ClaimDailyGoalHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req dto.DailyGoalClaimRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	res, status, err := services.ClaimDailyGoal(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Daily goal reward claimed", res)
}
