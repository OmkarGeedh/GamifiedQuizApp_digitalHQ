package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The billing tables come from migration 000009_play_billing (migration repo).
// Everything here is plain SQL that also runs on the SQLite test database.

var (
	// ErrBillingProductNotFound means the product id is not in billing_products.
	ErrBillingProductNotFound = errors.New("billing product not found")
	// ErrPurchaseOwnedByOther means the purchase token is already linked to a
	// different player: a replayed or shared token.
	ErrPurchaseOwnedByOther = errors.New("purchase belongs to another account")
)

// GetBillingProduct returns one catalog row, active or not (a purchase made
// before a product was retired must still be honoured).
func GetBillingProduct(ctx context.Context, productID string) (*models.BillingProduct, error) {
	var rows []models.BillingProduct
	err := GetDB().WithContext(ctx).Raw(
		`SELECT product_id, kind, title, coins, gems, active, sort_order FROM billing_products WHERE product_id = ?`,
		productID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, ErrBillingProductNotFound
	}
	return &rows[0], nil
}

// ListActiveBillingProducts returns the products the app may offer, in order.
func ListActiveBillingProducts(ctx context.Context) ([]models.BillingProduct, error) {
	var rows []models.BillingProduct
	err := GetDB().WithContext(ctx).Raw(
		`SELECT product_id, kind, title, coins, gems, active, sort_order
		   FROM billing_products WHERE active = ? ORDER BY sort_order, product_id`, true).Scan(&rows).Error
	return rows, err
}

// ConsumableGrant is a verified one-time purchase to pay out.
type ConsumableGrant struct {
	ClientID      int
	ProductID     string
	PurchaseToken string
	OrderID       string
	Quantity      int
	Coins         int // total to add (already multiplied by quantity)
	Gems          int
	IsTest        bool
	PurchasedAt   time.Time
	LedgerRef     string // wallet_ledger.reference_id (≤ 64 chars)
	// Audit is written in the same transaction as a new grant, so a grant is
	// never committed without its audit row.
	Audit *BillingAudit
}

// ConsumableGrantResult reports what GrantConsumable did.
type ConsumableGrantResult struct {
	AlreadyGranted bool // the token was granted before; nothing was added now
	Revoked        bool // the earlier grant was refunded
	Consumed       bool // Google already confirmed consumption
	Coins          int  // balances after
	Gems           int
}

