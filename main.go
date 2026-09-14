package main

import (
	"log"
	"net/http"
	"os"

	authRoutes "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/routes"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	gameRoutes "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/routes"
	profileRoutes "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/routes"
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

	// CORS Middleware
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		}
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// Health and Root endpoints
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "gamified-quiz-app",
		})
	})

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Gamified Quiz App API is running with Gin! 🚀")
	})

	// 5. Register Routes
	authRoutes.RegisterClientRoutes(r)
	profileRoutes.RegisterProfileRoutes(r)
	gameRoutes.RegisterGameRoutes(r)

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
