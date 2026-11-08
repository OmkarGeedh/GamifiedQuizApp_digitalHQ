package dto

import (
	"errors"
	"strings"
	"time"
)

// DailyGoalStatusDTO is one goal with the player's progress today.
type DailyGoalStatusDTO struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Target   int    `json:"target"`
	Progress int    `json:"progress"` // capped at Target
	Currency string `json:"currency"`
	Reward   int    `json:"reward"`
	// Completed: the target is reached. Claimed: the reward was already paid today.
	// Claimable: completed and not yet claimed.
	Completed bool `json:"completed"`
	Claimed   bool `json:"claimed"`
	Claimable bool `json:"claimable"`
}

// DailyGoalsResponseDTO is GET /daily-goals.
type DailyGoalsResponseDTO struct {
	Date     string               `json:"date"`      // UTC day, YYYY-MM-DD
	ResetsAt time.Time            `json:"resets_at"` // next UTC midnight
	Goals    []DailyGoalStatusDTO `json:"goals"`
}

// DailyGoalClaimRequestDTO is the body of POST /daily-goals/claim.
type DailyGoalClaimRequestDTO struct {
	GoalID string `json:"goal_id" binding:"required"`
}

// Validate trims and lowercases the goal id.
func (r *DailyGoalClaimRequestDTO) Validate() error {
	r.GoalID = strings.ToLower(strings.TrimSpace(r.GoalID))
	if r.GoalID == "" {
		return errors.New("goal_id is required")
	}
	return nil
}

// DailyGoalClaimResponseDTO reports the reward paid and the balances after it.
type DailyGoalClaimResponseDTO struct {
	GoalID   string `json:"goal_id"`
	Currency string `json:"currency"`
	Reward   int    `json:"reward"`
	Coins    int    `json:"coins"`
	Gems     int    `json:"gems"`
}
