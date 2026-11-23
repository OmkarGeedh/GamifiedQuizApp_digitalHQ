package handlers

import (
	"io"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
)

// GetBillingProductsHandler lists the products the app may sell.
// GET /api/v1/billing/products
func GetBillingProductsHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.ListBillingProducts(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Products fetched successfully", res)
}

// GetBillingAccountHandler returns the obfuscated account id for checkout.
// GET /api/v1/billing/account-id
func GetBillingAccountHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.GetBillingAccount(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Billing account fetched successfully", res)
}

// VerifyPurchaseHandler verifies a Google Play purchase and grants it once.
// POST /api/v1/billing/verify
func VerifyPurchaseHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req dto.VerifyPurchaseRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid input")
		return
	}
	res, status, err := services.VerifyPurchase(c.Request.Context(), clientID, &req)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	msg := "Purchase verified"
	if status == http.StatusAccepted {
		msg = "Purchase is pending payment"
	}
	response.Success(c, status, msg, res)
}

// GetSubscriptionStatusHandler reports whether the player has Premium.
// GET /api/v1/billing/subscription
func GetSubscriptionStatusHandler(c *gin.Context) {
	clientID, ok := middleware.GetAuthenticatedClientID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	res, status, err := services.GetSubscriptionStatus(c.Request.Context(), clientID)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Subscription status fetched successfully", res)
}

// BillingRTDNHandler receives Google Play Real-time Developer Notifications
// pushed by Pub/Sub. Authenticated by the ?token= secret plus the Google-signed
// OIDC token Pub/Sub sends; never by a player session.
// POST /api/v1/billing/rtdn
func BillingRTDNHandler(c *gin.Context) {
	if status, err := services.AuthenticateRTDN(c.Request.Context(), c.GetHeader("Authorization"), c.Query("token")); err != nil {
		response.Error(c, status, err.Error())
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Error(c, http.StatusRequestEntityTooLarge, "Request body too large")
		return
	}
	status, err := services.HandleRTDN(c.Request.Context(), body)
	if err != nil {
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, status, "Notification processed", nil)
}
