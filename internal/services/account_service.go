package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// DeleteAccount permanently deletes the signed-in player and all their data
// (Google Play requires in-app account deletion). The password must be
// confirmed. The current token is revoked and cached checks are dropped, so
// the account is locked out at once. A Google Play subscription is not
// cancelled by this; the app tells the player to cancel it in Google Play.
func DeleteAccount(ctx context.Context, clientID int, jti string, exp int64, req *dto.DeleteAccountRequestDTO) (int, error) {
	if err := req.Validate(); err != nil {
		return http.StatusBadRequest, err
	}
	client, err := repo.GetClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, errors.New("account not found")
		}
		return http.StatusInternalServerError, fmt.Errorf("database error: %w", err)
	}
	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	if err := bcrypt.CompareHashAndPassword([]byte(client.Password), []byte(req.Password+secret)); err != nil {
		// 403, not 401: a mistyped password must not look like an expired session.
		return http.StatusForbidden, errors.New("incorrect password")
	}

	deleted, err := repo.DeleteClientAccount(ctx, clientID)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to delete account: %w", err)
	}
	if !deleted {
		return http.StatusNotFound, errors.New("account not found")
	}

	// Lock the account out now, not when caches expire.
	_ = cache.Delete(ctx, cache.AuthClientKey(clientID))
	_ = cache.InvalidateCachedProfile(ctx, clientID)
	_ = repo.ClearClientActiveSession(ctx, clientID)
	if jti != "" && exp > 0 && config.RedisClient != nil {
		if ttl := time.Until(time.Unix(exp, 0)); ttl > 0 {
			_ = config.RedisClient.Set(ctx, "blacklist:"+jti, "revoked", ttl).Err()
		}
	}
	log.Printf("[Account] client %d deleted their account", clientID)
	return http.StatusOK, nil
}
