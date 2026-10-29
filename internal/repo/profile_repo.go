package repo

import (
	"context"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func GetProfileByClientID(ctx context.Context, clientID int) (*models.Profile, error) {
	if cached, ok := cache.GetCachedProfile(ctx, clientID); ok && cached != nil {
		return cached, nil
	}
	db := GetDB()
	if db == nil {
		return &models.Profile{
			ClientID: clientID,
			FullName: "Player",
			Level:    1,
		}, nil
	}
	var profile models.Profile
	err := db.WithContext(ctx).
		Where("id = ?", clientID).
		First(&profile).Error
	if err != nil {
		return nil, err
	}
	_ = cache.SetCachedProfile(ctx, &profile)
	return &profile, nil
}

func UpdateProfile(ctx context.Context, profile *models.Profile) error {
	db := GetDB()
	if db == nil {
		return nil
	}
	if err := db.WithContext(ctx).Save(profile).Error; err != nil {
		return err
	}
	if profile != nil {
		_ = cache.InvalidateCachedProfile(ctx, profile.ClientID)
	}
	return nil
}

// activeDaysKept is how many days of play history clients.active_days keeps.
const activeDaysKept = 31

// RecordDailyActivity adds dateStr (YYYY-MM-DD) to the player's active_days, keeping
// only the last activeDaysKept days.
func RecordDailyActivity(ctx context.Context, clientID int, dateStr string) error {
	db := GetDB()
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).Exec(`
UPDATE clients SET active_days = ARRAY(
    SELECT DISTINCT d FROM unnest(array_append(active_days, ?::date)) AS d
    WHERE d > ?::date - ? ORDER BY d)
WHERE id = ? AND NOT (?::date = ANY (active_days))`,
		dateStr, dateStr, activeDaysKept, clientID, dateStr).Error
}

func GetLast7DaysStreak(ctx context.Context, clientID int) ([]dto.DayStreakDTO, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	var activeDays []string
	err := db.WithContext(ctx).Raw(
		`SELECT to_char(d, 'YYYY-MM-DD') FROM clients, unnest(active_days) AS d WHERE id = ? AND d > ?::date - 7`,
		clientID, now.Format("2006-01-02")).
		Scan(&activeDays).Error
	if err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(activeDays))
	for _, d := range activeDays {
		active[d] = true
	}

	// Past 7 days, oldest to newest (T-6 .. T-0).
	result := make([]dto.DayStreakDTO, 7)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		result[6-i] = dto.DayStreakDTO{
			Date:      dateStr,
			Day:       d.Format("Mon"),
			Completed: active[dateStr],
		}
	}
	return result, nil
}

// CalculateWeeklyRank returns the player's 1-based weekly leaderboard rank.
// If clientID is provided, it attempts to read from Redis Sorted Set in < 0.2ms.
// Falls back to PostgreSQL COUNT(*) on cache miss or when Redis is unavailable.
func CalculateWeeklyRank(ctx context.Context, weeklyScore int, clientID ...int) (int, error) {
	if len(clientID) > 0 && clientID[0] > 0 {
		if rank, err := cache.GetWeeklyRank(ctx, clientID[0]); err == nil && rank > 0 {
			return rank, nil
		}
	}

	db := GetDB()
	if db == nil {
		return 1, nil
	}
	var count int64
	err := db.WithContext(ctx).
		Model(&models.Profile{}).
		Where("weekly_score > ?", weeklyScore).
		Count(&count).Error
	if err != nil {
		return 1, err
	}
	return int(count) + 1, nil
}

// RebuildWeeklyLeaderboardFromDB queries the clients table for all players
// with weekly_score > 0 and repopulates the Redis WeeklyLeaderboardKey Sorted Set.
func RebuildWeeklyLeaderboardFromDB(ctx context.Context) (int, error) {
	db := GetDB()
	if db == nil {
		return 0, nil
	}

	type PlayerScore struct {
		ClientID    int `gorm:"column:client_id"`
		WeeklyScore int `gorm:"column:weekly_score"`
	}

	var scores []PlayerScore
	err := db.WithContext(ctx).
		Model(&models.Profile{}).
		Select("id AS client_id, weekly_score").
		Where("weekly_score > 0").
		Order("weekly_score DESC").
		Find(&scores).Error
	if err != nil {
		return 0, err
	}

	if len(scores) == 0 {
		return 0, nil
	}

	entries := make([]cache.LeaderboardEntry, len(scores))
	for i, s := range scores {
		entries[i] = cache.LeaderboardEntry{
			ClientID: s.ClientID,
			Score:    float64(s.WeeklyScore),
			Rank:     i + 1,
		}
	}

	if err := cache.RebuildWeeklyLeaderboard(ctx, entries); err != nil {
		return 0, err
	}
	return len(entries), nil
}
