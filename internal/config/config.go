package config

import (
	"encoding/base64"
	"encoding/json"
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

// BillingConfig holds the Google Play Billing settings. Every secret here must
// come from the hosting environment (Catalyst AppSail env vars), never from the
// tracked .env file. Billing stays switched off (503) until it is complete.
type BillingConfig struct {
	// PackageName is the Android application id the purchases belong to.
	PackageName string
	// ServiceAccountJSON is the decoded Google service-account key used to call
	// the Google Play Developer API (from GOOGLE_PLAY_SERVICE_ACCOUNT_JSON_B64).
	ServiceAccountJSON []byte
	// AccountSecret keys the HMAC that links a Play purchase to one player
	// (obfuscatedAccountId). Changing it orphans unverified purchases.
	AccountSecret string
	// AllowTestPurchases accepts license-tester purchases (not charged).
	AllowTestPurchases bool
	// RTDNToken is the shared secret required in the Pub/Sub push URL (?token=).
	RTDNToken string
	// RTDNAudience is the audience the Pub/Sub push OIDC token must carry.
	RTDNAudience string
	// RTDNServiceAccountEmail is the service account Pub/Sub signs pushes as.
	RTDNServiceAccountEmail string

	serviceAccountErr string
}

// Minimum length for the billing secrets, so a placeholder cannot slip through.
const minBillingSecretLen = 32

// VerifyProblems lists what is missing or malformed for purchase verification.
// An empty list means verification can be switched on. Secrets are never echoed.
func (b BillingConfig) VerifyProblems() []string {
	var p []string
	if strings.TrimSpace(b.PackageName) == "" {
		p = append(p, "GOOGLE_PLAY_PACKAGE_NAME is empty")
	}
	if b.serviceAccountErr != "" {
		p = append(p, "GOOGLE_PLAY_SERVICE_ACCOUNT_JSON_B64: "+b.serviceAccountErr)
	} else if len(b.ServiceAccountJSON) == 0 {
		p = append(p, "GOOGLE_PLAY_SERVICE_ACCOUNT_JSON_B64 is not set")
	}
	if len(b.AccountSecret) < minBillingSecretLen {
		p = append(p, fmt.Sprintf("BILLING_ACCOUNT_SECRET must be at least %d characters", minBillingSecretLen))
	}
	return p
}

// RTDNProblems lists what is missing for accepting Real-time Developer
// Notifications. Verification problems also block notifications.
func (b BillingConfig) RTDNProblems() []string {
	p := b.VerifyProblems()
	if len(b.RTDNToken) < minBillingSecretLen {
		p = append(p, fmt.Sprintf("GOOGLE_PLAY_RTDN_TOKEN must be at least %d characters", minBillingSecretLen))
	}
	if strings.TrimSpace(b.RTDNAudience) == "" {
		p = append(p, "GOOGLE_PLAY_RTDN_AUDIENCE is empty")
	}
	if strings.TrimSpace(b.RTDNServiceAccountEmail) == "" {
		p = append(p, "GOOGLE_PLAY_RTDN_PUSH_SA_EMAIL is empty")
	}
	return p
}

// decodeServiceAccount decodes and sanity-checks the base64 service-account key.
func decodeServiceAccount(b64 string) ([]byte, string) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, ""
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "not valid base64"
	}
	var key struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.Unmarshal(raw, &key); err != nil {
		return nil, "not valid JSON"
	}
	if key.Type != "service_account" || key.ClientEmail == "" || key.PrivateKey == "" {
		return nil, "not a service-account key"
	}
	return raw, ""
}

// AdsConfig holds the rewarded-ads (AdMob) settings. Rewards are paid only for
// callbacks signed by Google (server-side verification); nothing the app says
// about an ad is trusted.
type AdsConfig struct {
	// Enabled is the kill switch (ADS_ENABLED, default true). Off: no new ad
	// rewards are offered (Quiz Pass instant rewards still work).
	Enabled bool
	// UserIDSecret keys the HMAC sent to AdMob as the SSV user id
	// (ADS_USER_ID_SECRET, falling back to BILLING_ACCOUNT_SECRET).
	UserIDSecret string
	// Rewarded ad unit ids per platform. Outside production they default to
	// Google's public test units. The SSV callback must name one of them.
	RewardedAdUnitAndroid string
	RewardedAdUnitIOS     string
	// SSVKeysURL is where Google publishes the SSV verification keys.
	SSVKeysURL string
	// DevAutoFulfill lets the app complete an ad reward without a Google
	// callback. Only for local testing with test ads; ignored in production.
	DevAutoFulfill bool
	// MaxAdContentRating caps ad content (G, PG, T, MA). Default G.
	MaxAdContentRating string
	// AgeTreatment is the AdMob age treatment for players whose age is not
	// verified (ADS_AGE_TREATMENT: teen = under 18, default; child = under 13,
	// use it if the Play listing targets children under 13).
	AgeTreatment string
}

