package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/middleware"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func setupLeaderboardTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Ensure config JWT secret is set for tests
	if config.AppConfig == nil {
		config.AppConfig = &config.Config{
			Server: config.ServerConfig{
				JWTSecret: "test_secret_key_1234567890_abcdef",
			},
		}
	} else if config.AppConfig.Server.JWTSecret == "" {
		config.AppConfig.Server.JWTSecret = "test_secret_key_1234567890_abcdef"
	}

	registerRoutes := func(grp *gin.RouterGroup) {
		grp.GET("/leaderboard", GetLeaderboardHandler)
	}

	root := r.Group("", middleware.ClientAuthMiddleware)
	registerRoutes(root)

	v1 := r.Group("/api/v1", middleware.ClientAuthMiddleware)
	registerRoutes(v1)

	return r
}

func generateTestToken(clientID int) string {
	secret := []byte(config.AppConfig.Server.JWTSecret)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":       clientID,
		"client_id": clientID,
		"exp":       time.Now().Add(1 * time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString(secret)
	return tokenString
}

func TestLeaderboardHandler_UnauthenticatedReturns401(t *testing.T) {
	router := setupLeaderboardTestRouter()

	for _, path := range []string{"/leaderboard", "/api/v1/leaderboard"} {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for path %s without token, got %d", path, w.Code)
		}
	}
}

func TestLeaderboardHandler_FilterRejections(t *testing.T) {
	router := setupLeaderboardTestRouter()
	token := generateTestToken(1)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantError  string
	}{
		{
			name:       "Unsupported scope friends",
			query:      "?scope=friends",
			wantStatus: http.StatusBadRequest,
			wantError:  "unsupported",
		},
		{
			name:       "Unsupported scope school",
			query:      "?scope=school",
			wantStatus: http.StatusBadRequest,
			wantError:  "unsupported",
		},
		{
			name:       "Invalid scope",
			query:      "?scope=random_scope",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid scope",
		},
		{
			name:       "Invalid time_range",
			query:      "?time_range=yearly",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid time_range",
		},
		{
			name:       "Invalid game_mode",
			query:      "?game_mode=battle_royale",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid game_mode",
		},
		{
			name:       "Invalid page non-integer",
			query:      "?page=abc",
			wantStatus: http.StatusBadRequest,
			wantError:  "page must be a valid integer",
		},
		{
			name:       "Invalid page zero or negative",
			query:      "?page=0",
			wantStatus: http.StatusBadRequest,
			wantError:  "page must be greater than or equal to 1",
		},
		{
			name:       "Invalid page_size non-integer",
			query:      "?page_size=abc",
			wantStatus: http.StatusBadRequest,
			wantError:  "page_size must be a valid integer",
		},
		{
			name:       "Invalid page_size zero",
			query:      "?page_size=0",
			wantStatus: http.StatusBadRequest,
			wantError:  "page_size must be between 1 and 100",
		},
		{
			name:       "Invalid page_size exceeding max 100",
			query:      "?page_size=150",
			wantStatus: http.StatusBadRequest,
			wantError:  "page_size must be between 1 and 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/leaderboard"+tt.query, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			if !strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(tt.wantError)) {
				t.Errorf("expected body to contain '%s', got: %s", tt.wantError, w.Body.String())
			}
		})
	}
}

func TestLeaderboardHandler_SuccessResponseContract(t *testing.T) {
	router := setupLeaderboardTestRouter()
	repo.ResetInMemoryLeaderboard()
	now := time.Now().UTC()

	// Seed client 1 (requesting user) and client 2
	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "player1", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 2, Username: "player2", Status: "active"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 1, UUID: "uuid-p1", FullName: "Player One", AvatarID: "robot"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 2, UUID: "uuid-p2", FullName: "Player Two", AvatarID: "user"})

	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-1",
		ClientID:    1,
		TopicID:     "accounting",
		GameMode:    models.GameModeMCQ,
		FinalScore:  250,
		CompletedAt: now,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-2",
		ClientID:    2,
		TopicID:     "accounting",
		GameMode:    models.GameModeMCQ,
		FinalScore:  150,
		CompletedAt: now,
	})

	token := generateTestToken(1)

	for _, path := range []string{"/leaderboard", "/api/v1/leaderboard"} {
		req, _ := http.NewRequest(http.MethodGet, path+"?scope=global&time_range=weekly&page=1&page_size=50", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200 for path %s, got %d. Body: %s", path, w.Code, w.Body.String())
		}

		type apiResponse struct {
			Success   bool                       `json:"success"`
			Message   string                     `json:"message"`
			Data      dto.LeaderboardResponseDTO `json:"data"`
			Errors    interface{}                `json:"errors"`
			Timestamp string                     `json:"timestamp"`
		}

		var resp apiResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response JSON: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success: true")
		}
		if resp.Data.TotalCount != 2 {
			t.Errorf("expected total_count: 2, got %d", resp.Data.TotalCount)
		}
		if len(resp.Data.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(resp.Data.Items))
		}
		if resp.Data.CurrentUserRank == nil || *resp.Data.CurrentUserRank != 1 {
			t.Errorf("expected current_user_rank: 1, got %v", resp.Data.CurrentUserRank)
		}

		// First item must be Player One (Rank 1, Score 250, IsCurrentUser: true)
		item1 := resp.Data.Items[0]
		if item1.Rank != 1 || item1.UserID != "uuid-p1" || item1.DisplayName != "Player One" || item1.Score != 250 || !item1.IsCurrentUser || item1.AvatarID != "robot" {
			t.Errorf("unexpected item 1: %+v", item1)
		}

		// Second item must be Player Two (Rank 2, Score 150, IsCurrentUser: false)
		item2 := resp.Data.Items[1]
		if item2.Rank != 2 || item2.UserID != "uuid-p2" || item2.DisplayName != "Player Two" || item2.Score != 150 || item2.IsCurrentUser {
			t.Errorf("unexpected item 2: %+v", item2)
		}
	}
}
