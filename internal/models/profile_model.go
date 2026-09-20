package models

import (
	"time"
)

type Profile struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ClientID       int       `gorm:"column:client_id;not null;uniqueIndex" json:"client_id"`
	UUID           string    `gorm:"column:uuid;size:64;not null;uniqueIndex" json:"uuid"`
	FullName       string    `gorm:"column:full_name;size:100;not null" json:"full_name"`
	AvatarID       string    `gorm:"column:avatar_id;size:50;not null;default:'user'" json:"avatar_id"`
	AvatarURL      string    `gorm:"column:avatar_url;size:255" json:"avatar_url,omitempty"`
	Class          string    `gorm:"column:class;size:50" json:"class,omitempty"`
	Board          string    `gorm:"column:board;size:100" json:"board,omitempty"`
	Subjects       string    `gorm:"column:subjects;type:text;default:'[]'" json:"subjects,omitempty"`
	IsOnboarded    bool      `gorm:"column:is_onboarded;not null;default:false;index:idx_client_profiles_onboarded" json:"is_onboarded"`
	Coins          int       `gorm:"column:coins;not null;default:100" json:"coins"`
	Gems           int       `gorm:"column:gems;not null;default:10" json:"gems"`
	Experience     int       `gorm:"column:experience;not null;default:0" json:"experience"`
	Level          int       `gorm:"column:level;not null;default:1" json:"level"`
	CurrentStreak  int       `gorm:"column:current_streak;not null;default:1" json:"current_streak"`
	HighestStreak  int       `gorm:"column:highest_streak;not null;default:1" json:"highest_streak"`
	LastActiveDate time.Time `gorm:"column:last_active_date;type:date" json:"last_active_date"`
	WeeklyScore    int       `gorm:"column:weekly_score;not null;default:0;index:idx_weekly_score" json:"weekly_score"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (Profile) TableName() string {
	return "client_profiles"
}

type StreakActivity struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ClientID     int       `gorm:"column:client_id;not null;index:idx_client_date,unique" json:"client_id"`
	ActivityDate string    `gorm:"column:activity_date;size:10;not null;index:idx_client_date,unique" json:"activity_date"` // YYYY-MM-DD
	Completed    bool      `gorm:"column:completed;not null;default:true" json:"completed"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (StreakActivity) TableName() string {
	return "client_streak_activities"
}
