package handlers

import (
	"fmt"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// -----------------------------------------------------------------------------
// SIGNUP FLOW HANDLERS
// -----------------------------------------------------------------------------

// SignupSendCodeHandler handles Step 1 of registration: validates details, enforces
// cooldown & rate limit, generates 6-digit OTP, and saves the pending signup session.
func SignupSendCodeHandler(c *gin.Context) {
	var req dto.SignupSendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}

	ip := c.ClientIP()
	res, status, err := services.SignupSendCodeService(c.Request.Context(), ip, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, res.Message, res)
}

// SignupResendCodeHandler handles resending verification code with cooldown enforcement.
func SignupResendCodeHandler(c *gin.Context) {
	var req dto.SignupResendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}

	ip := c.ClientIP()
	res, status, err := services.SignupResendCodeService(c.Request.Context(), ip, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, res.Message, res)
}

// SignupVerifyHandler handles Step 2 of registration: verifies the 6-digit code, creates
// the user account in PostgreSQL, issues JWT tokens & HTTP-only cookies, and logs the user in.
func SignupVerifyHandler(c *gin.Context) {
	var req dto.SignupVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}

	accessStr, refreshStr, clientRecord, status, err := services.SignupVerifyService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	// Set HTTP-only secure cookies for tokens
	utils.SetClientAuthCookies(c, accessStr, refreshStr)

	accessExpirySeconds := 30 * 24 * 60 * 60
	if config.AppConfig != nil && config.AppConfig.Server.JWTAccessTokenExpiry > 0 {
		accessExpirySeconds = int(config.AppConfig.Server.JWTAccessTokenExpiry.Seconds())
	}

	var clientResp *dto.ClientSessionResponse
	if clientRecord != nil {
		clientResp = &dto.ClientSessionResponse{
			ID:        clientRecord.ID,
			Username:  clientRecord.Username,
			Email:     clientRecord.Email,
			Phone:     clientRecord.Phone,
			Status:    clientRecord.Status,
			CreatedAt: clientRecord.CreatedAt,
			UpdatedAt: clientRecord.UpdatedAt,
		}
	}

	response.Success(c, status, "Account created and verified successfully! Welcome aboard.", dto.LoginSuccessResponse{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		TokenType:    "Bearer",
		ExpiresIn:    accessExpirySeconds,
		Client:       clientResp,
		Message:      "Account created and verified successfully.",
	})
}
