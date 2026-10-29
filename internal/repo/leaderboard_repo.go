package repo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// In-memory storage for test isolation and fallback when database connection is nil
var (
	memLeaderboardMu      sync.RWMutex
	memClients            = make(map[int]*models.Client)
	memProfiles           = make(map[int]*models.Profile)
	memLeaderboardResults = make([]models.LeaderboardGameResult, 0)
	memSessionIDs         = make(map[string]bool)
)

// ResetInMemoryLeaderboard clears all in-memory test data for leaderboard testing.
func ResetInMemoryLeaderboard() {
	memLeaderboardMu.Lock()
	defer memLeaderboardMu.Unlock()
	memClients = make(map[int]*models.Client)
	memProfiles = make(map[int]*models.Profile)
	memLeaderboardResults = make([]models.LeaderboardGameResult, 0)
	memSessionIDs = make(map[string]bool)
}

// SeedInMemoryClient adds or updates a client in the in-memory test store.
func SeedInMemoryClient(client *models.Client) {
	memLeaderboardMu.Lock()
	defer memLeaderboardMu.Unlock()
	memClients[client.ID] = client
}

// SeedInMemoryProfile adds or updates a profile in the in-memory test store.
func SeedInMemoryProfile(profile *models.Profile) {
	memLeaderboardMu.Lock()
	defer memLeaderboardMu.Unlock()
	memProfiles[profile.ClientID] = profile
}

// RecordInMemoryLeaderboardResult stores a game result idempotently in memory.
func RecordInMemoryLeaderboardResult(res *models.LeaderboardGameResult) error {
	if res == nil || res.SessionID == "" {
		return nil
	}
	memLeaderboardMu.Lock()
	defer memLeaderboardMu.Unlock()

	if memSessionIDs[res.SessionID] {
		// Idempotency: skip duplicate session
		return nil
	}
	memSessionIDs[res.SessionID] = true
	memLeaderboardResults = append(memLeaderboardResults, *res)
	return nil
}

// RecordLeaderboardResult persists a completed game session into the database or memory.
func RecordLeaderboardResult(ctx context.Context, result *models.LeaderboardGameResult) error {
	if result == nil || result.SessionID == "" {
		return nil
	}
	db := GetDB()
	if db == nil {
		return RecordInMemoryLeaderboardResult(result)
	}

	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoNothing: true,
	}).Create(result).Error
}

// dbLeaderboardRow represents the raw SQL query scan target.
type dbLeaderboardRow struct {
	Rank           int        `gorm:"column:rank"`
	ClientID       int        `gorm:"column:client_id"`
	UserID         string     `gorm:"column:user_id"`
	DisplayName    string     `gorm:"column:display_name"`
	AvatarID       string     `gorm:"column:avatar_id"`
	AvatarURL      *string    `gorm:"column:avatar_url"`
	Score          int        `gorm:"column:score"`
	LatestActivity *time.Time `gorm:"column:latest_activity"`
}

// GetLeaderboard retrieves a ranked, filtered, paginated leaderboard.
func GetLeaderboard(ctx context.Context, filter *dto.LeaderboardFilterDTO, currentClientID int) (*dto.LeaderboardResponseDTO, error) {
	db := GetDB()
	if db == nil {
		return getLeaderboardInMemory(filter, currentClientID)
	}
	return getLeaderboardFromDB(ctx, db, filter, currentClientID)
}

