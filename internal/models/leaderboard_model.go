package models

import (
	"time"
)

// LeaderboardGameResult stores a durable, immutable record of every successfully
// completed game session for authoritative leaderboard ranking and filtering.
type LeaderboardGameResult struct {
	SessionID   string    `gorm:"column:session_id;type:uuid;primaryKey" json:"session_id"`
	ClientID    int       `gorm:"column:client_id;not null" json:"client_id"`
	TopicID     string    `gorm:"column:topic_id;size:32;not null" json:"topic_id"`
	GameMode    string    `gorm:"column:game_mode;size:32;not null" json:"game_mode"`
	FinalScore  int       `gorm:"column:final_score;not null;default:0" json:"final_score"`
	CompletedAt time.Time `gorm:"column:completed_at;not null" json:"completed_at"`
}

func (LeaderboardGameResult) TableName() string {
	return "leaderboard_game_results"
}
