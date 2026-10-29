package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("Starting Gamified Quiz App Server...")

	// 1. Load environment configuration
	if err := config.LoadEnv(); err != nil {
		log.Printf("Warning loading .env: %v\n", err)
	}
	ws.InitPlatformTiming()
	log.Printf("WebSocket timings initialized for platform '%s' (PingInterval: %v, PongWait: %v, TCPKeepAlive: %v)\n",
		ws.CurrentPlatform, ws.PingInterval, ws.PongWait, ws.TCPKeepAlive)

	// 2. Initialize PostgreSQL connection with GORM
	databaseURL := config.AppConfig.Database.ConnectionString()
	pgDB, err := db.Connect(databaseURL)
	if err != nil {
		log.Fatalf("Fatal: Database connection failed: %v\n", err)
	}
	config.DB = pgDB
	if sqlDB, err := pgDB.DB(); err == nil {
		defer sqlDB.Close()
	}

	// 3. Initialize Redis connection
	redisAddr := config.AppConfig.Redis.Endpoint()
	redisPassword := config.AppConfig.Redis.Password
	redisDB := config.AppConfig.Redis.DB

	redisClient, err := db.ConnectRedis(redisAddr, redisPassword, redisDB)
	if err != nil {
		log.Printf("Warning: Redis connection failed: %v (falling back to memory-only where applicable)\n", err)
	} else {
		config.RedisClient = redisClient
		defer redisClient.Close()
	}

	// 4. Setup Gin Engine
	if config.AppConfig != nil && config.AppConfig.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	} else if os.Getenv("ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.HandleMethodNotAllowed = true

	// Robust CORS Middleware
	r.Use(func(c *gin.Context) {
		// Clean trailing slashes (except root "/") to prevent 301/307 redirects from dropping CORS headers
		if len(c.Request.URL.Path) > 1 && strings.HasSuffix(c.Request.URL.Path, "/") {
			c.Request.URL.Path = strings.TrimRight(c.Request.URL.Path, "/")
		}

		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")

		// Echo requested headers if provided, otherwise provide a broad set of allowed headers
		reqHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
		if reqHeaders != "" {
			c.Writer.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With, Sec-Ch-Ua, Sec-Ch-Ua-Mobile, Sec-Ch-Ua-Platform, Sec-Fetch-Dest, Sec-Fetch-Mode, Sec-Fetch-Site, User-Agent")
		}

		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Authorization, X-Total-Count, Link")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		c.Writer.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	})

	// Health and Root endpoints (supporting both root and /api/v1 prefixes)
	healthHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "gamified-quiz-app",
		})
	}
	r.GET("/health", healthHandler)
	r.GET("/api/v1/health", healthHandler)

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Gamified Quiz App API is running with Gin! 🚀")
	})

	// 5. Register Routes
	routes.RegisterAuthRoutes(r)
	routes.RegisterProfileRoutes(r)
	routes.RegisterGameRoutes(r)
	routes.RegisterWalletRoutes(r)
	routes.RegisterLeaderboardRoutes(r)

	// 5b. Start background session inactivity sweeper (TTL: 5 minutes, sweeps every 1 minute)
	services.StartSessionTTLSweeper(context.Background(), 1*time.Minute)

	// Fallback 404 handler with JSON response
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": fmt.Sprintf("Path '%s' not found", c.Request.URL.Path),
		})
	})

	// Fallback 405 Method Not Allowed handler with JSON response
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"success": false,
			"message": fmt.Sprintf("Method %s not allowed on '%s'", c.Request.Method, c.Request.URL.Path),
		})
	})

	// 6. Start Server with Graceful Shutdown
	port := os.Getenv("X_ZOHO_CATALYST_LISTEN_PORT")
	if port == "" {
		port = os.Getenv("X_ZOHO_CATALYST_APPSAIL_PORT")
	}
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("HTTP server listening on port :%s\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v\n", err)
		}
	}()

	// Wait for OS interrupt or SIGTERM (sent by Docker / Catalyst when scaling or restarting)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received signal %v. Initiating graceful shutdown...\n", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown forced with error: %v\n", err)
	} else {
		log.Println("Server exited cleanly.")
	}
}