// getLeaderboardFromDB executes an authoritative, indexed CTE query in PostgreSQL.
func getLeaderboardFromDB(ctx context.Context, db *gorm.DB, filter *dto.LeaderboardFilterDTO, currentClientID int) (*dto.LeaderboardResponseDTO, error) {
	// 1. Build parameterized filter conditions
	var whereConditions []string
	var args []interface{}

	now := time.Now().UTC()
	startTime, endTime := filter.CalculateTimeBounds(now)
	if startTime != nil {
		whereConditions = append(whereConditions, "completed_at >= ?")
		args = append(args, *startTime)
	}
	if endTime != nil {
		whereConditions = append(whereConditions, "completed_at < ?")
		args = append(args, *endTime)
	}
	if filter.GameMode != "" {
		whereConditions = append(whereConditions, "game_mode = ?")
		args = append(args, filter.GameMode)
	}
	if filter.SubjectID != "" {
		whereConditions = append(whereConditions, "topic_id = ?")
		args = append(args, filter.SubjectID)
	}

	subqueryWhere := ""
	if len(whereConditions) > 0 {
		subqueryWhere = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// 2. Count total eligible active users
	var totalCount int64
	err := db.WithContext(ctx).
		Table("clients").
		Where("status = ?", "active").
		Count(&totalCount).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count eligible users: %w", err)
	}

	offset := (filter.Page - 1) * filter.PageSize

	// 3. CTE query with deterministic tie-breaking:
	// Order by score DESC, latest_activity DESC NULLS LAST, display_name ASC, client_id ASC
	cteQuery := `
WITH user_scores AS (
    SELECT 
        client_id,
        SUM(final_score) AS score,
        MAX(completed_at) AS latest_activity
    FROM leaderboard_game_results
    ` + subqueryWhere + `
    GROUP BY client_id
),
ranked_leaderboard AS (
    SELECT 
        c.id AS client_id,
        c.uuid::text AS user_id,
        COALESCE(NULLIF(TRIM(c.full_name), ''), NULLIF(TRIM(c.username), ''), 'Player') AS display_name,
        COALESCE(NULLIF(TRIM(c.avatar_id), ''), 'user') AS avatar_id,
        c.avatar_url,
        COALESCE(s.score, 0)::int AS score,
        s.latest_activity,
        ROW_NUMBER() OVER (
            ORDER BY 
                COALESCE(s.score, 0) DESC,
                s.latest_activity DESC NULLS LAST,
                COALESCE(NULLIF(TRIM(c.full_name), ''), NULLIF(TRIM(c.username), ''), 'Player') ASC,
                c.id ASC
        ) AS rank
    FROM clients c
    LEFT JOIN user_scores s ON s.client_id = c.id
    WHERE c.status = 'active'
)
`

	// 4. Fetch page rows
	pageQuery := cteQuery + `
SELECT 
    rank,
    client_id,
    user_id,
    display_name,
    avatar_id,
    avatar_url,
    score,
    latest_activity
FROM ranked_leaderboard
ORDER BY rank ASC
LIMIT ? OFFSET ?
`
	pageArgs := append([]interface{}{}, args...)
	pageArgs = append(pageArgs, filter.PageSize, offset)

	var rows []dbLeaderboardRow
	if err := db.Session(&gorm.Session{PrepareStmt: false}).WithContext(ctx).Raw(pageQuery, pageArgs...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch leaderboard page: %w", err)
	}

	items := make([]dto.LeaderboardItemDTO, len(rows))
	var currentUserRank *int

	for i, r := range rows {
		isCurrent := currentClientID > 0 && r.ClientID == currentClientID
		if isCurrent {
			currentRank := r.Rank
			currentUserRank = &currentRank
		}
		items[i] = dto.LeaderboardItemDTO{
			Rank:          r.Rank,
			UserID:        r.UserID,
			DisplayName:   r.DisplayName,
			AvatarID:      r.AvatarID,
			AvatarURL:     r.AvatarURL,
			Score:         r.Score,
			IsCurrentUser: isCurrent,
		}
	}

	// 5. If current user is not on this page and has a valid ID, calculate their rank across the full dataset
	if currentUserRank == nil && currentClientID > 0 {
		rankQuery := cteQuery + `
SELECT rank FROM ranked_leaderboard WHERE client_id = ?
`
		rankArgs := append([]interface{}{}, args...)
		rankArgs = append(rankArgs, currentClientID)

		var userRank int
		err := db.Session(&gorm.Session{PrepareStmt: false}).WithContext(ctx).Raw(rankQuery, rankArgs...).Scan(&userRank).Error
		if err == nil && userRank > 0 {
			currentUserRank = &userRank
		}
	}

	hasMore := int64(filter.Page*filter.PageSize) < totalCount

	return &dto.LeaderboardResponseDTO{
		Items:           items,
		Page:            filter.Page,
		PageSize:        filter.PageSize,
		TotalCount:      totalCount,
		HasMore:         hasMore,
		CurrentUserRank: currentUserRank,
	}, nil
}

