package dto

import (
	"errors"
	"strings"
	"time"
)

// WalletDebitRequestDTO validates input for debiting player wallet balance.
type WalletDebitRequestDTO struct {
	Currency    string `json:"currency" binding:"required"`
	Amount      int    `json:"amount" binding:"required"`
	Reason      string `json:"reason"`
	ReferenceID string `json:"reference_id,omitempty"`
}

// Validate ensures currency and amount are valid.
func (r *WalletDebitRequestDTO) Validate() error {
	r.Currency = strings.ToLower(strings.TrimSpace(r.Currency))
	if r.Currency != "coins" && r.Currency != "gems" {
		return errors.New("currency must be 'coins' or 'gems'")
	}
	if r.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" {
		r.Reason = "In-game purchase"
	}
	return nil
}

// WalletCreditRequestDTO validates input for crediting player wallet balance.
type WalletCreditRequestDTO struct {
	Currency    string `json:"currency" binding:"required"`
	Amount      int    `json:"amount" binding:"required"`
	Reason      string `json:"reason"`
	ReferenceID string `json:"reference_id,omitempty"`
}

// Validate ensures currency and amount are valid.
func (r *WalletCreditRequestDTO) Validate() error {
	r.Currency = strings.ToLower(strings.TrimSpace(r.Currency))
	if r.Currency != "coins" && r.Currency != "gems" {
		return errors.New("currency must be 'coins' or 'gems'")
	}
	if r.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" {
		r.Reason = "Wallet credit"
	}
	return nil
}

// WalletBalanceDTO represents current wallet balance.
type WalletBalanceDTO struct {
	Coins int `json:"coins"`
	Gems  int `json:"gems"`
}

// WalletTransactionDTO represents a single immutable ledger record.
type WalletTransactionDTO struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Currency     string    `json:"currency"`
	Direction    string    `json:"direction"`
	Amount       int       `json:"amount"`
	Reason       string    `json:"reason"`
	BalanceAfter int       `json:"balance_after"`
	CreatedAt    time.Time `json:"created_at"`
}

// WalletHistoryResponseDTO wraps a paginated list of transactions.
type WalletHistoryResponseDTO struct {
	Transactions []WalletTransactionDTO `json:"transactions"`
	Total        int64                  `json:"total"`
	Limit        int                    `json:"limit"`
	Offset       int                    `json:"offset"`
}
