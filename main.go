package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("Starting Gamified Quiz App Server...")

	// 1. Load environment configuration
	if err := config.LoadEnv(); err != nil {
		log.Printf("Warning loading .env: %v\n", err)
	}

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

	// Run AutoMigrate for all models
	if err := db.AutoMigrate(pgDB); err != nil {
		log.Printf("Warning: Database AutoMigrate failed: %v\n", err)
	}

	// Auto-seed initial demo user and questions if database is empty
	var questionCount int64
	pgDB.Table("questions").Count(&questionCount)
	if questionCount == 0 {
		log.Println("==> Fresh database detected. Running automatic seeding...")
		if err := db.Seed(pgDB); err != nil {
			log.Printf("Warning: Auto-seed failed: %v\n", err)
		}
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
	if os.Getenv("ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

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

	// Fallback 404 handler with JSON response
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": fmt.Sprintf("Path '%s' not found", c.Request.URL.Path),
		})
	})

	// 6. Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("HTTP server listening on port :%s\n", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v\n", err)
	}
}
