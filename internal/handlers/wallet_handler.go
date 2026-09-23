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

// DebitWalletHandler handles in-game and power-up deductions.
// POST /api/v1/wallet/debit
func DebitWalletHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.WalletDebitRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}

	res, status, err := services.DebitWallet(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Debit successful", res)
}

// CreditWalletHandler handles wallet top-ups or rewards.
// POST /api/v1/wallet/credit
func CreditWalletHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req dto.WalletCreditRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, fmt.Sprintf("Invalid input: %s", err.Error()))
		return
	}

	res, status, err := services.CreditWallet(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Credit successful", res)
}

// GetWalletBalanceHandler returns current coins and gems for the player.
// GET /api/v1/wallet/balance
func GetWalletBalanceHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	res, status, err := services.GetWalletBalance(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Wallet balance fetched successfully", res)
}

// GetWalletTransactionsHandler returns paginated transaction history.
// GET /api/v1/wallet/transactions
func GetWalletTransactionsHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	res, status, err := services.GetWalletTransactions(c.Request.Context(), clientID, limit, offset)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}

	response.Success(c, status, "Transactions fetched successfully", res)
}
