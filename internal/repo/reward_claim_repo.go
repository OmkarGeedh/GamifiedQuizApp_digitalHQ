package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
)

// ErrRewardAlreadyClaimed means this reward (this reference id) was already paid.
var ErrRewardAlreadyClaimed = errors.New("reward already claimed")

// RewardPayout is a paid reward and the balances after it.
type RewardPayout struct {
	Coins int
	Gems  int
}

// ClaimedRewards returns which of the given reference ids already have a ledger
// row of this transaction type for the player.
func ClaimedRewards(ctx context.Context, clientID int, txType string, refs []string) (map[string]bool, error) {
	claimed := make(map[string]bool, len(refs))
	if len(refs) == 0 {
		return claimed, nil
	}
	var found []string
	err := GetDB().WithContext(ctx).Raw(
		`SELECT reference_id FROM wallet_ledger
		  WHERE client_id = ? AND transaction_type = ? AND reference_id IN ?`,
		clientID, txType, refs).Scan(&found).Error
	if err != nil {
		return nil, err
	}
	for _, r := range found {
		claimed[r] = true
	}
	return claimed, nil
}

// claimReward pays a reward once per (player, txType, refID).
//
// Everything runs in one transaction. On PostgreSQL the player's claims are
// serialized with an advisory lock, so two quick taps cannot both pass the
// "already claimed" check. eligible runs inside the lock and returns the error
// that refuses the claim (or nil to allow it).
func claimReward(ctx context.Context, clientID int, txType, refID, currency string, amount int,
	eligible func(tx *gorm.DB) error) (*RewardPayout, error) {

	if currency != models.CurrencyCoins && currency != models.CurrencyGems {
		return nil, fmt.Errorf("unsupported currency: %s", currency)
	}
	var out RewardPayout

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			// Released automatically when the transaction ends.
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('reward_claim'), ?)`, clientID).Error; err != nil {
				return fmt.Errorf("failed to lock claim: %w", err)
			}
		}

		var existing int64
		if err := tx.Raw(
			`SELECT COUNT(*) FROM wallet_ledger
			  WHERE client_id = ? AND transaction_type = ? AND reference_id = ?`,
			clientID, txType, refID).Scan(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrRewardAlreadyClaimed
		}

		if err := eligible(tx); err != nil {
			return err
		}

		// currency is one of the two constants checked above, never user input.
		query := fmt.Sprintf(
			`UPDATE clients SET %[1]s = %[1]s + ?, updated_at = ? WHERE id = ? RETURNING coins, gems`, currency)
		var bal []RewardPayout
		if err := tx.Raw(query, amount, time.Now().UTC(), clientID).Scan(&bal).Error; err != nil {
			return fmt.Errorf("failed to pay reward: %w", err)
		}
		if len(bal) != 1 {
			return ErrProfileNotFound
		}
		out = bal[0]

		after := out.Coins
		if currency == models.CurrencyGems {
			after = out.Gems
		}
		return tx.Exec(
			`INSERT INTO wallet_ledger (client_id, transaction_type, direction, amount, currency, balance_after, reference_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			clientID, txType, models.DirectionCredit, amount, currency, after, refID).Error
	})
	if err != nil {
		return nil, err
	}

	_ = cache.InvalidateCachedProfile(ctx, clientID)
	return &out, nil
}
