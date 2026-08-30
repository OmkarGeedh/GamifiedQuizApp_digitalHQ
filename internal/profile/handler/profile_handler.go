package handler

import (
	"fmt"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

func GetProfileHandler(c *gin.Context) {
	clientIDVal, exists := c.Get("client_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	clientID, ok := clientIDVal.(int)
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

func CreateProfileHandler(c *gin.Context) {
	clientIDVal, exists := c.Get("client_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	clientID, ok := clientIDVal.(int)
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

func UpdateProfileHandler(c *gin.Context) {
	clientIDVal, exists := c.Get("client_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	clientID, ok := clientIDVal.(int)
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
