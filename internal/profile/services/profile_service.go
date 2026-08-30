package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	clientRepo "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/repository"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CalculateExperienceForLevel returns the total XP required to reach the next level
func CalculateExperienceForLevel(level int) int {
	if level <= 1 {
		return 100
	}
	return level * 150
}

func GetOrCreateProfile(ctx context.Context, clientID int) (*models.Profile, error) {
	profile, err := repository.GetProfileByClientID(ctx, clientID)
	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	todayDate, _ := time.Parse("2006-01-02", todayStr)

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Fetch client record to initialize name/username
			client, err := clientRepo.GetClientByID(ctx, clientID)
			if err != nil {
				return nil, fmt.Errorf("client not found: %w", err)
			}

			displayName := client.Username
			if displayName == "" {
				displayName = "Player"
			}

			newProfile := models.Profile{
				ClientID:       clientID,
				UUID:           uuid.New().String(),
				FullName:       displayName,
				Coins:          100, // Starter bonus
				Gems:           10,  // Starter bonus
				Experience:     0,
				Level:          1,
				CurrentStreak:  1,
				HighestStreak:  1,
				LastActiveDate: todayDate,
				WeeklyScore:    0,
			}

			if err := repository.CreateProfile(ctx, &newProfile); err != nil {
				return nil, fmt.Errorf("failed to create default profile: %w", err)
			}

			// Record today's activity
			_ = repository.RecordDailyActivity(ctx, clientID, todayStr)

			return &newProfile, nil
		}
		return nil, err
	}

	// Profile exists: check and update daily streak
	lastActiveStr := profile.LastActiveDate.Format("2006-01-02")
	if lastActiveStr != todayStr {
		yesterdayStr := now.AddDate(0, 0, -1).Format("2006-01-02")
		if lastActiveStr == yesterdayStr {
			// Consecutive day: increment streak
			profile.CurrentStreak++
			if profile.CurrentStreak > profile.HighestStreak {
				profile.HighestStreak = profile.CurrentStreak
			}
		} else {
			// Broken streak: reset to 1
			profile.CurrentStreak = 1
		}

		profile.LastActiveDate = todayDate
		_ = repository.UpdateProfile(ctx, profile)
		_ = repository.RecordDailyActivity(ctx, clientID, todayStr)
	}

	return profile, nil
}

func GetMyProfile(ctx context.Context, clientID int) (*dto.ProfileResponseDTO, int, error) {
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve profile: %w", err)
	}

	client, err := clientRepo.GetClientByID(ctx, clientID)
	if err != nil {
		return nil, http.StatusNotFound, errors.New("client not found")
	}

	last7Days, err := repository.GetLast7DaysStreak(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve streak history: %w", err)
	}

	weeklyRank, err := repository.CalculateWeeklyRank(ctx, profile.WeeklyScore)
	if err != nil {
		weeklyRank = 1
	}

	nextLevelExp := CalculateExperienceForLevel(profile.Level)

	stats := &dto.GamificationStatsDTO{
		Coins:               profile.Coins,
		Gems:                profile.Gems,
		Level:               profile.Level,
		Experience:          profile.Experience,
		NextLevelExperience: nextLevelExp,
		WeeklyScore:         profile.WeeklyScore,
		WeeklyRank:          weeklyRank,
	}

	streak := &dto.StreakStatsDTO{
		Current: profile.CurrentStreak,
		Highest: profile.HighestStreak,
		History: last7Days,
	}

	res := &dto.ProfileResponseDTO{
		UUID:            profile.UUID,
		Name:            profile.FullName,
		Email:           client.Email,
		Phone:           client.Phone,
		AvatarURL:       profile.AvatarURL,
		Streaks:         profile.CurrentStreak,
		HighestStreak:   profile.HighestStreak,
		Last7DaysStreak: last7Days,
		Gems:            profile.Gems,
		Coins:           profile.Coins,
		Level:           profile.Level,
		Experience:      profile.Experience,
		NextLevelExp:    nextLevelExp,
		WeeklyScore:     profile.WeeklyScore,
		WeeklyRank:      weeklyRank,
		Stats:           stats,
		Streak:          streak,
		CreatedAt:       profile.CreatedAt,
		UpdatedAt:       profile.UpdatedAt,
	}

	return res, http.StatusOK, nil
}

func CreateMyProfile(ctx context.Context, clientID int, req *dto.CreateProfileRequestDTO) (*dto.ProfileResponseDTO, int, error) {
	existing, err := repository.GetProfileByClientID(ctx, clientID)
	if err == nil && existing != nil {
		return nil, http.StatusConflict, errors.New("profile already exists for this client")
	}

	now := time.Now().UTC()
	todayDate, _ := time.Parse("2006-01-02", now.Format("2006-01-02"))

	newProfile := models.Profile{
		ClientID:       clientID,
		UUID:           uuid.New().String(),
		FullName:       req.FullName,
		AvatarURL:      req.AvatarURL,
		Coins:          100,
		Gems:           10,
		Experience:     0,
		Level:          1,
		CurrentStreak:  1,
		HighestStreak:  1,
		LastActiveDate: todayDate,
		WeeklyScore:    0,
	}

	if err := repository.CreateProfile(ctx, &newProfile); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to create profile: %w", err)
	}

	if req.Phone != nil {
		_ = repository.UpdateClientPhone(ctx, clientID, req.Phone)
	}

	_ = repository.RecordDailyActivity(ctx, clientID, now.Format("2006-01-02"))

	return GetMyProfile(ctx, clientID)
}

func UpdateMyProfile(ctx context.Context, clientID int, req *dto.UpdateProfileRequestDTO) (*dto.ProfileResponseDTO, int, error) {
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve profile: %w", err)
	}

	if req.Name != nil && *req.Name != "" {
		profile.FullName = *req.Name
	}
	if req.AvatarURL != nil {
		profile.AvatarURL = *req.AvatarURL
	}
	if req.Phone != nil {
		_ = repository.UpdateClientPhone(ctx, clientID, req.Phone)
	}

	if err := repository.UpdateProfile(ctx, profile); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to update profile: %w", err)
	}

	return GetMyProfile(ctx, clientID)
}
