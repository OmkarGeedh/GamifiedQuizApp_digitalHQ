package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrProfileNotFound   = errors.New("profile not found")
	// ErrDailyRewardLimit means today's total for this kind of reward is used up.
	ErrDailyRewardLimit = errors.New("daily reward limit reached")
)

// GetWalletBalance fetches only the coin and gem counts for a client.
func GetWalletBalance(ctx context.Context, clientID int) (coins int, gems int, err error) {
	var profile models.Profile
	err = GetDB().WithContext(ctx).
		Select("coins, gems").
		Where("id = ?", clientID).
		First(&profile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, 0, ErrProfileNotFound
		}
		return 0, 0, err
	}
	return profile.Coins, profile.Gems, nil
}

// DebitWalletBalance atomically verifies balance and deducts coins or gems.
// Uses PostgreSQL row-level locking (SELECT ... FOR UPDATE) to prevent race conditions.
func DebitWalletBalance(ctx context.Context, clientID int, currency string, amount int, reason string, refID string) (*models.WalletLedger, int, error) {
	var ledgerEntry models.WalletLedger
	var remainingBalance int

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.Profile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", clientID).
			First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProfileNotFound
			}
			return err
		}

		switch currency {
		case models.CurrencyCoins:
			if profile.Coins < amount {
				return ErrInsufficientFunds
			}
			profile.Coins -= amount
			remainingBalance = profile.Coins
		case models.CurrencyGems:
			if profile.Gems < amount {
				return ErrInsufficientFunds
			}
			profile.Gems -= amount
			remainingBalance = profile.Gems
		default:
			return fmt.Errorf("unsupported currency: %s", currency)
		}

		// Update cached aggregate on profile
		if err := tx.Model(&models.Profile{}).
			Where("id = ?", clientID).
			Updates(map[string]interface{}{
				currency: remainingBalance,
			}).Error; err != nil {
			return fmt.Errorf("failed to update profile balance: %w", err)
		}

		txType := models.TxTypePowerUpPurchase
		if reason != "" && reason != "In-game purchase" {
			txType = models.TxTypeManualDebit
		}

		ledgerEntry = models.WalletLedger{
			ClientID:        clientID,
			TransactionType: txType,
			Direction:       models.DirectionDebit,
			Amount:          amount,
			Currency:        currency,
			BalanceAfter:    remainingBalance,
			ReferenceID:     refID,
			CreatedAt:       time.Now().UTC(),
		}

		if err := tx.Create(&ledgerEntry).Error; err != nil {
			return fmt.Errorf("failed to record ledger entry: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, 0, err
	}

	_ = cache.InvalidateCachedProfile(ctx, clientID)
	return &ledgerEntry, remainingBalance, nil
}

// CreditWalletBalance atomically credits coins or gems and creates an audit
// ledger entry of type txType. When dailyCap > 0, the credit is refused
// (ErrDailyRewardLimit) if it would take today's (UTC) total of txType in this
// currency above dailyCap. The check runs under the player's row lock, so
// concurrent requests cannot both slip under the cap, with or without Redis.
func CreditWalletBalance(ctx context.Context, clientID int, currency string, amount int, txType string, refID string, dailyCap int) (*models.WalletLedger, int, error) {
	var ledgerEntry models.WalletLedger
	var newBalance int

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.Profile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", clientID).
			First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProfileNotFound
			}
			return err
		}

		if dailyCap > 0 {
			now := time.Now().UTC()
			dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			var today []int64
			if err := tx.Raw(
				`SELECT COALESCE(SUM(amount), 0) FROM wallet_ledger
				  WHERE client_id = ? AND transaction_type = ? AND currency = ? AND direction = ? AND created_at >= ?`,
				clientID, txType, currency, models.DirectionCredit, dayStart).Scan(&today).Error; err != nil {
				return fmt.Errorf("failed to read today's rewards: %w", err)
			}
			if len(today) == 1 && today[0]+int64(amount) > int64(dailyCap) {
				return ErrDailyRewardLimit
			}
		}

		switch currency {
		case models.CurrencyCoins:
			profile.Coins += amount
			newBalance = profile.Coins
		case models.CurrencyGems:
			profile.Gems += amount
			newBalance = profile.Gems
		default:
			return fmt.Errorf("unsupported currency: %s", currency)
		}

		if err := tx.Model(&models.Profile{}).
			Where("id = ?", clientID).
			Updates(map[string]interface{}{
				currency: newBalance,
			}).Error; err != nil {
			return fmt.Errorf("failed to update profile balance: %w", err)
		}

		if txType == "" {
			txType = models.TxTypeManualCredit
		}
		ledgerEntry = models.WalletLedger{
			ClientID:        clientID,
			TransactionType: txType,
			Direction:       models.DirectionCredit,
			Amount:          amount,
			Currency:        currency,
			BalanceAfter:    newBalance,
			ReferenceID:     refID,
			CreatedAt:       time.Now().UTC(),
		}

		if err := tx.Create(&ledgerEntry).Error; err != nil {
			return fmt.Errorf("failed to record ledger entry: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, 0, err
	}

	_ = cache.InvalidateCachedProfile(ctx, clientID)
	return &ledgerEntry, newBalance, nil
}

// GetWalletLedgerHistory returns paginated transaction history for a client.
func GetWalletLedgerHistory(ctx context.Context, clientID int, limit, offset int) ([]models.WalletLedger, int64, error) {
	var entries []models.WalletLedger
	var total int64

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return GetDB().WithContext(gCtx).Model(&models.WalletLedger{}).
			Where("client_id = ?", clientID).Count(&total).Error
	})

	g.Go(func() error {
		return GetDB().WithContext(gCtx).Model(&models.WalletLedger{}).
			Where("client_id = ?", clientID).
			Order("created_at DESC").Limit(limit).Offset(offset).Find(&entries).Error
	})

	if err := g.Wait(); err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
