package dto

import (
	"fmt"
	"strings"
	"time"
)

// Supported leaderboard scopes
const (
	ScopeGlobal  = "global"
	ScopeFriends = "friends"
	ScopeSchool  = "school"
)

// Supported leaderboard time ranges
const (
	TimeRangeDaily   = "daily"
	TimeRangeWeekly  = "weekly"
	TimeRangeMonthly = "monthly"
	TimeRangeAllTime = "all_time"
)

// Supported game mode filter constants matching frontend & backend models
const (
	GameModeFilterMCQ               = "mcq"
	GameModeFilterSuddenDeath       = "sudden_death"
	GameModeFilterSortItOut         = "sort_it_out"
	GameModeFilterMatchTheFollowing = "match_the_following"
)

// Pagination defaults and bounds
const (
	DefaultLeaderboardPage     = 1
	DefaultLeaderboardPageSize = 50
	MaxLeaderboardPageSize     = 100
)

// LeaderboardFilterDTO holds validated query parameters for leaderboard retrieval.
type LeaderboardFilterDTO struct {
	Scope     string `form:"scope"`
	TimeRange string `form:"time_range"`
	GameMode  string `form:"game_mode"`
	SubjectID string `form:"subject_id"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

// Validate sanitizes and validates all filter fields according to system constraints.
func (f *LeaderboardFilterDTO) Validate() error {
	// 1. Scope validation
	f.Scope = strings.ToLower(strings.TrimSpace(f.Scope))
	if f.Scope == "" {
		f.Scope = ScopeGlobal
	}
	switch f.Scope {
	case ScopeGlobal:
		// Fully supported
	case ScopeFriends:
		return fmt.Errorf("scope 'friends' is currently unsupported: friendship relationships are not available")
	case ScopeSchool:
		return fmt.Errorf("scope 'school' is currently unsupported: school affiliation data is not available")
	default:
		return fmt.Errorf("invalid scope '%s': must be one of 'global', 'friends', 'school'", f.Scope)
	}

	// 2. Time range validation
	f.TimeRange = strings.ToLower(strings.TrimSpace(f.TimeRange))
	if f.TimeRange == "" {
		f.TimeRange = TimeRangeWeekly
	}
	switch f.TimeRange {
	case TimeRangeDaily, TimeRangeWeekly, TimeRangeMonthly, TimeRangeAllTime:
		// Fully supported
	default:
		return fmt.Errorf("invalid time_range '%s': must be one of 'daily', 'weekly', 'monthly', 'all_time'", f.TimeRange)
	}

	// 3. Game mode validation (optional)
	f.GameMode = strings.ToLower(strings.TrimSpace(f.GameMode))
	if f.GameMode != "" {
		switch f.GameMode {
		case GameModeFilterMCQ, GameModeFilterSuddenDeath, GameModeFilterSortItOut, GameModeFilterMatchTheFollowing:
			// Fully supported
		default:
			return fmt.Errorf("invalid game_mode '%s': must be one of 'mcq', 'sudden_death', 'sort_it_out', 'match_the_following'", f.GameMode)
		}
	}

	// 4. Subject/Topic ID (optional)
	f.SubjectID = strings.TrimSpace(f.SubjectID)

	// 5. Pagination validation & bounding
	if f.Page <= 0 {
		f.Page = DefaultLeaderboardPage
	}
	if f.PageSize <= 0 {
		f.PageSize = DefaultLeaderboardPageSize
	} else if f.PageSize > MaxLeaderboardPageSize {
		f.PageSize = MaxLeaderboardPageSize
	}

	return nil
}

// CalculateTimeBounds derives consistent start-inclusive, end-exclusive UTC time boundaries.
func (f *LeaderboardFilterDTO) CalculateTimeBounds(refTime time.Time) (startTime *time.Time, endTime *time.Time) {
	utc := refTime.UTC()
	switch f.TimeRange {
	case TimeRangeDaily:
		// Daily = calendar today in UTC: [today 00:00:00 UTC, tomorrow 00:00:00 UTC)
		start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 0, 1)
		return &start, &end
	case TimeRangeWeekly:
		// Weekly = rolling last 7 days: completed_at >= now - 7 days
		start := utc.AddDate(0, 0, -7)
		return &start, nil
	case TimeRangeMonthly:
		// Monthly = rolling last 30 days: completed_at >= now - 30 days
		start := utc.AddDate(0, 0, -30)
		return &start, nil
	case TimeRangeAllTime:
		// All Time = no date restrictions
		return nil, nil
	default:
		return nil, nil
	}
}

// LeaderboardItemDTO represents a single ranked player entry on the leaderboard.
type LeaderboardItemDTO struct {
	Rank          int     `json:"rank"`
	UserID        string  `json:"user_id"`
	DisplayName   string  `json:"display_name"`
	AvatarID      string  `json:"avatar_id"`
	AvatarURL     *string `json:"avatar_url"`
	Score         int     `json:"score"`
	IsCurrentUser bool    `json:"is_current_user"`
}

// LeaderboardResponseDTO represents the paginated leaderboard response payload.
type LeaderboardResponseDTO struct {
	Items           []LeaderboardItemDTO `json:"items"`
	Page            int                  `json:"page"`
	PageSize        int                  `json:"page_size"`
	TotalCount      int64                `json:"total_count"`
	HasMore         bool                 `json:"has_more"`
	CurrentUserRank *int                 `json:"current_user_rank"`
}
