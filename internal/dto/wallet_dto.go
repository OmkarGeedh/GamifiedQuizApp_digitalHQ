package dto

import (
	"errors"
	"fmt"
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

// ClientRewardRule limits one kind of reward the app is allowed to report.
type ClientRewardRule struct {
	Currency     string
	MaxPerCredit int // the largest single prize of this kind the app can award
	MaxPerDay    int // total per player per UTC day, enforced in SQL
}

// ClientRewardRules is the only list of reasons POST /wallet/credit accepts.
// The app still decides these prizes itself (spin wheel, chests), so each is
// capped at the largest prize it can really give, plus a daily total. Anything
// paid for with real money (coin packs, Premium) goes through /billing/verify,
// which checks the purchase with Google Play; it can never be credited here.
//
// Values mirror Gamified-learning/lib/features/rewards/data/rewards_catalog.dart.
var ClientRewardRules = map[string]ClientRewardRule{
	"spin_wheel":          {Currency: "coins", MaxPerCredit: 100, MaxPerDay: 300},
	"chest_reward_golden": {Currency: "coins", MaxPerCredit: 100, MaxPerDay: 2000},
	"chest_reward_cyber":  {Currency: "coins", MaxPerCredit: 150, MaxPerDay: 1500},
	"chest_reward_galaxy": {Currency: "gems", MaxPerCredit: 25, MaxPerDay: 250},
}

// WalletCreditRequestDTO validates input for crediting player wallet balance.
type WalletCreditRequestDTO struct {
	Currency    string `json:"currency" binding:"required"`
	Amount      int    `json:"amount" binding:"required"`
	Reason      string `json:"reason"`
	ReferenceID string `json:"reference_id,omitempty"`
}

// Validate ensures the reason is an allowed in-game reward and the amount is
// within what that reward can give. It returns the rule to enforce.
func (r *WalletCreditRequestDTO) Validate() (ClientRewardRule, error) {
	r.Currency = strings.ToLower(strings.TrimSpace(r.Currency))
	if r.Currency != "coins" && r.Currency != "gems" {
		return ClientRewardRule{}, errors.New("currency must be 'coins' or 'gems'")
	}
	if r.Amount <= 0 {
		return ClientRewardRule{}, errors.New("amount must be greater than zero")
	}
	r.Reason = strings.ToLower(strings.TrimSpace(r.Reason))
	rule, ok := ClientRewardRules[r.Reason]
	if !ok {
		return ClientRewardRule{}, errors.New("this reward cannot be credited by the app")
	}
	if rule.Currency != r.Currency {
		return ClientRewardRule{}, fmt.Errorf("%s pays %s, not %s", r.Reason, rule.Currency, r.Currency)
	}
	if r.Amount > rule.MaxPerCredit {
		return ClientRewardRule{}, fmt.Errorf("amount too large: %s pays at most %d %s", r.Reason, rule.MaxPerCredit, rule.Currency)
	}
	if len(r.ReferenceID) > 64 {
		return ClientRewardRule{}, errors.New("reference_id must be at most 64 characters")
	}
	return rule, nil
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
