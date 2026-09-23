package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrProfileNotFound   = errors.New("profile not found")
)

// GetWalletBalance fetches only the coin and gem counts for a client.
func GetWalletBalance(ctx context.Context, clientID int) (coins int, gems int, err error) {
	var profile models.Profile
	err = GetDB().WithContext(ctx).
		Select("coins, gems").
		Where("client_id = ?", clientID).
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
			Where("client_id = ?", clientID).
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
			Where("client_id = ?", clientID).
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

	return &ledgerEntry, remainingBalance, nil
}

// CreditWalletBalance atomically credits coins or gems and creates an audit ledger entry.
func CreditWalletBalance(ctx context.Context, clientID int, currency string, amount int, reason string, refID string) (*models.WalletLedger, int, error) {
	var ledgerEntry models.WalletLedger
	var newBalance int

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.Profile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("client_id = ?", clientID).
			First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProfileNotFound
			}
			return err
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
			Where("client_id = ?", clientID).
			Updates(map[string]interface{}{
				currency: newBalance,
			}).Error; err != nil {
			return fmt.Errorf("failed to update profile balance: %w", err)
		}

		ledgerEntry = models.WalletLedger{
			ClientID:        clientID,
			TransactionType: models.TxTypeManualCredit,
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

	return &ledgerEntry, newBalance, nil
}

// GetWalletLedgerHistory returns paginated transaction history for a client.
func GetWalletLedgerHistory(ctx context.Context, clientID int, limit, offset int) ([]models.WalletLedger, int64, error) {
	var entries []models.WalletLedger
	var total int64

	db := GetDB().WithContext(ctx).Model(&models.WalletLedger{}).Where("client_id = ?", clientID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&entries).Error
	return entries, total, err
}
