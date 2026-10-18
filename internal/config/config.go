package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	AllowedOrigins        string
	SeedOnBoot            bool
	SeedDemoUser          bool
}

type DatabaseConfig struct {
	URL      string
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

func (d DatabaseConfig) ConnectionString() string {
	if d.URL != "" {
		return d.URL
	}
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", d.User, d.Password, d.Host, d.Port, d.Name, sslMode)
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

// IsProduction returns true when running in production environment
func (c *Config) IsProduction() bool {
	if c == nil {
		env := os.Getenv("ENV")
		if env == "" {
			env = os.Getenv("APP_ENV")
		}
		return env == "production" || env == "prod"
	}
	return c.Server.Env == "production" || c.Server.Env == "prod"
}

// IsDevelopment returns true when running in development environment
func (c *Config) IsDevelopment() bool {
	if c == nil {
		env := os.Getenv("ENV")
		if env == "" {
			env = os.Getenv("APP_ENV")
		}
		return env == "" || env == "development" || env == "dev" || env == "local"
	}
	return c.Server.Env == "development" || c.Server.Env == "dev" || c.Server.Env == "local"
}

// IsTest returns true when running in test environment
func (c *Config) IsTest() bool {
	if c == nil {
		env := os.Getenv("ENV")
		if env == "" {
			env = os.Getenv("APP_ENV")
		}
		return env == "test"
	}
	return c.Server.Env == "test"
}

var (
	AppConfig   *Config
	DB          *gorm.DB
	RedisClient *redis.Client
)

// LoadEnv loads environment variables based on the active environment (development, production, test)
// Hierarchy: OS Environment Variables > .env.<env>.local > .env.<env> > .env.local > .env
func LoadEnv() error {
	// 1. Detect target environment (default: development)
	currentEnv := os.Getenv("ENV")
	if currentEnv == "" {
		currentEnv = os.Getenv("APP_ENV")
	}
	if currentEnv == "" {
		currentEnv = "development"
	}
	currentEnv = strings.ToLower(strings.TrimSpace(currentEnv))

	// 2. Discover and load environment files in priority order
	envFiles := []string{
		fmt.Sprintf(".env.%s.local", currentEnv),
		fmt.Sprintf(".env.%s", currentEnv),
		".env.local",
		".env",
	}

	searchDirs := []string{".", "..", "../..", "../../..", "../../../.."}
	for _, dir := range searchDirs {
		for _, file := range envFiles {
			p := filepath.Join(dir, file)
			if stat, err := os.Stat(p); err == nil && !stat.IsDir() {
				// godotenv.Load never overwrites already-set environment variables
				_ = godotenv.Load(p)
			}
		}
	}

	// 3. Re-read environment after dotenv resolution
	activeEnv := getEnv("ENV", currentEnv)
	isProd := activeEnv == "production" || activeEnv == "prod"

	accessExpiryDays, _ := strconv.Atoi(getEnv("JWT_ACCESS_TOKEN_EXPIRY_DAYS", "30"))
	refreshExpiryDays, _ := strconv.Atoi(getEnv("JWT_REFRESH_TOKEN_EXPIRY_DAYS", "180"))
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	seedOnBoot := getEnv("SEED_ON_BOOT", strconv.FormatBool(!isProd)) == "true"
	seedDemoUser := getEnv("SEED_DEMO_USER", strconv.FormatBool(!isProd)) == "true"

	defaultSSLMode := "disable"
	if isProd {
		defaultSSLMode = "require"
	}

	AppConfig = &Config{
		Server: ServerConfig{
			Port:                  getEnv("PORT", "8080"),
			Env:                   activeEnv,
			PasswordSecret:        getEnv("PASSWORD_SECRET", "super_secret_pepper_salt"),
			JWTSecret:             getEnv("JWT_SECRET", "super_secret_jwt_key_quizapp_2026"),
			JWTAccessTokenExpiry:  time.Duration(accessExpiryDays) * 24 * time.Hour,
			JWTRefreshTokenExpiry: time.Duration(refreshExpiryDays) * 24 * time.Hour,
			CookieDomain:          getEnv("COOKIE_DOMAIN", ""),
			AllowedOrigins:        getEnv("CORS_ALLOWED_ORIGINS", ""),
			SeedOnBoot:            seedOnBoot,
			SeedDemoUser:          seedDemoUser,
		},
		Database: DatabaseConfig{
			URL:      os.Getenv("DATABASE_URL"),
			Host:     getEnv("POSTGRES_HOST", "127.0.0.1"),
			Port:     getEnv("POSTGRES_PORT", "5432"),
			User:     getEnv("POSTGRES_USER", "skillverse"),
			Password: getEnv("POSTGRES_PASSWORD", "skillverse_secrets"),
			Name:     getEnv("POSTGRES_DB", "gamifiedapp"),
			SSLMode:  getEnv("POSTGRES_SSLMODE", defaultSSLMode),
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
