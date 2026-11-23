package dto

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	billingProductIDPattern = regexp.MustCompile(`^[a-z0-9_.]{1,64}$`)
	// Play purchase tokens are opaque printable ASCII; this rejects anything else.
	purchaseTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._\-:]+$`)
)

// MaxPurchaseTokenLength is the longest purchase token accepted (column size).
const MaxPurchaseTokenLength = 512

// VerifyPurchaseRequestDTO is what the app sends after Google Play reports a
// purchase. Only the token and product id are taken from the app; every other
// fact (paid, amount, owner, expiry) comes from Google.
type VerifyPurchaseRequestDTO struct {
	ProductID     string `json:"product_id"`
	PurchaseToken string `json:"purchase_token"`
}

// Validate checks the shape of the request.
func (r *VerifyPurchaseRequestDTO) Validate() error {
	r.ProductID = strings.TrimSpace(r.ProductID)
	r.PurchaseToken = strings.TrimSpace(r.PurchaseToken)
	if !billingProductIDPattern.MatchString(r.ProductID) {
		return errors.New("product_id is invalid")
	}
	if r.PurchaseToken == "" {
		return errors.New("purchase_token is required")
	}
	if len(r.PurchaseToken) > MaxPurchaseTokenLength || !purchaseTokenPattern.MatchString(r.PurchaseToken) {
		return errors.New("purchase_token is invalid")
	}
	return nil
}

// BillingProductDTO is one product the app may offer. Prices are not here:
// the app shows the localized price Google Play returns for the product id.
type BillingProductDTO struct {
	ProductID string `json:"product_id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Coins     int    `json:"coins"`
	Gems      int    `json:"gems"`
	// Subjects a topic pack (kind non_consumable) unlocks.
	Subjects []string `json:"subjects,omitempty"`
	// Owned is true for a topic pack the player already has.
	Owned bool `json:"owned,omitempty"`
}

// BillingAccountDTO carries the opaque id the app passes to Google Play at
// checkout (obfuscatedAccountId), which ties the purchase to this player.
type BillingAccountDTO struct {
	ObfuscatedAccountID string `json:"obfuscated_account_id"`
}

// SubscriptionStatusDTO says whether the player has Premium right now. The
// server computes it; the app uses it for display only.
type SubscriptionStatusDTO struct {
	IsPremium    bool       `json:"is_premium"`
	ProductID    string     `json:"product_id,omitempty"`
	Status       string     `json:"status,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	AutoRenewing bool       `json:"auto_renewing"`
}

// Verify results.
const (
	VerifyStatusGranted        = "granted"
	VerifyStatusAlreadyGranted = "already_granted"
	VerifyStatusPending        = "pending"
)

// VerifyPurchaseResponseDTO reports the outcome of a verification.
type VerifyPurchaseResponseDTO struct {
	Status       string                 `json:"status"`
	ProductID    string                 `json:"product_id"`
	Kind         string                 `json:"kind"`
	CoinsAdded   int                    `json:"coins_added"`
	GemsAdded    int                    `json:"gems_added"`
	Balance      *WalletBalanceDTO      `json:"balance,omitempty"`
	Subscription *SubscriptionStatusDTO `json:"subscription,omitempty"`
	// UnlockedSubjects lists what a topic pack opened.
	UnlockedSubjects []string `json:"unlocked_subjects,omitempty"`
}
