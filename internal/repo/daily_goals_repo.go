package repo

import (
	"context"
	"errors"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
)

var (
	ErrDailyGoalNotMet = errors.New("daily goal is not completed yet")
	// Same error as every other reward, so callers can test for either name.
	ErrDailyGoalAlreadyClaimed = ErrRewardAlreadyClaimed
)

// DailyProgress is what a player has done in one day (counted from finished games).
type DailyProgress struct {
	Games int
	Score int
}

// GetDailyProgress counts the player's finished games and points in [start, end).
// It reads leaderboard_game_results, which holds one row per finished game.
func GetDailyProgress(ctx context.Context, clientID int, start, end time.Time) (DailyProgress, error) {
	return dailyProgress(GetDB().WithContext(ctx), clientID, start, end)
}

func dailyProgress(db *gorm.DB, clientID int, start, end time.Time) (DailyProgress, error) {
	var p DailyProgress
	err := db.Raw(
		`SELECT COUNT(*) AS games, COALESCE(SUM(final_score), 0) AS score
		   FROM leaderboard_game_results
		  WHERE client_id = ? AND completed_at >= ? AND completed_at < ?`,
		clientID, start, end).Scan(&p).Error
	return p, err
}

// ClaimedDailyGoals returns which of the given reference ids already have a
// daily goal reward in the player's ledger.
func ClaimedDailyGoals(ctx context.Context, clientID int, refs []string) (map[string]bool, error) {
	return ClaimedRewards(ctx, clientID, models.TxTypeDailyGoalReward, refs)
}

// DailyGoalPayout is a paid reward and the balances after it.
type DailyGoalPayout = RewardPayout

// ClaimDailyGoal pays a goal's reward once per player per reference id.
// isMet decides, from today's progress read inside the claim lock, whether the
// goal is completed.
func ClaimDailyGoal(ctx context.Context, clientID int, refID, currency string, amount int,
	start, end time.Time, isMet func(DailyProgress) bool) (*DailyGoalPayout, error) {

	return claimReward(ctx, clientID, models.TxTypeDailyGoalReward, refID, currency, amount,
		func(tx *gorm.DB) error {
			p, err := dailyProgress(tx, clientID, start, end)
			if err != nil {
				return err
			}
			if !isMet(p) {
				return ErrDailyGoalNotMet
			}
			return nil
		})
}
