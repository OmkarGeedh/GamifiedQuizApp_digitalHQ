package models

import (
	"time"
)

// LeaderboardGameResult stores a durable, immutable record of every successfully
// completed game session for authoritative leaderboard ranking and filtering.
type LeaderboardGameResult struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID   string    `gorm:"column:session_id;size:64;not null;uniqueIndex:idx_lgr_session" json:"session_id"`
	ClientID    int       `gorm:"column:client_id;not null;index:idx_lgr_client;index:idx_lgr_query,priority:1" json:"client_id"`
	TopicID     string    `gorm:"column:topic_id;size:64;not null;index:idx_lgr_topic;index:idx_lgr_query,priority:2" json:"topic_id"`
	GameMode    string    `gorm:"column:game_mode;size:32;not null;index:idx_lgr_mode;index:idx_lgr_query,priority:3" json:"game_mode"`
	FinalScore  int       `gorm:"column:final_score;not null;default:0" json:"final_score"`
	CompletedAt time.Time `gorm:"column:completed_at;not null;index:idx_lgr_completed;index:idx_lgr_query,priority:4" json:"completed_at"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (LeaderboardGameResult) TableName() string {
	return "leaderboard_game_results"
}
