package routes

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterWalletRoutes registers all wallet debit, credit, balance, and ledger history endpoints.
func RegisterWalletRoutes(router *gin.Engine) {
	registerRoutes := func(grp *gin.RouterGroup) {
		grp.POST("/debit", handlers.DebitWalletHandler)
		grp.POST("/credit", handlers.CreditWalletHandler)
		grp.GET("/balance", handlers.GetWalletBalanceHandler)
		grp.GET("/transactions", handlers.GetWalletTransactionsHandler)
	}

	// 1. Standard REST v1 path: /api/v1/wallet/...
	v1Wallet := router.Group("/api/v1/wallet", middleware.ClientAuthMiddleware)
	registerRoutes(v1Wallet)

	// 2. Client-root alias path: /wallet/...
	rootWallet := router.Group("/wallet", middleware.ClientAuthMiddleware)
	registerRoutes(rootWallet)
}
