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

// maxAnalyticsBodyBytes caps a batch; 25 small events are far below it.
const maxAnalyticsBodyBytes = 64 << 10

// RecordAnalyticsEventsHandler stores the events the app reports.
// POST /api/v1/analytics/events
func RecordAnalyticsEventsHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAnalyticsBodyBytes)

	var req dto.AnalyticsBatchDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	res, status, err := services.RecordAnalyticsEvents(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Events received", res)
}
