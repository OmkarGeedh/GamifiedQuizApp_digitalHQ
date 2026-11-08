package dto

import (
	"errors"
	"time"
)

// StreakMilestoneDTO is one streak reward and where the player stands on it.
type StreakMilestoneDTO struct {
	Days     int    `json:"days"`
	Currency string `json:"currency"`
	Reward   int    `json:"reward"`
	// Reached: the current streak is at least Days. Claimed: already paid for this
	// streak. Claimable: reached and not yet claimed.
	Reached   bool `json:"reached"`
	Claimed   bool `json:"claimed"`
	Claimable bool `json:"claimable"`
}

// StreakStatusDTO is GET /streak.
type StreakStatusDTO struct {
	Current int `json:"current"`
	Highest int `json:"highest"`
	// DayEndsAt is the next UTC midnight: the streak day the player is on ends then.
	DayEndsAt  time.Time            `json:"day_ends_at"`
	Milestones []StreakMilestoneDTO `json:"milestones"`
	// NextMilestoneDays is the next milestone above the current streak, or 0 when
	// every milestone has been reached. DaysToNextMilestone counts the days left.
	NextMilestoneDays   int `json:"next_milestone_days"`
	DaysToNextMilestone int `json:"days_to_next_milestone"`

	// Streak freezes: one is used up automatically for each missed day. When
	// FreezesEnabled is false (the database has no freeze column yet) the other
	// freeze fields are zero and the app should not offer freezes.
	FreezesEnabled  bool `json:"freezes_enabled"`
	Freezes         int  `json:"freezes"`
	MaxFreezes      int  `json:"max_freezes"`
	FreezePriceGems int  `json:"freeze_price_gems"`
}

// StreakFreezeBuyResponseDTO is the result of POST /streak/freeze.
type StreakFreezeBuyResponseDTO struct {
	Freezes int `json:"freezes"`
	Gems    int `json:"gems"`
}

// StreakClaimRequestDTO is the body of POST /streak/claim.
type StreakClaimRequestDTO struct {
	Days int `json:"days" binding:"required"`
}

// Validate requires a positive day count.
func (r *StreakClaimRequestDTO) Validate() error {
	if r.Days <= 0 {
		return errors.New("days must be a positive number")
	}
	return nil
}

// StreakClaimResponseDTO reports the reward paid and the balances after it.
type StreakClaimResponseDTO struct {
	Days     int    `json:"days"`
	Currency string `json:"currency"`
	Reward   int    `json:"reward"`
	Coins    int    `json:"coins"`
	Gems     int    `json:"gems"`
}
