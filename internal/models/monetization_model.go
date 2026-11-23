package models

import "time"

// Monetization tables come from migration 000010_monetization in the migration
// repo (client_power_ups, power_up_uses, ad_rewards, billing_product_subjects).
// The repo reads and writes them with plain SQL.

// BillingKindNonConsumable is a one-time Play product that unlocks content
// (a topic pack). It never gives currency.
const BillingKindNonConsumable = "non_consumable"

// Power-ups charged on the server. They double as wallet_ledger currencies for
// the token a player can hold instead of paying coins.
const (
	PowerUpFiftyFifty = "fifty_fifty" // MCQ: hide two wrong options (once per question)
	PowerUpAddTime    = "add_time"    // Sudden Death: +5 seconds (once per run)
	PowerUpSkip       = "skip"        // Sudden Death: skip safely (once per run)
)

// IsPowerUp reports whether s is a server-charged power-up.
func IsPowerUp(s string) bool {
	switch s {
	case PowerUpFiftyFifty, PowerUpAddTime, PowerUpSkip:
		return true
	}
	return false
}

// How a power-up was paid for (power_up_uses.paid_with).
const (
	PayWithCoins = "coins"
	PayWithToken = "token"
)

// Ledger transaction types written by the server-side economy. Each carries a
// unique reference (see uq_wallet_ledger_server_event), so an event is
// written once.
const (
	TxTypeAdReward        = "ad_reward"          // a verified rewarded ad (or a Quiz Pass claim of it)
	TxTypePassReward      = "pass_reward"        // Quiz Pass perk (monthly gems)
	TxTypePowerUpCharge   = "power_up_charge"    // coins paid for a power-up in a game
	TxTypePowerUpTokenUse = "power_up_token_use" // a token spent on a power-up in a game
)

// Ad reward placements (ad_rewards.placement): the ad chest, or one token.
const (
	AdPlacementChest = "ad_chest"
)

// Ad reward lifecycle (ad_rewards.status / ad_rewards.source).
const (
	AdStatusPending   = "pending"
	AdStatusFulfilled = "fulfilled"
	AdStatusExpired   = "expired"
	AdSourceAd        = "ad"
	AdSourcePass      = "pass"
)

// AdReward is one row of ad_rewards.
type AdReward struct {
	ID               string     `gorm:"column:id"`
	ClientID         int        `gorm:"column:client_id"`
	Placement        string     `gorm:"column:placement"`
	Source           string     `gorm:"column:source"`
	Status           string     `gorm:"column:status"`
	RewardCurrency   *string    `gorm:"column:reward_currency"`
	RewardAmount     *int       `gorm:"column:reward_amount"`
	SSVTransactionID *string    `gorm:"column:ssv_transaction_id"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	ExpiresAt        time.Time  `gorm:"column:expires_at"`
	FulfilledAt      *time.Time `gorm:"column:fulfilled_at"`
}
