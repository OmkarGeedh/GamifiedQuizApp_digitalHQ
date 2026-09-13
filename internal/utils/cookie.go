package utils

import (
	"net/http"
	"os"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/gin-gonic/gin"
)

// GetCookieConfig returns whether the environment is production/staging and the cookie domain
func GetCookieConfig() (isProduction bool, domain string) {
	if config.AppConfig != nil {
		env := config.AppConfig.Server.Env
		isProduction = env == "production" || env == "staging" || os.Getenv("ENV") == "production" || os.Getenv("APP_ENV") == "production"
		domain = config.AppConfig.Server.CookieDomain
	} else {
		env := os.Getenv("ENV")
		if env == "" {
			env = os.Getenv("APP_ENV")
		}
		isProduction = env == "production" || env == "staging"
		domain = os.Getenv("COOKIE_DOMAIN")
	}
	return isProduction, domain
}

// SetClientAuthCookies sets HTTP-only secure cookies for both access and refresh tokens
func SetClientAuthCookies(c *gin.Context, accessStr, refreshStr string) {
	isProduction, domain := GetCookieConfig()

	accessExpirySeconds := 30 * 24 * 60 * 60 // 30 days default
	if config.AppConfig != nil && config.AppConfig.Server.JWTAccessTokenExpiry > 0 {
		accessExpirySeconds = int(config.AppConfig.Server.JWTAccessTokenExpiry.Seconds())
	}
	refreshExpirySeconds := 180 * 24 * 60 * 60 // 180 days default
	if config.AppConfig != nil && config.AppConfig.Server.JWTRefreshTokenExpiry > 0 {
		refreshExpirySeconds = int(config.AppConfig.Server.JWTRefreshTokenExpiry.Seconds())
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("clientAccessToken", accessStr, accessExpirySeconds, "/", domain, isProduction, true)
	c.SetCookie("clientRefreshToken", refreshStr, refreshExpirySeconds, "/", domain, isProduction, true)
}

// SetClientAccessTokenCookie sets only the access token cookie (used during refresh)
func SetClientAccessTokenCookie(c *gin.Context, accessStr string) {
	isProduction, domain := GetCookieConfig()

	accessExpirySeconds := 30 * 24 * 60 * 60
	if config.AppConfig != nil && config.AppConfig.Server.JWTAccessTokenExpiry > 0 {
		accessExpirySeconds = int(config.AppConfig.Server.JWTAccessTokenExpiry.Seconds())
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("clientAccessToken", accessStr, accessExpirySeconds, "/", domain, isProduction, true)
}

// ClearClientAuthCookies invalidates client authentication cookies
func ClearClientAuthCookies(c *gin.Context) {
	isProduction, domain := GetCookieConfig()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("clientAccessToken", "", -1, "/", domain, isProduction, true)
	c.SetCookie("clientRefreshToken", "", -1, "/", domain, isProduction, true)
}