// GrantConsumable records the purchase and pays it out in one transaction.
// The UNIQUE purchase_token decides: the first insert wins, so however many
// requests arrive at once (or from however many accounts) it pays once.
func GrantConsumable(ctx context.Context, g ConsumableGrant) (*ConsumableGrantResult, error) {
	out := &ConsumableGrantResult{}
	now := time.Now().UTC()

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`INSERT INTO play_purchases (client_id, product_id, purchase_token, order_id, quantity, coins, gems,
			                             state, consumed, test_purchase, purchased_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'granted', ?, ?, ?, ?)
			 ON CONFLICT (purchase_token) DO NOTHING`,
			g.ClientID, g.ProductID, g.PurchaseToken, truncate(g.OrderID, 64), g.Quantity, g.Coins, g.Gems,
			false, g.IsTest, g.PurchasedAt.UTC(), now)
		if res.Error != nil {
			return fmt.Errorf("record purchase: %w", res.Error)
		}

		if res.RowsAffected == 0 {
			// Seen before: only the same player may get an (idempotent) answer.
			var prev []struct {
				ClientID int
				State    string
				Consumed bool
			}
			if err := tx.Raw(`SELECT client_id, state, consumed FROM play_purchases WHERE purchase_token = ?`,
				g.PurchaseToken).Scan(&prev).Error; err != nil {
				return err
			}
			if len(prev) != 1 {
				return errors.New("purchase row vanished")
			}
			if prev[0].ClientID != g.ClientID {
				return ErrPurchaseOwnedByOther
			}
			out.AlreadyGranted = true
			out.Revoked = prev[0].State == "revoked"
			out.Consumed = prev[0].Consumed
			var bal []struct{ Coins, Gems int }
			if err := tx.Raw(`SELECT coins, gems FROM clients WHERE id = ?`, g.ClientID).Scan(&bal).Error; err != nil {
				return err
			}
			if len(bal) == 1 {
				out.Coins, out.Gems = bal[0].Coins, bal[0].Gems
			}
			return nil
		}

		var bal []struct{ Coins, Gems int }
		if err := tx.Raw(
			`UPDATE clients SET coins = coins + ?, gems = gems + ?, updated_at = ? WHERE id = ? RETURNING coins, gems`,
			g.Coins, g.Gems, now, g.ClientID).Scan(&bal).Error; err != nil {
			return fmt.Errorf("credit purchase: %w", err)
		}
		if len(bal) != 1 {
			return ErrProfileNotFound
		}
		out.Coins, out.Gems = bal[0].Coins, bal[0].Gems

		if g.Coins > 0 {
			if err := insertLedger(tx, g.ClientID, models.TxTypeIAPPurchase, models.DirectionCredit,
				g.Coins, models.CurrencyCoins, out.Coins, g.LedgerRef, now); err != nil {
				return err
			}
		}
		if g.Gems > 0 {
			if err := insertLedger(tx, g.ClientID, models.TxTypeIAPPurchase, models.DirectionCredit,
				g.Gems, models.CurrencyGems, out.Gems, g.LedgerRef, now); err != nil {
				return err
			}
		}
		if g.Audit != nil {
			if err := insertAuditTx(tx, *g.Audit, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !out.AlreadyGranted {
		_ = cache.InvalidateCachedProfile(ctx, g.ClientID)
	}
	return out, nil
}

func insertLedger(tx *gorm.DB, clientID int, txType, direction string, amount int, currency string, balanceAfter int, ref string, at time.Time) error {
	err := tx.Exec(
		`INSERT INTO wallet_ledger (client_id, transaction_type, direction, amount, currency, balance_after, reference_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		clientID, txType, direction, amount, currency, balanceAfter, truncate(ref, 64), at).Error
	if err != nil {
		return fmt.Errorf("record ledger entry: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// PurchaseOwner returns the player a one-time purchase token was granted to.
func PurchaseOwner(ctx context.Context, token string) (clientID int, found bool, err error) {
	var ids []int
	err = GetDB().WithContext(ctx).Raw(`SELECT client_id FROM play_purchases WHERE purchase_token = ?`, token).Scan(&ids).Error
	if err != nil || len(ids) == 0 {
		return 0, false, err
	}
	return ids[0], true, nil
}

// MarkPurchaseConsumed records that Google confirmed the consumption.
func MarkPurchaseConsumed(ctx context.Context, token string) error {
	return GetDB().WithContext(ctx).Exec(`UPDATE play_purchases SET consumed = ? WHERE purchase_token = ?`, true, token).Error
}

// UnconsumedPurchase is a granted purchase Google has not confirmed as consumed.
type UnconsumedPurchase struct {
	ClientID      int
	ProductID     string
	PurchaseToken string
	Kind          string // consumable (consume with Google) or non_consumable (acknowledge)
}

// ListUnconsumedPurchases returns granted, unconsumed purchases created before
// olderThan, oldest first. Unconsumed purchases are refunded by Google after 3 days.
func ListUnconsumedPurchases(ctx context.Context, olderThan time.Time, limit int) ([]UnconsumedPurchase, error) {
	var rows []UnconsumedPurchase
	err := GetDB().WithContext(ctx).Raw(
		`SELECT pp.client_id, pp.product_id, pp.purchase_token, bp.kind
		   FROM play_purchases pp JOIN billing_products bp ON bp.product_id = pp.product_id
		  WHERE pp.consumed = ? AND pp.state = 'granted' AND pp.created_at < ?
		  ORDER BY pp.created_at LIMIT ?`, false, olderThan.UTC(), limit).Scan(&rows).Error
	return rows, err
}

// RevokeResult reports what a refund took back.
type RevokeResult struct {
	Found          bool // a granted purchase was revoked now
	AlreadyRevoked bool
	ClientID       int
	ProductID      string
	CoinsTaken     int
	GemsTaken      int
	CoinsShort     int // could not be taken back: the player had already spent them
	GemsShort      int
}

// RevokeConsumable takes back a refunded pack: marks it revoked and debits what
// it gave, never below zero. Safe to repeat: only the first call takes anything.
func RevokeConsumable(ctx context.Context, token, tokenHash, source string) (*RevokeResult, error) {
	out := &RevokeResult{}
	now := time.Now().UTC()

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ClientID  int
			ProductID string
			Coins     int
			Gems      int
		}
		if err := tx.Raw(
			`UPDATE play_purchases SET state = 'revoked', revoked_at = ?
			  WHERE purchase_token = ? AND state = 'granted'
			  RETURNING client_id, product_id, coins, gems`, now, token).Scan(&rows).Error; err != nil {
			return fmt.Errorf("revoke purchase: %w", err)
		}
		if len(rows) == 0 {
			var prev []int
			if err := tx.Raw(`SELECT client_id FROM play_purchases WHERE purchase_token = ?`, token).Scan(&prev).Error; err != nil {
				return err
			}
			if len(prev) == 1 {
				out.AlreadyRevoked = true
				out.ClientID = prev[0]
			}
			return nil
		}
		r := rows[0]
		out.Found, out.ClientID, out.ProductID = true, r.ClientID, r.ProductID

		// Lock the player row so the amount taken matches the balance it is taken from.
		var p models.Profile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, coins, gems").Where("id = ?", r.ClientID).First(&p).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // player deleted: nothing to take back
			}
			return err
		}
		out.CoinsTaken = min(p.Coins, r.Coins)
		out.GemsTaken = min(p.Gems, r.Gems)
		out.CoinsShort = r.Coins - out.CoinsTaken
		out.GemsShort = r.Gems - out.GemsTaken
		if err := insertAuditTx(tx, BillingAudit{ClientID: r.ClientID, TokenHash: tokenHash, ProductID: r.ProductID,
			Action: "refund", Result: "ok", Reason: fmt.Sprintf("%s: took coins=%d gems=%d", source, out.CoinsTaken, out.GemsTaken)}, now); err != nil {
			return err
		}
		if out.CoinsShort > 0 || out.GemsShort > 0 {
			// Flags the account for review: the player spent refunded currency.
			if err := insertAuditTx(tx, BillingAudit{ClientID: r.ClientID, TokenHash: tokenHash, ProductID: r.ProductID,
				Action: "refund", Result: "shortfall", Reason: fmt.Sprintf("could not take back coins=%d gems=%d", out.CoinsShort, out.GemsShort)}, now); err != nil {
				return err
			}
		}
		if out.CoinsTaken == 0 && out.GemsTaken == 0 {
			return nil
		}
		if err := tx.Exec(`UPDATE clients SET coins = coins - ?, gems = gems - ?, updated_at = ? WHERE id = ?`,
			out.CoinsTaken, out.GemsTaken, now, r.ClientID).Error; err != nil {
			return fmt.Errorf("debit refund: %w", err)
		}
		ref := "refund:" + truncate(token, 32)
		if out.CoinsTaken > 0 {
			if err := insertLedger(tx, r.ClientID, models.TxTypeIAPRefund, models.DirectionDebit,
				out.CoinsTaken, models.CurrencyCoins, p.Coins-out.CoinsTaken, ref, now); err != nil {
				return err
			}
		}
		if out.GemsTaken > 0 {
			if err := insertLedger(tx, r.ClientID, models.TxTypeIAPRefund, models.DirectionDebit,
				out.GemsTaken, models.CurrencyGems, p.Gems-out.GemsTaken, ref, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out.CoinsTaken > 0 || out.GemsTaken > 0 {
		_ = cache.InvalidateCachedProfile(ctx, out.ClientID)
	}
	return out, nil
}

// ---- Subscriptions ---------------------------------------------------------

// GetSubscriptionByToken returns the stored subscription for a token, or nil.
func GetSubscriptionByToken(ctx context.Context, token string) (*models.ClientSubscription, error) {
	var rows []models.ClientSubscription
	err := GetDB().WithContext(ctx).Raw(
		`SELECT purchase_token, client_id, product_id, COALESCE(linked_purchase_token, '') AS linked_purchase_token,
		        status, expires_at, auto_renewing, acknowledged, latest_order_id, test_purchase
		   FROM client_subscriptions WHERE purchase_token = ?`, token).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ListClientSubscriptions returns a player's subscriptions, newest expiry first.
func ListClientSubscriptions(ctx context.Context, clientID int) ([]models.ClientSubscription, error) {
	var rows []models.ClientSubscription
	err := GetDB().WithContext(ctx).Raw(
		`SELECT purchase_token, client_id, product_id, COALESCE(linked_purchase_token, '') AS linked_purchase_token,
		        status, expires_at, auto_renewing, acknowledged, latest_order_id, test_purchase
		   FROM client_subscriptions WHERE client_id = ?
		  ORDER BY expires_at DESC LIMIT 20`, clientID).Scan(&rows).Error
	return rows, err
}

// UpsertSubscription stores Google's current view of a subscription token for
// one player. A token already linked to another player is refused. A 'revoked'
// row stays revoked. The token it replaces (linked token) is marked superseded.
func UpsertSubscription(ctx context.Context, s models.ClientSubscription) error {
	now := time.Now().UTC()
	var linked any
	if s.LinkedPurchaseToken != "" {
		linked = s.LinkedPurchaseToken
	}
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`INSERT INTO client_subscriptions (purchase_token, client_id, product_id, linked_purchase_token, status,
			                                   expires_at, auto_renewing, acknowledged, latest_order_id, test_purchase,
			                                   created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (purchase_token) DO NOTHING`,
			s.PurchaseToken, s.ClientID, s.ProductID, linked, s.Status, s.ExpiresAt, s.AutoRenewing,
			s.Acknowledged, truncate(s.LatestOrderID, 64), s.TestPurchase, now, now)
		if res.Error != nil {
			return fmt.Errorf("record subscription: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			var owner []int
			if err := tx.Raw(`SELECT client_id FROM client_subscriptions WHERE purchase_token = ?`, s.PurchaseToken).Scan(&owner).Error; err != nil {
				return err
			}
			if len(owner) != 1 || owner[0] != s.ClientID {
				return ErrPurchaseOwnedByOther
			}
			if err := tx.Exec(
				`UPDATE client_subscriptions
				    SET status = ?, expires_at = ?, auto_renewing = ?, acknowledged = (acknowledged OR ?),
				        latest_order_id = ?, linked_purchase_token = ?, test_purchase = ?, updated_at = ?
				  WHERE purchase_token = ? AND status <> ?`,
				s.Status, s.ExpiresAt, s.AutoRenewing, s.Acknowledged, truncate(s.LatestOrderID, 64), linked,
				s.TestPurchase, now, s.PurchaseToken, models.SubStatusRevoked).Error; err != nil {
				return fmt.Errorf("update subscription: %w", err)
			}
		}
		if s.LinkedPurchaseToken != "" && s.LinkedPurchaseToken != s.PurchaseToken {
			if err := tx.Exec(
				`UPDATE client_subscriptions SET status = ?, updated_at = ?
				  WHERE purchase_token = ? AND client_id = ? AND status <> ?`,
				models.SubStatusSuperseded, now, s.LinkedPurchaseToken, s.ClientID, models.SubStatusRevoked).Error; err != nil {
				return fmt.Errorf("supersede old subscription: %w", err)
			}
		}
		return nil
	})
}

// MarkSubscriptionAcknowledged records that Google confirmed the acknowledgement.
func MarkSubscriptionAcknowledged(ctx context.Context, token string) error {
	return GetDB().WithContext(ctx).Exec(`UPDATE client_subscriptions SET acknowledged = ? WHERE purchase_token = ?`, true, token).Error
}

// ListUnacknowledgedSubscriptions returns paying subscriptions Google has not
// confirmed as acknowledged, updated before olderThan.
func ListUnacknowledgedSubscriptions(ctx context.Context, olderThan time.Time, limit int) ([]models.ClientSubscription, error) {
	var rows []models.ClientSubscription
	err := GetDB().WithContext(ctx).Raw(
		`SELECT purchase_token, client_id, product_id, status
		   FROM client_subscriptions
		  WHERE acknowledged = ? AND status IN (?, ?) AND updated_at < ?
		  ORDER BY updated_at LIMIT ?`,
		false, models.SubStatusActive, models.SubStatusInGrace, olderThan.UTC(), limit).Scan(&rows).Error
	return rows, err
}

// RevokeSubscription ends access for a refunded subscription token.
func RevokeSubscription(ctx context.Context, token string) (found bool, clientID int, err error) {
	var ids []int
	err = GetDB().WithContext(ctx).Raw(
		`UPDATE client_subscriptions SET status = ?, updated_at = ?
		  WHERE purchase_token = ? AND status <> ? RETURNING client_id`,
		models.SubStatusRevoked, time.Now().UTC(), token, models.SubStatusRevoked).Scan(&ids).Error
	if err != nil || len(ids) == 0 {
		return false, 0, err
	}
	return true, ids[0], nil
}

// ---- Audit and notification dedupe -----------------------------------------

// BillingAudit is one billing_audit row. TokenHash is the hex SHA-256 of the
// purchase token; raw tokens are never written here.
type BillingAudit struct {
	ClientID  int // 0 = unknown
	TokenHash string
	ProductID string
	Action    string
	Result    string
	Reason    string
}

// insertAuditTx appends an audit row inside a transaction.
func insertAuditTx(tx *gorm.DB, a BillingAudit, at time.Time) error {
	var clientID any
	if a.ClientID > 0 {
		clientID = a.ClientID
	}
	if err := tx.Exec(
		`INSERT INTO billing_audit (client_id, purchase_token_hash, product_id, action, result, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		clientID, a.TokenHash, truncate(a.ProductID, 64), truncate(a.Action, 20), truncate(a.Result, 20),
		truncate(a.Reason, 200), at).Error; err != nil {
		return fmt.Errorf("record billing audit: %w", err)
	}
	return nil
}

// InsertBillingAudit appends an audit row.
func InsertBillingAudit(ctx context.Context, a BillingAudit) error {
	var clientID any
	if a.ClientID > 0 {
		clientID = a.ClientID
	}
	return GetDB().WithContext(ctx).Exec(
		`INSERT INTO billing_audit (client_id, purchase_token_hash, product_id, action, result, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		clientID, a.TokenHash, truncate(a.ProductID, 64), truncate(a.Action, 20), truncate(a.Result, 20),
		truncate(a.Reason, 200), time.Now().UTC()).Error
}

// RTDNMessageSeen reports whether a Pub/Sub message was already handled.
func RTDNMessageSeen(ctx context.Context, messageID string) (bool, error) {
	var n []int
	err := GetDB().WithContext(ctx).Raw(`SELECT 1 FROM billing_rtdn_messages WHERE message_id = ?`, messageID).Scan(&n).Error
	return len(n) > 0, err
}

// RecordRTDNMessage marks a Pub/Sub message as handled.
func RecordRTDNMessage(ctx context.Context, messageID string) error {
	return GetDB().WithContext(ctx).Exec(
		`INSERT INTO billing_rtdn_messages (message_id, received_at) VALUES (?, ?) ON CONFLICT (message_id) DO NOTHING`,
		truncate(messageID, 100), time.Now().UTC()).Error
}
