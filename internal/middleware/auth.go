package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils/response"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// clientAuthCacheTTL bounds how long a cached active-client check is trusted.
const clientAuthCacheTTL = time.Minute

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
		} else if authHeader != "" {
			tokenStr = authHeader
		}
	}
	if tokenStr == "" {
		tokenStr = c.Query("token")
	}
	if tokenStr == "" {
		tokenStr = c.Query("access_token")
	}
	if tokenStr == "" {
		tokenStr = c.Query("accessToken")
	}
	if tokenStr == "" {
		tokenStr = c.Query("auth_token")
	}
	if tokenStr == "" {
		tokenStr = c.Query("authToken")
	}
	if tokenStr == "" {
		tokenStr = c.Query("jwt")
	}
	if tokenStr == "" {
		// Fallback for browser WebSocket clients passing token in Sec-WebSocket-Protocol
		if proto := c.GetHeader("Sec-WebSocket-Protocol"); proto != "" {
			parts := strings.Split(proto, ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if strings.EqualFold(trimmed, "bearer") ||
					strings.EqualFold(trimmed, "access_token") ||
					strings.EqualFold(trimmed, "token") ||
					strings.EqualFold(trimmed, "jwt") {
					continue
				}
				if len(trimmed) > 0 {
					tokenStr = trimmed
					break
				}
			}
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

	var (
		jtiVal string
		expVal float64
	)
	if j, ok := claims["jti"].(string); ok && j != "" {
		jtiVal = j
	}
	if e, ok := claims["exp"].(float64); ok {
		expVal = e
	}

	clientIDVal, ok := claims["client_id"]
	if !ok || clientIDVal == nil {
		clientIDVal, ok = claims["id"]
	}
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

	// Concurrently check token blacklist in Redis and client active status in PostgreSQL
	var (
		client        ClientClaims
		isBlacklisted bool
		dbErr         error
		wg            sync.WaitGroup
	)
	client.ID = clientID

	if jtiVal != "" && config.RedisClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, err := config.RedisClient.Exists(c.Request.Context(), "blacklist:"+jtiVal).Result()
			if err == nil && count > 0 {
				isBlacklisted = true
			}
		}()
	}

	// The active-client check is cached in Redis for clientAuthCacheTTL so gameplay
	// requests skip the PostgreSQL round trip; a blocked/deleted player is locked out
	// within that window. Without Redis every request checks PostgreSQL.
	if config.DB != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := c.Request.Context()
			cacheKey := fmt.Sprintf("quiz:auth:client:%d", clientID)
			if cache.GetJSON(ctx, cacheKey, &client) {
				return
			}
			dbErr = config.DB.WithContext(ctx).
				Table("clients").
				Select("id, username, email, status, phone").
				Where("id = ? AND status != ?", clientID, "deleted").
				Limit(1).
				Take(&client).Error
			if dbErr == nil {
				_ = cache.SetJSON(ctx, cacheKey, client, clientAuthCacheTTL)
			}
		}()
	}

	wg.Wait()

	if isBlacklisted {
		ClearClientAuthCookies(c)
		response.Error(c, http.StatusUnauthorized, "Token is revoked (logged out)")
		return
	}

	if dbErr != nil {
		ClearClientAuthCookies(c)
		response.Error(c, http.StatusUnauthorized, "Invalid or inactive client")
		return
	}

	// 5. Attach client to context
	c.Set("client_id", client.ID)
	c.Set("client", client)
	c.Set("jti", jtiVal)
	c.Set("exp", expVal)

	// 6. Continue
	c.Next()
}
