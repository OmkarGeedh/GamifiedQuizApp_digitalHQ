package routes

import (
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/gin-gonic/gin"
)

// Credits and debits per player per minute. Far above what the app sends (a
// few at most), so honest players never reach it; it stops scripted spamming.
const walletWritesPerMinute = 60

// RegisterWalletRoutes registers all wallet debit, credit, balance, and ledger history endpoints.
func RegisterWalletRoutes(router *gin.Engine) {
	// One limiter for every copy of the routes, so /wallet and /api/v1/wallet
	// share a player's count.
	walletWriteLimit := middleware.RateLimitPerClient("wallet", walletWritesPerMinute, time.Minute)

	registerRoutes := func(grp *gin.RouterGroup) {
		grp.POST("/debit", walletWriteLimit, handlers.DebitWalletHandler)
		grp.POST("/credit", walletWriteLimit, handlers.CreditWalletHandler)
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
