package handlers

import (
	"fmt"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

func LoginHandler(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	accessStr, refreshStr, clientRecord, status, err := services.LoginService(c.Request.Context(), &req)
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

	response.Success(c, status, "Client logged in successfully", dto.LoginSuccessResponse{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		TokenType:    "Bearer",
		ExpiresIn:    accessExpirySeconds,
		Client:       clientResp,
		Message:      "Authentication successful.",
	})
}

func LogoutClientHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var jti string
	if jtiVal, exists := c.Get("jti"); exists {
		jti, _ = jtiVal.(string)
	}

	var exp int64
	if expVal, exists := c.Get("exp"); exists {
		if expFloat, ok := expVal.(float64); ok {
			exp = int64(expFloat)
		}
	}

	status, err := services.LogoutClientService(c.Request.Context(), clientID, jti, exp)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	utils.ClearClientAuthCookies(c)

	response.Success(c, status, "Client logged out successfully", gin.H{
		"message": "Logged out successfully and cookies cleared.",
	})
}

func ClientRefreshTokenHandler(c *gin.Context) {
	refreshStr, _ := c.Cookie("clientRefreshToken")
	if refreshStr == "" {
		var bodyReq struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := c.ShouldBindJSON(&bodyReq); err == nil {
			refreshStr = bodyReq.RefreshToken
		}
	}

	if refreshStr == "" {
		response.Error(c, http.StatusBadRequest, "No refresh token found in cookies or request body")
		return
	}

	newAccessStr, status, err := services.RefreshTokenService(c.Request.Context(), refreshStr)
	if err != nil {
		utils.ClearClientAuthCookies(c)
		response.Error(c, status, err.Error())
		return
	}

	utils.SetClientAccessTokenCookie(c, newAccessStr)

	response.Success(c, status, "Token refreshed successfully", gin.H{
		"message":     "Access token refreshed and updated in cookie.",
		"accessToken": newAccessStr,
	})
}

func ClientVerifySessionHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	res, status, err := services.ClientVerifySessionService(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	msg := "Client fetched successfully"
	if !res.Active {
		msg = "Client not found"
	}

	response.Success(c, status, msg, res)
}

func ClientSessionHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	res, status, err := services.ClientSessionService(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Client fetched successfully", res)
}

func ChangePasswordRequestHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := input.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	otp, status, err := services.ChangePasswordRequestService(c.Request.Context(), clientID, &input)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "OTP generated successfully. Verify to confirm password change.", gin.H{
		"message": "OTP generated successfully. Verify to confirm password change.",
		"otp":     otp,
	})
}

func ChangePasswordVerifyHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input dto.ChangePasswordVerifyRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := input.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.ChangePasswordVerifyService(c.Request.Context(), clientID, &input)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Password updated successfully", gin.H{})
}

func RegisterValidateBasicHandler(c *gin.Context) {
	var input dto.RegisterValidateBasicRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := input.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.RegisterValidateBasicService(c.Request.Context(), &input)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Client registered successfully", gin.H{})
}

func CheckEmailVerifyHandler(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		response.Error(c, http.StatusBadRequest, "email query parameter is required")
		return
	}

	exists, status, err := services.CheckEmailVerifyService(c.Request.Context(), email)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Email checked successfully", gin.H{
		"exists": exists,
	})
}

func RegisterEmailRequestHandler(c *gin.Context) {
	var req dto.RegisterEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	otp, status, err := services.RegisterEmailRequestService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Registration email OTP sent successfully", gin.H{
		"message": "Registration email OTP sent successfully",
		"otp":     otp,
	})
}

func RegisterEmailVerifyHandler(c *gin.Context) {
	var req dto.RegisterEmailVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.RegisterEmailVerifyService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Email verified successfully", gin.H{})
}

func RegisterPhoneOtpRequestHandler(c *gin.Context) {
	var req dto.RegisterPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	otp, status, err := services.RegisterPhoneOtpRequestService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Registration phone OTP sent successfully", gin.H{
		"message": "Registration phone OTP sent successfully",
		"otp":     otp,
	})
}

func RegisterPhoneOtpVerifyHandler(c *gin.Context) {
	var req dto.RegisterPhoneVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.RegisterPhoneOtpVerifyService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Phone verified successfully", gin.H{})
}

func ForgotPasswordRequestHandler(c *gin.Context) {
	var req dto.ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	token, status, err := services.ForgotPasswordRequestService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Password reset token/link generated successfully", gin.H{
		"message": "Password reset token/link generated successfully",
		"token":   token,
	})
}

func ForgotPasswordVerifyTokenHandler(c *gin.Context) {
	var req dto.ForgotPasswordVerifyTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.ForgotPasswordVerifyTokenService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Reset token is valid", gin.H{})
}

func ResetPasswordVerifyHandler(c *gin.Context) {
	var req dto.ResetPasswordVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	status, err := services.ResetPasswordVerifyService(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Password has been reset successfully", gin.H{})
}
