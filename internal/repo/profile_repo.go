package repo

import (
	"context"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm/clause"
)

func GetProfileByClientID(ctx context.Context, clientID int) (*models.Profile, error) {
	var profile models.Profile
	err := GetDB().WithContext(ctx).
		Where("client_id = ?", clientID).
		First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func CreateProfile(ctx context.Context, profile *models.Profile) error {
	return GetDB().WithContext(ctx).Create(profile).Error
}

func UpdateProfile(ctx context.Context, profile *models.Profile) error {
	return GetDB().WithContext(ctx).Save(profile).Error
}

func RecordDailyActivity(ctx context.Context, clientID int, dateStr string) error {
	activity := models.StreakActivity{
		ClientID:     clientID,
		ActivityDate: dateStr,
		Completed:    true,
	}
	// Upsert on conflict
	return GetDB().WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "client_id"}, {Name: "activity_date"}},
			DoUpdates: clause.AssignmentColumns([]string{"completed"}),
		}).
		Create(&activity).Error
}

func GetLast7DaysStreak(ctx context.Context, clientID int) ([]dto.DayStreakDTO, error) {
	now := time.Now().UTC()
	dates := make([]string, 7)
	streakMap := make(map[string]bool)

	// Prepare list of past 7 days (oldest to newest: T-6 to T-0)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dates[6-i] = d.Format("2006-01-02")
	}

	var activities []models.StreakActivity
	err := GetDB().WithContext(ctx).
		Where("client_id = ? AND activity_date IN ?", clientID, dates).
		Find(&activities).Error
	if err != nil {
		return nil, err
	}

	for _, a := range activities {
		streakMap[a.ActivityDate] = a.Completed
	}

	result := make([]dto.DayStreakDTO, 7)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		dayName := d.Format("Mon")
		completed := streakMap[dateStr]

		result[6-i] = dto.DayStreakDTO{
			Date:      dateStr,
			Day:       dayName,
			Completed: completed,
		}
	}

	return result, nil
}

func CalculateWeeklyRank(ctx context.Context, weeklyScore int) (int, error) {
	var count int64
	err := GetDB().WithContext(ctx).
		Model(&models.Profile{}).
		Where("weekly_score > ?", weeklyScore).
		Count(&count).Error
	if err != nil {
		return 1, err
	}
	return int(count) + 1, nil
}