// getLeaderboardInMemory implements the exact same ranking, tie-breaking, filtering,
// and pagination rules in memory for hermetic testing and DB-less environments.
func getLeaderboardInMemory(filter *dto.LeaderboardFilterDTO, currentClientID int) (*dto.LeaderboardResponseDTO, error) {
	memLeaderboardMu.RLock()
	defer memLeaderboardMu.RUnlock()

	now := time.Now().UTC()
	startTime, endTime := filter.CalculateTimeBounds(now)

	// 1. Gather all active eligible clients
	type candidate struct {
		clientID       int
		userID         string
		displayName    string
		avatarID       string
		avatarURL      *string
		score          int
		latestActivity *time.Time
	}

	var candidates []candidate
	for clientID, client := range memClients {
		// Only active clients are eligible
		if client.Status != "active" {
			continue
		}

		profile := memProfiles[clientID]
		userID := fmt.Sprintf("usr_%d", clientID)
		displayName := client.Username
		avatarID := "user"
		var avatarURL *string

		if profile != nil {
			if profile.UUID != "" {
				userID = profile.UUID
			}
			if strings.TrimSpace(profile.FullName) != "" {
				displayName = strings.TrimSpace(profile.FullName)
			}
			if strings.TrimSpace(profile.AvatarID) != "" {
				avatarID = strings.TrimSpace(profile.AvatarID)
			}
			if profile.AvatarURL != "" {
				url := profile.AvatarURL
				avatarURL = &url
			}
		}
		if strings.TrimSpace(displayName) == "" {
			displayName = "Player"
		}

		// Calculate user's score from matching results
		score := 0
		var latest *time.Time
		for _, res := range memLeaderboardResults {
			if res.ClientID != clientID {
				continue
			}
			// Apply time boundaries
			if startTime != nil && res.CompletedAt.Before(*startTime) {
				continue
			}
			if endTime != nil && !res.CompletedAt.Before(*endTime) {
				continue
			}
			// Apply game mode filter
			if filter.GameMode != "" && res.GameMode != filter.GameMode {
				continue
			}
			// Apply subject filter
			if filter.SubjectID != "" && res.TopicID != filter.SubjectID {
				continue
			}

			score += res.FinalScore
			if latest == nil || res.CompletedAt.After(*latest) {
				t := res.CompletedAt
				latest = &t
			}
		}

		candidates = append(candidates, candidate{
			clientID:       clientID,
			userID:         userID,
			displayName:    displayName,
			avatarID:       avatarID,
			avatarURL:      avatarURL,
			score:          score,
			latestActivity: latest,
		})
	}

	totalCount := int64(len(candidates))

	// 2. Deterministic sort:
	// a. score DESC
	// b. latestActivity DESC (non-nil before nil)
	// c. displayName ASC
	// d. clientID ASC
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		// Tie-breaker: latest qualifying activity
		if candidates[i].latestActivity != nil && candidates[j].latestActivity != nil {
			if !candidates[i].latestActivity.Equal(*candidates[j].latestActivity) {
				return candidates[i].latestActivity.After(*candidates[j].latestActivity)
			}
		} else if candidates[i].latestActivity != nil {
			return true
		} else if candidates[j].latestActivity != nil {
			return false
		}
		// Tie-breaker: display name alphabetical
		if candidates[i].displayName != candidates[j].displayName {
			return candidates[i].displayName < candidates[j].displayName
		}
		// Tie-breaker: stable client ID
		return candidates[i].clientID < candidates[j].clientID
	})

	// 3. Find current user rank across entire sorted candidates
	var currentUserRank *int
	if currentClientID > 0 {
		for i, c := range candidates {
			if c.clientID == currentClientID {
				r := i + 1
				currentUserRank = &r
				break
			}
		}
	}

	// 4. Slice for pagination
	offset := (filter.Page - 1) * filter.PageSize
	items := make([]dto.LeaderboardItemDTO, 0)
	if offset < len(candidates) {
		end := offset + filter.PageSize
		if end > len(candidates) {
			end = len(candidates)
		}
		paged := candidates[offset:end]
		for i, c := range paged {
			rank := offset + i + 1
			isCurrent := currentClientID > 0 && c.clientID == currentClientID
			items = append(items, dto.LeaderboardItemDTO{
				Rank:          rank,
				UserID:        c.userID,
				DisplayName:   c.displayName,
				AvatarID:      c.avatarID,
				AvatarURL:     c.avatarURL,
				Score:         c.score,
				IsCurrentUser: isCurrent,
			})
		}
	}

	hasMore := int64(filter.Page*filter.PageSize) < totalCount

	return &dto.LeaderboardResponseDTO{
		Items:           items,
		Page:            filter.Page,
		PageSize:        filter.PageSize,
		TotalCount:      totalCount,
		HasMore:         hasMore,
		CurrentUserRank: currentUserRank,
	}, nil
}
