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

// ErrStreakFreezeLimit means the player already holds the most freezes allowed.
var ErrStreakFreezeLimit = errors.New("you already have the maximum number of streak freezes")

// The freeze balance is clients.streak_freezes (migration 000007 in the migration
// repo). It is deliberately not a field of models.Profile: UpdateProfile saves the
// whole Profile, so a Profile field would make every profile write depend on the
// column existing. Only the code in this file touches it, with plain SQL.

// GetStreakFreezes returns how many freezes the player holds.
func GetStreakFreezes(ctx context.Context, clientID int) (int, error) {
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	var n []int
	if err := db.WithContext(ctx).Raw(`SELECT streak_freezes FROM clients WHERE id = ?`, clientID).Scan(&n).Error; err != nil {
		return 0, err
	}
	if len(n) != 1 {
		return 0, ErrProfileNotFound
	}
	return n[0], nil
}

// UseStreakFreezes spends n freezes to bridge missed days and, in the same
// statement, moves last_active_date to today. It only applies if the player still
// has n freezes AND last_active_date is still the day the caller saw, so two
// requests arriving together on a new day cannot both spend freezes. It reports
// whether the freezes were spent.
func UseStreakFreezes(ctx context.Context, clientID, n int, lastActive, today time.Time) (bool, error) {
	db := GetDB()
	if db == nil || n <= 0 {
		return false, nil
	}
	res := db.WithContext(ctx).Exec(
		`UPDATE clients
		    SET streak_freezes = streak_freezes - ?, last_active_date = ?, updated_at = ?
		  WHERE id = ? AND streak_freezes >= ? AND substr(CAST(last_active_date AS TEXT), 1, 10) = ?`,
		n, today.Format("2006-01-02"), time.Now().UTC(), clientID, n, lastActive.Format("2006-01-02"))
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected > 0 {
		_ = cache.InvalidateCachedProfile(ctx, clientID)
	}
	return res.RowsAffected > 0, nil
}

// FreezePurchase is a bought freeze and the balances after it.
type FreezePurchase struct {
	Freezes int
	Gems    int
}

// BuyStreakFreeze takes price gems and adds one freeze, in one transaction, with
// a ledger entry for the gems. It refuses when the player cannot afford it
// (ErrInsufficientFunds) or already holds max freezes (ErrStreakFreezeLimit).
func BuyStreakFreeze(ctx context.Context, clientID, price, max int) (*FreezePurchase, error) {
	var out FreezePurchase

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// One statement decides and pays, so no lock is needed: the WHERE clause
		// is the check, and concurrent buys cannot both pass it.
		var rows []FreezePurchase
		err := tx.Raw(
			`UPDATE clients
			    SET gems = gems - ?, streak_freezes = streak_freezes + 1, updated_at = ?
			  WHERE id = ? AND gems >= ? AND streak_freezes < ?
			  RETURNING streak_freezes AS freezes, gems`,
			price, time.Now().UTC(), clientID, price, max).Scan(&rows).Error
		if err != nil {
			return fmt.Errorf("failed to buy streak freeze: %w", err)
		}
		if len(rows) == 1 {
			out = rows[0]
			return tx.Exec(
				`INSERT INTO wallet_ledger (client_id, transaction_type, direction, amount, currency, balance_after, reference_id)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				clientID, models.TxTypeStreakFreeze, models.DirectionDebit, price, models.CurrencyGems,
				out.Gems, fmt.Sprintf("sf:%d", time.Now().UnixNano())).Error
		}

		// Nothing was bought: work out why, so the player gets the right message.
		var now []struct{ Gems, Freezes int }
		if err := tx.Raw(`SELECT gems, streak_freezes AS freezes FROM clients WHERE id = ?`, clientID).Scan(&now).Error; err != nil {
			return err
		}
		switch {
		case len(now) != 1:
			return ErrProfileNotFound
		case now[0].Freezes >= max:
			return ErrStreakFreezeLimit
		default:
			return ErrInsufficientFunds
		}
	})
	if err != nil {
		return nil, err
	}

	_ = cache.InvalidateCachedProfile(ctx, clientID)
	return &out, nil
}
