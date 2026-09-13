package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type ClientClaims struct {
	ID       int     `gorm:"column:id" db:"id" json:"id"`
	Username string  `gorm:"column:username" db:"username" json:"username"`
	Email    string  `gorm:"column:email" db:"email" json:"email"`
	Status   string  `gorm:"column:status" db:"status" json:"status"`
	Phone    *string `gorm:"column:phone" db:"phone" json:"phone,omitempty"`
}

// GetAuthenticatedClientID retrieves the authenticated client's ID from gin.Context
func GetAuthenticatedClientID(c *gin.Context) (int, bool) {
	val, exists := c.Get("client_id")
	if !exists {
		return 0, false
	}
	id, ok := val.(int)
	return id, ok
}

// ClearClientAuthCookies clears client JWT cookies
func ClearClientAuthCookies(c *gin.Context) {
	utils.ClearClientAuthCookies(c)
}

// ClientAuthMiddleware verifies JWT from cookie or Authorization header for clients
func ClientAuthMiddleware(c *gin.Context) {
	// Try the client cookie first, then support bearer or raw Authorization tokens.
	tokenStr, _ := c.Cookie("clientAccessToken")
	if tokenStr == "" {
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			tokenStr = strings.TrimSpace(authHeader[7:])
		} else {
			tokenStr = authHeader
		}
	}

	if tokenStr == "" {
		// No token provided
		ClearClientAuthCookies(c)
		response.Error(c, http.StatusUnauthorized, "Unauthorized request")
		return
	}

	// 2. Verify token
	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.JWTSecret
	}
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		ClearClientAuthCookies(c)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, fmt.Sprintf("Invalid or expired access token: %v", err))
			return
		}
		response.Error(c, http.StatusUnauthorized, "Invalid or expired access token")
		return
	}

	// 3. Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		ClearClientAuthCookies(c)
		response.Error(c, http.StatusUnauthorized, "Invalid token payload")
		return
	}

	// Check if token is blacklisted in Redis
	var jtiVal string
	var expVal float64
	if j, ok := claims["jti"].(string); ok && j != "" {
		jtiVal = j
		if config.RedisClient != nil {
			isBlacklisted, err := config.RedisClient.Exists(c.Request.Context(), "blacklist:"+jtiVal).Result()
			if err == nil && isBlacklisted > 0 {
				ClearClientAuthCookies(c)
				response.Error(c, http.StatusUnauthorized, "Token is revoked (logged out)")
				return
			}
		}
	}
	if e, ok := claims["exp"].(float64); ok {
		expVal = e
	}

	clientIDVal, ok := claims["client_id"]
	if !ok || clientIDVal == nil {
		ClearClientAuthCookies(c)
		response.Error(c, http.StatusUnauthorized, "Invalid token payload (client_id missing)")
		return
	}
	clientIDFloat, ok := clientIDVal.(float64)
	if !ok {
		ClearClientAuthCookies(c)
		response.Error(c, http.StatusUnauthorized, "Invalid token payload (client_id invalid)")
		return
	}
	clientID := int(clientIDFloat)

	// 4. Verify client exists and is not deleted
	var client ClientClaims
	if config.DB != nil {
		err = config.DB.WithContext(c.Request.Context()).
			Table("clients").
			Select("id, username, email, status, phone").
			Where("id = ? AND status != ?", clientID, "deleted").
			Limit(1).
			Take(&client).Error
		if err != nil {
			ClearClientAuthCookies(c)
			response.Error(c, http.StatusUnauthorized, "Invalid or inactive client")
			return
		}
	}

	// 5. Attach client to context
	c.Set("client_id", client.ID)
	c.Set("client", client)
	c.Set("jti", jtiVal)
	c.Set("exp", expVal)

	// 6. Continue
	c.Next()
}
