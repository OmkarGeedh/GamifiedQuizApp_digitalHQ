package dto

import (
	"errors"
	"strings"
	"time"
)

type DayStreakDTO struct {
	Date      string `json:"date"`      // e.g. "2026-08-30"
	Day       string `json:"day"`       // e.g. "Sun"
	Completed bool   `json:"completed"` // true if active
}

type GamificationStatsDTO struct {
	Coins               int `json:"coins"`
	Gems                int `json:"gems"`
	Level               int `json:"level"`
	Experience          int `json:"experience"`
	NextLevelExperience int `json:"nextLevelExperience"`
	WeeklyScore         int `json:"weeklyScore"`
	WeeklyRank          int `json:"weeklyRank"`
}

type StreakStatsDTO struct {
	Current int            `json:"current"`
	Highest int            `json:"highest"`
	History []DayStreakDTO `json:"history"`
}

type ProfileResponseDTO struct {
	UUID            string                `json:"uuid"`
	Name            string                `json:"name"`
	Phone           *string               `json:"phone,omitempty"`
	Email           string                `json:"email"`
	AvatarURL       string                `json:"avatarUrl,omitempty"`
	Streaks         int                   `json:"streaks"`
	HighestStreak   int                   `json:"highestStreak"`
	Last7DaysStreak []DayStreakDTO        `json:"last7DaysStreak"`
	Gems            int                   `json:"gems"`
	Coins           int                   `json:"coins"`
	Level           int                   `json:"level"`
	Experience      int                   `json:"experience"`
	NextLevelExp    int                   `json:"nextLevelExp"`
	WeeklyScore     int                   `json:"weeklyScore"`
	WeeklyRank      int                   `json:"weeklyRank"`
	Stats           *GamificationStatsDTO `json:"stats,omitempty"`
	Streak          *StreakStatsDTO       `json:"streak,omitempty"`
	CreatedAt       time.Time             `json:"createdAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
}

type CreateProfileRequestDTO struct {
	FullName  string  `json:"name"`
	Phone     *string `json:"phone,omitempty"`
	AvatarURL string  `json:"avatarUrl,omitempty"`
}

func (r *CreateProfileRequestDTO) Validate() error {
	r.FullName = strings.TrimSpace(r.FullName)
	if r.FullName == "" {
		return errors.New("name is required and cannot be empty")
	}
	if len(r.FullName) < 2 {
		return errors.New("name must be at least 2 characters long")
	}
	return nil
}

type UpdateProfileRequestDTO struct {
	Name      *string `json:"name,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	AvatarURL *string `json:"avatarUrl,omitempty"`
}

func (r *UpdateProfileRequestDTO) Validate() error {
	if r.Name != nil {
		trimmed := strings.TrimSpace(*r.Name)
		if trimmed == "" {
			return errors.New("name cannot be empty")
		}
		if len(trimmed) < 2 {
			return errors.New("name must be at least 2 characters long")
		}
		*r.Name = trimmed
	}
	if r.Phone != nil {
		trimmed := strings.TrimSpace(*r.Phone)
		*r.Phone = trimmed
	}
	return nil
}
