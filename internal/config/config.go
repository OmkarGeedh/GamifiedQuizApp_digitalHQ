package config

import (
	"fmt"
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

type DatabaseConfig struct {
	URL      string
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

func (d DatabaseConfig) ConnectionString() string {
	if d.URL != "" {
		return d.URL
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", d.User, d.Password, d.Host, d.Port, d.Name)
}

type RedisConfig struct {
	Addr     string
	Host     string
	Port     string
	Password string
	DB       int
}

func (r RedisConfig) Endpoint() string {
	if r.Addr != "" {
		return r.Addr
	}
	return fmt.Sprintf("%s:%s", r.Host, r.Port)
}

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
}

var (
	AppConfig   *Config
	DB          *gorm.DB
	RedisClient *redis.Client
)

func LoadEnv() error {
	// Attempt to find .env in current or parent directories (useful for go test in subdirectories)
	envPaths := []string{".env", "../.env", "../../.env", "../../../.env", "../../../../.env"}
	for _, p := range envPaths {
		_ = godotenv.Load(p)
	}

	accessExpiryDays, _ := strconv.Atoi(getEnv("JWT_ACCESS_TOKEN_EXPIRY_DAYS", "30"))
	refreshExpiryDays, _ := strconv.Atoi(getEnv("JWT_REFRESH_TOKEN_EXPIRY_DAYS", "180"))
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

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
		Database: DatabaseConfig{
			URL:      os.Getenv("DATABASE_URL"),
			Host:     getEnv("POSTGRES_HOST", "127.0.0.1"),
			Port:     getEnv("POSTGRES_PORT", "5432"),
			User:     getEnv("POSTGRES_USER", "skillverse"),
			Password: getEnv("POSTGRES_PASSWORD", "skillverse_secrets"),
			Name:     getEnv("POSTGRES_DB", "gamifiedapp"),
		},
		Redis: RedisConfig{
			Addr:     os.Getenv("REDIS_ADDR"),
			Host:     getEnv("REDIS_HOST", "127.0.0.1"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       redisDB,
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
