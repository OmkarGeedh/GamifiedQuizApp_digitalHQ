package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ServerConfig struct {
	Port                  string
	Env                   string
	PasswordSecret        string
	JWTSecret             string
	JWTAccessTokenExpiry  time.Duration
	JWTRefreshTokenExpiry time.Duration
	CookieDomain          string
}

type Config struct {
	Server ServerConfig
}

var (
	AppConfig   *Config
	DB          *gorm.DB
	RedisClient *redis.Client
)

func LoadEnv() error {
	if err := godotenv.Load(".env"); err != nil {
		log.Println("Note: .env file not found or environment already loaded")
	}

	accessExpiryDays, _ := strconv.Atoi(getEnv("JWT_ACCESS_TOKEN_EXPIRY_DAYS", "30"))
	refreshExpiryDays, _ := strconv.Atoi(getEnv("JWT_REFRESH_TOKEN_EXPIRY_DAYS", "180"))

	AppConfig = &Config{
		Server: ServerConfig{
			Port:                  getEnv("PORT", "8080"),
			Env:                   getEnv("ENV", "development"),
			PasswordSecret:        getEnv("PASSWORD_SECRET", "super_secret_pepper_salt"),
			JWTSecret:             getEnv("JWT_SECRET", "super_secret_jwt_key_quizapp_2026"),
			JWTAccessTokenExpiry:  time.Duration(accessExpiryDays) * 24 * time.Hour,
			JWTRefreshTokenExpiry: time.Duration(refreshExpiryDays) * 24 * time.Hour,
			CookieDomain:          getEnv("COOKIE_DOMAIN", ""),
		},
	}

	return nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
