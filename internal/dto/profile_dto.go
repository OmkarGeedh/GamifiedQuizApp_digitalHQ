package dto

import (
	"errors"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// DYNAMIC PROFILE SETUP OPTIONS (Zero Frontend Hardcoding)
// -----------------------------------------------------------------------------

type AvatarOptionDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type SubjectOptionDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Difficulty  string `json:"difficulty"`
	TopicsCount int    `json:"topicsCount"`
}

type ProfileSetupOptionsDTO struct {
	Avatars  []AvatarOptionDTO  `json:"avatars"`
	Classes  []string           `json:"classes"`
	Boards   []string           `json:"boards"`
	Subjects []SubjectOptionDTO `json:"subjects"`
}

// -----------------------------------------------------------------------------
// PROFILE SETUP / ONBOARDING REQUEST
// -----------------------------------------------------------------------------

type ProfileSetupRequestDTO struct {
	AvatarID  string   `json:"avatarId"`
	AvatarURL *string  `json:"avatarUrl,omitempty"`
	Name      string   `json:"name"`
	Class     string   `json:"class"`
	Board     string   `json:"board"`
	Subjects  []string `json:"subjects"`
}

func (r *ProfileSetupRequestDTO) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("name is required")
	}
	if len(r.Name) < 2 {
		return errors.New("name must be at least 2 characters long")
	}

	r.Class = strings.TrimSpace(r.Class)
	if r.Class == "" {
		return errors.New("class is required")
	}

	r.Board = strings.TrimSpace(r.Board)
	if r.Board == "" {
		return errors.New("board is required")
	}

	if len(r.Subjects) == 0 {
		return errors.New("at least one subject must be selected")
	}

	r.AvatarID = strings.TrimSpace(r.AvatarID)
	if r.AvatarID == "" {
		r.AvatarID = "user"
	}

	return nil
}

// -----------------------------------------------------------------------------
// STREAK & STATS DTOs
// -----------------------------------------------------------------------------

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

// -----------------------------------------------------------------------------
// PROFILE RESPONSE DTO
// -----------------------------------------------------------------------------

type ProfileResponseDTO struct {
	UUID            string                `json:"uuid"`
	Name            string                `json:"name"`
	Phone           *string               `json:"phone,omitempty"`
	Email           string                `json:"email"`
	AvatarID        string                `json:"avatarId"`
	AvatarURL       string                `json:"avatarUrl,omitempty"`
	Class           string                `json:"class,omitempty"`
	Board           string                `json:"board,omitempty"`
	Subjects        []string              `json:"subjects"`
	IsOnboarded     bool                  `json:"isOnboarded"`
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

// -----------------------------------------------------------------------------
// LEGACY / UPDATE DTOs
// -----------------------------------------------------------------------------

type CreateProfileRequestDTO struct {
	FullName  string   `json:"name"`
	Phone     *string  `json:"phone,omitempty"`
	AvatarID  *string  `json:"avatarId,omitempty"`
	AvatarURL *string  `json:"avatarUrl,omitempty"`
	Class     *string  `json:"class,omitempty"`
	Board     *string  `json:"board,omitempty"`
	Subjects  []string `json:"subjects,omitempty"`
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
	Name      *string  `json:"name,omitempty"`
	Phone     *string  `json:"phone,omitempty"`
	AvatarID  *string  `json:"avatarId,omitempty"`
	AvatarURL *string  `json:"avatarUrl,omitempty"`
	Class     *string  `json:"class,omitempty"`
	Board     *string  `json:"board,omitempty"`
	Subjects  []string `json:"subjects,omitempty"`
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
