package repo

import (
	"context"
	"errors"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
)

// ErrStreakMilestoneNotReached means the player's streak is shorter than the milestone.
var ErrStreakMilestoneNotReached = errors.New("streak milestone not reached yet")

// ClaimedStreakMilestones returns which of the given reference ids already have a
// streak reward in the player's ledger.
func ClaimedStreakMilestones(ctx context.Context, clientID int, refs []string) (map[string]bool, error) {
	return ClaimedRewards(ctx, clientID, models.TxTypeStreakBonus, refs)
}

// ClaimStreakMilestone pays a streak milestone's reward once per reference id.
// The player's streak is read again inside the claim lock, so a stale value from
// the caller cannot pay a milestone that has not been reached.
func ClaimStreakMilestone(ctx context.Context, clientID int, refID, currency string, amount, minStreak int) (*RewardPayout, error) {
	return claimReward(ctx, clientID, models.TxTypeStreakBonus, refID, currency, amount,
		func(tx *gorm.DB) error {
			var streak []int
			if err := tx.Raw(`SELECT current_streak FROM clients WHERE id = ?`, clientID).Scan(&streak).Error; err != nil {
				return err
			}
			if len(streak) != 1 {
				return ErrProfileNotFound
			}
			if streak[0] < minStreak {
				return ErrStreakMilestoneNotReached
			}
			return nil
		})
}
