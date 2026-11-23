package models

import "time"

// Billing tables come from migration 000009_play_billing in the migration repo.
// The backend never migrates them; the repo reads and writes them with plain SQL.

// Billing product kinds.
const (
	BillingKindConsumable   = "consumable"
	BillingKindSubscription = "subscription"
)

// Subscription statuses stored in client_subscriptions.status.
const (
	SubStatusActive     = "active"
	SubStatusInGrace    = "in_grace"
	SubStatusOnHold     = "on_hold"
	SubStatusPaused     = "paused"
	SubStatusCanceled   = "canceled" // auto-renew turned off; access until expiry
	SubStatusExpired    = "expired"
	SubStatusPending    = "pending"
	SubStatusRevoked    = "revoked"    // refunded / revoked by Google
	SubStatusSuperseded = "superseded" // replaced by an upgrade or resubscribe
)

// SubscriptionGrantsAccess reports whether a status (before its expiry) gives Premium.
func SubscriptionGrantsAccess(status string) bool {
	switch status {
	case SubStatusActive, SubStatusInGrace, SubStatusCanceled:
		return true
	}
	return false
}

// BillingProduct is one row of billing_products.
type BillingProduct struct {
	ProductID string `gorm:"column:product_id" json:"product_id"`
	Kind      string `gorm:"column:kind" json:"kind"`
	Title     string `gorm:"column:title" json:"title"`
	Coins     int    `gorm:"column:coins" json:"coins"`
	Gems      int    `gorm:"column:gems" json:"gems"`
	Active    bool   `gorm:"column:active" json:"active"`
	SortOrder int    `gorm:"column:sort_order" json:"sort_order"`
}

// ClientSubscription is one row of client_subscriptions.
type ClientSubscription struct {
	PurchaseToken       string     `gorm:"column:purchase_token"`
	ClientID            int        `gorm:"column:client_id"`
	ProductID           string     `gorm:"column:product_id"`
	LinkedPurchaseToken string     `gorm:"column:linked_purchase_token"`
	Status              string     `gorm:"column:status"`
	ExpiresAt           *time.Time `gorm:"column:expires_at"`
	AutoRenewing        bool       `gorm:"column:auto_renewing"`
	Acknowledged        bool       `gorm:"column:acknowledged"`
	LatestOrderID       string     `gorm:"column:latest_order_id"`
	TestPurchase        bool       `gorm:"column:test_purchase"`
}
