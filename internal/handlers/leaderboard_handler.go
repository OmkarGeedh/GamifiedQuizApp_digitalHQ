package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// GetLeaderboardHandler handles GET /api/v1/leaderboard and GET /leaderboard.
func GetLeaderboardHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok || clientID <= 0 {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	filter := dto.LeaderboardFilterDTO{
		Scope:     c.DefaultQuery("scope", dto.ScopeGlobal),
		TimeRange: c.DefaultQuery("time_range", dto.TimeRangeWeekly),
		GameMode:  c.Query("game_mode"),
		SubjectID: c.Query("subject_id"),
	}

	if pageStr := c.Query("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "page must be a valid integer")
			return
		}
		if p <= 0 {
			response.Error(c, http.StatusBadRequest, "page must be greater than or equal to 1")
			return
		}
		filter.Page = p
	} else {
		filter.Page = dto.DefaultLeaderboardPage
	}

	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "page_size must be a valid integer")
			return
		}
		if ps < 1 || ps > dto.MaxLeaderboardPageSize {
			response.Error(c, http.StatusBadRequest, fmt.Sprintf("page_size must be between 1 and %d", dto.MaxLeaderboardPageSize))
			return
		}
		filter.PageSize = ps
	} else {
		filter.PageSize = dto.DefaultLeaderboardPageSize
	}

	if err := filter.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.GetLeaderboard(c.Request.Context(), clientID, &filter)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Leaderboard fetched successfully", res)
}
