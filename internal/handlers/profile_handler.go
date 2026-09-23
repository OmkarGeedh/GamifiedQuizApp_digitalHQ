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

// GetProfileOptionsHandler returns the dynamic setup options (avatars, classes,
// boards, and subjects) for the 4-step onboarding flow.
func GetProfileOptionsHandler(c *gin.Context) {
	opts, status, err := services.GetProfileSetupOptions(c.Request.Context())
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Profile setup options fetched successfully", opts)
}

// SetupProfileHandler completes the 4-step profile setup onboarding flow.
func SetupProfileHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.ProfileSetupRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.SetupProfile(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Profile setup completed successfully", res)
}

// GetProfileHandler returns the current authenticated client's full profile.
func GetProfileHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	res, status, err := services.GetMyProfile(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Profile fetched successfully", res)
}

// CreateProfileHandler handles legacy profile creation / update.
func CreateProfileHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.CreateProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.CreateMyProfile(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Profile created successfully", res)
}

// UpdateProfileHandler updates profile fields (name, avatar, class, board, subjects, phone).
func UpdateProfileHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.UpdateProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	res, status, err := services.UpdateMyProfile(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Profile updated successfully", res)
}