// Google's public test rewarded ad units (safe to show while developing).
const (
	TestRewardedAdUnitAndroid = "ca-app-pub-3940256099942544/5224354917"
	TestRewardedAdUnitIOS     = "ca-app-pub-3940256099942544/1712485313"
)

// Problems lists what blocks rewarded ads. Empty means ads can be offered.
func (a AdsConfig) Problems(isProd bool) []string {
	var p []string
	if !a.Enabled {
		p = append(p, "ADS_ENABLED is false")
	}
	if len(a.UserIDSecret) < minBillingSecretLen {
		p = append(p, fmt.Sprintf("ADS_USER_ID_SECRET (or BILLING_ACCOUNT_SECRET) must be at least %d characters", minBillingSecretLen))
	}
	if a.RewardedAdUnitAndroid == "" && a.RewardedAdUnitIOS == "" {
		p = append(p, "ADMOB_REWARDED_AD_UNIT_ANDROID / ADMOB_REWARDED_AD_UNIT_IOS are not set")
	}
	if isProd && (a.RewardedAdUnitAndroid == TestRewardedAdUnitAndroid || a.RewardedAdUnitIOS == TestRewardedAdUnitIOS) {
		p = append(p, "test ad units are not allowed in production")
	}
	return p
}

// Publicly known fallback secrets from this (public) repository. Production must
// never run with them: anyone could forge login tokens.
const (
	defaultJWTSecret      = "super_secret_jwt_key_quizapp_2026"
	defaultPasswordSecret = "super_secret_pepper_salt"
)

// ProductionSecretProblems lists secrets that are missing or still the public
// defaults. main refuses to start in production while the JWT secret is unsafe.
func (c *Config) ProductionSecretProblems() (fatal []string, warnings []string) {
	if c == nil {
		return nil, nil
	}
	if s := c.Server.JWTSecret; s == "" || s == defaultJWTSecret || len(s) < 32 {
		fatal = append(fatal, "JWT_SECRET must be set to a private value of at least 32 characters")
	}
	if c.Server.PasswordSecret == defaultPasswordSecret {
		// Not fatal: changing the pepper invalidates every stored password. Plan a
		// rehash on next login before rotating it.
		warnings = append(warnings, "PASSWORD_SECRET is the public default pepper")
	}
	return fatal, warnings
}

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Billing  BillingConfig
	Ads      AdsConfig
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

	serviceAccountJSON, serviceAccountErr := decodeServiceAccount(os.Getenv("GOOGLE_PLAY_SERVICE_ACCOUNT_JSON_B64"))

	defaultSSLMode := "disable"
	if isProd {
		defaultSSLMode = "require"
	}

	AppConfig = &Config{
		Server: ServerConfig{
			Port:                  getEnv("PORT", "8080"),
			Env:                   activeEnv,
			PasswordSecret:        getEnv("PASSWORD_SECRET", defaultPasswordSecret),
			JWTSecret:             getEnv("JWT_SECRET", defaultJWTSecret),
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
		Billing: BillingConfig{
			PackageName:             getEnv("GOOGLE_PLAY_PACKAGE_NAME", "com.skillverse.skillverse_app"),
			ServiceAccountJSON:      serviceAccountJSON,
			AccountSecret:           os.Getenv("BILLING_ACCOUNT_SECRET"),
			AllowTestPurchases:      getEnv("GOOGLE_PLAY_ALLOW_TEST_PURCHASES", strconv.FormatBool(!isProd)) == "true",
			RTDNToken:               os.Getenv("GOOGLE_PLAY_RTDN_TOKEN"),
			RTDNAudience:            os.Getenv("GOOGLE_PLAY_RTDN_AUDIENCE"),
			RTDNServiceAccountEmail: os.Getenv("GOOGLE_PLAY_RTDN_PUSH_SA_EMAIL"),
			serviceAccountErr:       serviceAccountErr,
		},
		Ads: AdsConfig{
			Enabled:               getEnv("ADS_ENABLED", "true") == "true",
			UserIDSecret:          getEnv("ADS_USER_ID_SECRET", os.Getenv("BILLING_ACCOUNT_SECRET")),
			RewardedAdUnitAndroid: getEnv("ADMOB_REWARDED_AD_UNIT_ANDROID", testAdUnitUnlessProd(isProd, TestRewardedAdUnitAndroid)),
			RewardedAdUnitIOS:     getEnv("ADMOB_REWARDED_AD_UNIT_IOS", testAdUnitUnlessProd(isProd, TestRewardedAdUnitIOS)),
			SSVKeysURL:            getEnv("ADMOB_SSV_KEYS_URL", "https://www.gstatic.com/admob/reward/verifier-keys.json"),
			DevAutoFulfill:        !isProd && os.Getenv("ADS_DEV_AUTO_FULFILL") == "true",
			MaxAdContentRating:    strings.ToUpper(getEnv("ADS_MAX_CONTENT_RATING", "G")),
			AgeTreatment:          strings.ToLower(getEnv("ADS_AGE_TREATMENT", "teen")),
		},
	}

	return nil
}

func testAdUnitUnlessProd(isProd bool, unit string) string {
	if isProd {
		return ""
	}
	return unit
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
