package handlers

import (
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// DeleteAccountHandler permanently deletes the signed-in player's account
// after the password is confirmed.
// POST /api/v1/profile/delete
func DeleteAccountHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req dto.DeleteAccountRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid input")
		return
	}
	jti, _ := c.Get("jti")
	exp, _ := c.Get("exp")
	jtiStr, _ := jti.(string)
	var expUnix int64
	switch v := exp.(type) {
	case int64:
		expUnix = v
	case float64:
		expUnix = int64(v)
	}
	status, err := services.DeleteAccount(c.Request.Context(), clientID, jtiStr, expUnix, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	middleware.ClearClientAuthCookies(c)
	response.Success(c, status, "Your account and its data were deleted", nil)
}
