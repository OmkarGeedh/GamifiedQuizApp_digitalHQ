package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Tables from migration 000010_monetization (migration repo). Plain SQL that
// also runs on the SQLite test database.
//
// Logging rule: every function here that changes a balance (coins, gems or a
// power-up token) writes the wallet_ledger row in the same transaction, so a
// balance never changes without its log entry and vice versa.

var (
	// ErrNoPowerUpToken means the player asked to pay with a token they do not have.
	ErrNoPowerUpToken = errors.New("no token for this power-up")
	// ErrPowerUpTokenCap means the player already holds the most tokens allowed.
	ErrPowerUpTokenCap = errors.New("power-up token limit reached")
	// ErrAdDailyCap means today's ads for this placement are used up.
	ErrAdDailyCap = errors.New("daily limit reached for this reward")
	// ErrAdTotalCap means today's ads across all placements are used up.
	ErrAdTotalCap = errors.New("daily ad limit reached")
	// ErrAdCooldown means the player asked for another ad too soon.
	ErrAdCooldown = errors.New("please wait a moment before the next ad")
	// errUseRace means a concurrent request recorded the same power-up use first.
	errUseRace = errors.New("power-up use recorded by a concurrent request")
)

// isTokenCurrency reports whether a ledger currency is a power-up token.
func isTokenCurrency(currency string) bool { return models.IsPowerUp(currency) }

// grantInTx adds amount of currency (coins, gems or a power-up token) to a
// player and writes the ledger row. It returns the balance after.
func grantInTx(tx *gorm.DB, clientID int, currency string, amount int, txType, ref string, now time.Time) (int, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("grant amount must be positive, got %d", amount)
	}
	var after []int
	switch {
	case currency == models.CurrencyCoins || currency == models.CurrencyGems:
		// currency is one of two fixed column names, never user input.
		if err := tx.Raw(
			fmt.Sprintf(`UPDATE clients SET %[1]s = %[1]s + ?, updated_at = ? WHERE id = ? RETURNING %[1]s`, currency),
			amount, now, clientID).Scan(&after).Error; err != nil {
			return 0, fmt.Errorf("credit %s: %w", currency, err)
		}
	case isTokenCurrency(currency):
		if err := tx.Raw(
			`INSERT INTO client_power_ups (client_id, power_up, quantity, updated_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT (client_id, power_up)
			 DO UPDATE SET quantity = client_power_ups.quantity + excluded.quantity, updated_at = excluded.updated_at
			 RETURNING quantity`,
			clientID, currency, amount, now).Scan(&after).Error; err != nil {
			return 0, fmt.Errorf("credit %s token: %w", currency, err)
		}
	default:
		return 0, fmt.Errorf("unsupported grant currency %q", currency)
	}
	if len(after) != 1 {
		return 0, ErrProfileNotFound
	}
	if err := insertLedger(tx, clientID, txType, models.DirectionCredit, amount, currency, after[0], ref, now); err != nil {
		return 0, err
	}
	return after[0], nil
}

// tokenQuantityInTx returns how many tokens of powerUp a player holds.
func tokenQuantityInTx(tx *gorm.DB, clientID int, powerUp string) (int, error) {
	var q []int
	if err := tx.Raw(`SELECT quantity FROM client_power_ups WHERE client_id = ? AND power_up = ?`, clientID, powerUp).Scan(&q).Error; err != nil {
		return 0, err
	}
	if len(q) == 0 {
		return 0, nil
	}
	return q[0], nil
}

// GetPowerUpTokens returns the player's token balances (missing = 0).
func GetPowerUpTokens(ctx context.Context, clientID int) (map[string]int, error) {
	var rows []struct {
		PowerUp  string
		Quantity int
	}
	if err := GetDB().WithContext(ctx).Raw(
		`SELECT power_up, quantity FROM client_power_ups WHERE client_id = ?`, clientID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int{models.PowerUpFiftyFifty: 0, models.PowerUpAddTime: 0, models.PowerUpSkip: 0}
	for _, r := range rows {
		out[r.PowerUp] = r.Quantity
	}
	return out, nil
}

// ---- Power-up charges --------------------------------------------------------------

// PowerUpCharge is one power-up use to pay for.
type PowerUpCharge struct {
	ClientID    int
	SessionID   string
	QuestionKey string // question code, or "*" for once-per-run power-ups
	PowerUp     string
	PayWith     string // "coins", "token", or "" = a token if the player has one, else coins
	Price       int    // coins
	Result      string // stored with the use and returned on retries (50:50's hidden options)
}

// PowerUpChargeResult reports how a use was paid.
type PowerUpChargeResult struct {
	AlreadyPaid bool   // this use was paid before (a retry); nothing was charged now
	PaidWith    string // coins or token
	Amount      int
	Result      string
	CoinsAfter  *int // set when paid with coins now
	TokensAfter *int // set when paid with a token now
}

// PowerUpUseRef is the ledger reference for one use: short and stable.
func PowerUpUseRef(sessionID, questionKey, powerUp string) string {
	sum := sha256.Sum256([]byte(sessionID + "|" + questionKey + "|" + powerUp))
	return "pu:" + hex.EncodeToString(sum[:16])
}

// ChargePowerUp pays for one power-up use, once. A retry of the same use (same
// session, question key and power-up) is never charged again: it returns the
// stored result. Payment, ledger row and use record are one transaction.
// ErrNoDatabase means there is no database connection to charge against.
var ErrNoDatabase = errors.New("database unavailable")

func ChargePowerUp(ctx context.Context, c PowerUpCharge) (*PowerUpChargeResult, error) {
	if GetDB() == nil {
		return nil, ErrNoDatabase // never hand out a power-up that cannot be paid for
	}
	if prev, err := findPowerUpUse(ctx, c); err != nil || prev != nil {
		return prev, err
	}
	out := &PowerUpChargeResult{Result: c.Result}
	now := time.Now().UTC()
	ref := PowerUpUseRef(c.SessionID, c.QuestionKey, c.PowerUp)

	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		paid := false
		if c.PayWith == models.PayWithToken || c.PayWith == "" {
			var left []int
			if err := tx.Raw(
				`UPDATE client_power_ups SET quantity = quantity - 1, updated_at = ?
				  WHERE client_id = ? AND power_up = ? AND quantity >= 1 RETURNING quantity`,
				now, c.ClientID, c.PowerUp).Scan(&left).Error; err != nil {
				return fmt.Errorf("spend token: %w", err)
			}
			if len(left) == 1 {
				paid = true
				out.PaidWith, out.Amount, out.TokensAfter = models.PayWithToken, 1, &left[0]
				if err := insertLedger(tx, c.ClientID, models.TxTypePowerUpTokenUse, models.DirectionDebit,
					1, c.PowerUp, left[0], ref, now); err != nil {
					return err
				}
			} else if c.PayWith == models.PayWithToken {
				return ErrNoPowerUpToken
			}
		}
		if !paid {
			var coins []int
			if err := tx.Raw(
				`UPDATE clients SET coins = coins - ?, updated_at = ? WHERE id = ? AND coins >= ? RETURNING coins`,
				c.Price, now, c.ClientID, c.Price).Scan(&coins).Error; err != nil {
				return fmt.Errorf("charge coins: %w", err)
			}
			if len(coins) != 1 {
				return ErrInsufficientFunds
			}
			out.PaidWith, out.Amount, out.CoinsAfter = models.PayWithCoins, c.Price, &coins[0]
			if err := insertLedger(tx, c.ClientID, models.TxTypePowerUpCharge, models.DirectionDebit,
				c.Price, models.CurrencyCoins, coins[0], ref, now); err != nil {
				return err
			}
		}
		res := tx.Exec(
			`INSERT INTO power_up_uses (session_id, question_code, power_up, client_id, paid_with, amount, result, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (session_id, question_code, power_up) DO NOTHING`,
			c.SessionID, c.QuestionKey, c.PowerUp, c.ClientID, out.PaidWith, out.Amount, c.Result, now)
		if res.Error != nil {
			return fmt.Errorf("record power-up use: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return errUseRace // undo this payment; the other request paid
		}
		return nil
	})
	if errors.Is(err, errUseRace) || errors.Is(err, gorm.ErrDuplicatedKey) {
		prev, ferr := findPowerUpUse(ctx, c)
		if ferr != nil {
			return nil, ferr
		}
		if prev != nil {
			return prev, nil
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	_ = cache.InvalidateCachedProfile(ctx, c.ClientID)
	return out, nil
}

// findPowerUpUse returns a recorded use as an AlreadyPaid result, or nil.
func findPowerUpUse(ctx context.Context, c PowerUpCharge) (*PowerUpChargeResult, error) {
	var prev []struct {
		ClientID int
		PaidWith string
		Amount   int
		Result   string
	}
	if err := GetDB().WithContext(ctx).Raw(
		`SELECT client_id, paid_with, amount, result FROM power_up_uses
		  WHERE session_id = ? AND question_code = ? AND power_up = ?`,
		c.SessionID, c.QuestionKey, c.PowerUp).Scan(&prev).Error; err != nil {
		return nil, err
	}
	if len(prev) == 0 {
		return nil, nil
	}
	if prev[0].ClientID != c.ClientID {
		return nil, ErrPurchaseOwnedByOther
	}
	return &PowerUpChargeResult{AlreadyPaid: true, PaidWith: prev[0].PaidWith, Amount: prev[0].Amount, Result: prev[0].Result}, nil
}

// ---- Ad rewards ------------------------------------------------------------------------

// AdRewardRequest asks for a new ad reward row (or reuses a pending one).
type AdRewardRequest struct {
	ID           string // random id, sent to AdMob as custom_data
	ClientID     int
	Placement    string
	PlacementCap int           // per placement per UTC day
	TotalCap     int           // across placements per UTC day
	Cooldown     time.Duration // minimum time between two ad requests
	TTL          time.Duration
	TokenCap     int  // for token placements: the most tokens a player may hold (0 = none)
	Instant      bool // Quiz Pass: no ad; decide and pay now
	Decide       func(tx *gorm.DB, clientID int, placement string) (currency string, amount int, err error)
	Now          time.Time
	DayStartUTC  time.Time
	ReusePending bool // return the player's pending request for this placement instead of a new one
}

const adRewardColumns = `id, client_id, placement, source, status, reward_currency, reward_amount,
	ssv_transaction_id, created_at, expires_at, fulfilled_at`

// CreateAdReward checks the daily caps, cooldown and token cap, and records
// the request. When Instant is set (Quiz Pass: no ad needed) the reward is
// decided and paid in the same transaction. A pending request for the same
// placement is reused instead of creating another.
func CreateAdReward(ctx context.Context, r AdRewardRequest) (row *models.AdReward, reused bool, err error) {
	err = GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// One request at a time per player, so two taps cannot both pass the caps.
		var p models.Profile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", r.ClientID).First(&p).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProfileNotFound
			}
			return err
		}

		if r.ReusePending && !r.Instant {
			var pending []models.AdReward
			if err := tx.Raw(`SELECT `+adRewardColumns+` FROM ad_rewards
				WHERE client_id = ? AND placement = ? AND source = ? AND status = ? AND expires_at > ?
				ORDER BY created_at DESC LIMIT 1`,
				r.ClientID, r.Placement, models.AdSourceAd, models.AdStatusPending, r.Now).Scan(&pending).Error; err != nil {
				return err
			}
			if len(pending) == 1 {
				row, reused = &pending[0], true
				return nil
			}
		}

		var counts []struct {
			Placement string
			N         int
		}
		if err := tx.Raw(`SELECT placement, COUNT(*) AS n FROM ad_rewards
			WHERE client_id = ? AND created_at >= ?
			  AND (status = ? OR (status = ? AND expires_at > ?))
			GROUP BY placement`,
			r.ClientID, r.DayStartUTC, models.AdStatusFulfilled, models.AdStatusPending, r.Now).Scan(&counts).Error; err != nil {
			return err
		}
		total, mine := 0, 0
		for _, c := range counts {
			total += c.N
			if c.Placement == r.Placement {
				mine = c.N
			}
		}
		if mine >= r.PlacementCap {
			return ErrAdDailyCap
		}
		if total >= r.TotalCap {
			return ErrAdTotalCap
		}

		if !r.Instant && r.Cooldown > 0 {
			var last []struct{ CreatedAt time.Time }
			if err := tx.Raw(`SELECT created_at FROM ad_rewards WHERE client_id = ? AND source = ?
				ORDER BY created_at DESC LIMIT 1`, r.ClientID, models.AdSourceAd).Scan(&last).Error; err != nil {
				return err
			}
			if len(last) == 1 && r.Now.Sub(last[0].CreatedAt.UTC()) < r.Cooldown {
				return ErrAdCooldown
			}
		}

		if r.TokenCap > 0 {
			held, err := tokenQuantityInTx(tx, r.ClientID, r.Placement)
			if err != nil {
				return err
			}
			if held >= r.TokenCap {
				return ErrPowerUpTokenCap
			}
		}

		source := models.AdSourceAd
		if r.Instant {
			source = models.AdSourcePass
		}
		if err := tx.Exec(`INSERT INTO ad_rewards (id, client_id, placement, source, status, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ClientID, r.Placement, source, models.AdStatusPending, r.Now, r.Now.Add(r.TTL)).Error; err != nil {
			return fmt.Errorf("record ad reward: %w", err)
		}

		if r.Instant {
			currency, amount, err := r.Decide(tx, r.ClientID, r.Placement)
			if err != nil {
				return err
			}
			if _, err := grantInTx(tx, r.ClientID, currency, amount, models.TxTypeAdReward, "ad:"+r.ID, r.Now); err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE ad_rewards SET status = ?, reward_currency = ?, reward_amount = ?, fulfilled_at = ?
				WHERE id = ?`, models.AdStatusFulfilled, currency, amount, r.Now, r.ID).Error; err != nil {
				return fmt.Errorf("complete ad reward: %w", err)
			}
		}

		var rows []models.AdReward
		if err := tx.Raw(`SELECT `+adRewardColumns+` FROM ad_rewards WHERE id = ?`, r.ID).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return errors.New("ad reward row vanished")
		}
		row = &rows[0]
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if row.Status == models.AdStatusFulfilled {
		_ = cache.InvalidateCachedProfile(ctx, r.ClientID)
	}
	return row, reused, nil
}

// GetAdReward returns one ad reward row, or nil.
func GetAdReward(ctx context.Context, id string) (*models.AdReward, error) {
	var rows []models.AdReward
	if err := GetDB().WithContext(ctx).Raw(`SELECT `+adRewardColumns+` FROM ad_rewards WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// AdRewardCountsToday returns today's counted ad rewards per placement
// (fulfilled, or pending and not expired).
func AdRewardCountsToday(ctx context.Context, clientID int, dayStartUTC, now time.Time) (map[string]int, error) {
	var counts []struct {
		Placement string
		N         int
	}
	if err := GetDB().WithContext(ctx).Raw(`SELECT placement, COUNT(*) AS n FROM ad_rewards
		WHERE client_id = ? AND created_at >= ? AND (status = ? OR (status = ? AND expires_at > ?))
		GROUP BY placement`,
		clientID, dayStartUTC, models.AdStatusFulfilled, models.AdStatusPending, now).Scan(&counts).Error; err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, c := range counts {
		out[c.Placement] = c.N
	}
	return out, nil
}

// SSVFulfillment is a verified AdMob server-side verification callback.
type SSVFulfillment struct {
	ID            string
	TransactionID string
	AdNetwork     string
	AdUnit        string
	KeyID         int64
	RewardedAt    time.Time
	Now           time.Time
	// OwnerMatches checks the callback's user id against the row's player.
	OwnerMatches func(clientID int) bool
	Decide       func(tx *gorm.DB, clientID int, placement string) (currency string, amount int, err error)
}

// SSV outcomes.
const (
	SSVFulfilled       = "fulfilled"
	SSVAlreadyDone     = "already_done"
	SSVExpired         = "expired"
	SSVUnknown         = "unknown"
	SSVWrongUser       = "wrong_user"
	SSVDuplicateTxn    = "duplicate_transaction"
	ssvDuplicateMarker = "ssv duplicate transaction"
)

// FulfillAdReward pays a pending ad reward for a verified callback, once:
// the guarded status change decides between concurrent or repeated callbacks,
// and the UNIQUE transaction id stops one ad paying two requests.
func FulfillAdReward(ctx context.Context, f SSVFulfillment) (*models.AdReward, string, error) {
	row, err := GetAdReward(ctx, f.ID)
	if err != nil {
		return nil, "", err
	}
	switch {
	case row == nil:
		return nil, SSVUnknown, nil
	case row.Status == models.AdStatusFulfilled:
		return row, SSVAlreadyDone, nil
	case row.Status != models.AdStatusPending || !f.Now.Before(row.ExpiresAt.UTC()):
		_ = GetDB().WithContext(ctx).Exec(`UPDATE ad_rewards SET status = ? WHERE id = ? AND status = ?`,
			models.AdStatusExpired, row.ID, models.AdStatusPending).Error
		return row, SSVExpired, nil
	case !f.OwnerMatches(row.ClientID):
		return row, SSVWrongUser, nil
	}

	outcome := SSVFulfilled
	err = GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		currency, amount, err := f.Decide(tx, row.ClientID, row.Placement)
		if err != nil {
			return err
		}
		res := tx.Exec(`UPDATE ad_rewards
			   SET status = ?, reward_currency = ?, reward_amount = ?, fulfilled_at = ?,
			       ssv_transaction_id = ?, ad_network = ?, ad_unit = ?, ssv_key_id = ?, ssv_rewarded_at = ?
			 WHERE id = ? AND status = ? AND expires_at > ?`,
			models.AdStatusFulfilled, currency, amount, f.Now,
			truncate(f.TransactionID, 128), truncate(f.AdNetwork, 40), truncate(f.AdUnit, 100), f.KeyID, f.RewardedAt,
			row.ID, models.AdStatusPending, f.Now)
		if res.Error != nil {
			if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
				return errors.New(ssvDuplicateMarker)
			}
			return fmt.Errorf("complete ad reward: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			outcome = SSVAlreadyDone // a concurrent callback won
			return nil
		}
		if _, err := grantInTx(tx, row.ClientID, currency, amount, models.TxTypeAdReward, "ad:"+row.ID, f.Now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if err.Error() == ssvDuplicateMarker {
			return row, SSVDuplicateTxn, nil
		}
		return nil, "", err
	}
	if outcome == SSVFulfilled {
		_ = cache.InvalidateCachedProfile(ctx, row.ClientID)
	}
	updated, err := GetAdReward(ctx, f.ID)
	if err != nil || updated == nil {
		return row, outcome, err
	}
	return updated, outcome, nil
}

// TokenQuantityInTx exposes the token balance to reward deciders inside a transaction.
func TokenQuantityInTx(tx *gorm.DB, clientID int, powerUp string) (int, error) {
	return tokenQuantityInTx(tx, clientID, powerUp)
}

// ExpireAdRewards marks pending requests past their expiry as expired.
func ExpireAdRewards(ctx context.Context, now time.Time) (int64, error) {
	res := GetDB().WithContext(ctx).Exec(`UPDATE ad_rewards SET status = ? WHERE status = ? AND expires_at <= ?`,
		models.AdStatusExpired, models.AdStatusPending, now)
	return res.RowsAffected, res.Error
}

// ---- Quiz Pass perks -------------------------------------------------------------------

// ClaimPassReward pays a Quiz Pass perk once per reference (for example once per
// month). A second claim returns ErrRewardAlreadyClaimed.
func ClaimPassReward(ctx context.Context, clientID int, currency string, amount int, ref string) (int, error) {
	var after int
	now := time.Now().UTC()
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var seen []int
		if err := tx.Raw(`SELECT 1 FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND currency = ? AND reference_id = ?`,
			clientID, models.TxTypePassReward, currency, ref).Scan(&seen).Error; err != nil {
			return err
		}
		if len(seen) > 0 {
			return ErrRewardAlreadyClaimed
		}
		a, err := grantInTx(tx, clientID, currency, amount, models.TxTypePassReward, ref, now)
		if err != nil {
			return err
		}
		after = a
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return 0, ErrRewardAlreadyClaimed // a concurrent claim won (unique ledger event)
	}
	if err != nil {
		return 0, err
	}
	_ = cache.InvalidateCachedProfile(ctx, clientID)
	return after, nil
}

// PassRewardClaimed reports whether a Quiz Pass perk reference was paid.
func PassRewardClaimed(ctx context.Context, clientID int, currency, ref string) (bool, error) {
	var seen []int
	err := GetDB().WithContext(ctx).Raw(`SELECT 1 FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND currency = ? AND reference_id = ?`,
		clientID, models.TxTypePassReward, currency, ref).Scan(&seen).Error
	return len(seen) > 0, err
}

// ---- Topic packs -------------------------------------------------------------------------

// TopicPackSubject is one row of billing_product_subjects.
type TopicPackSubject struct {
	ProductID string
	Subject   string
}

// ListTopicPackSubjects returns every pack → subject mapping.
func ListTopicPackSubjects(ctx context.Context) ([]TopicPackSubject, error) {
	var rows []TopicPackSubject
	err := GetDB().WithContext(ctx).Raw(`SELECT product_id, subject FROM billing_product_subjects ORDER BY product_id, subject`).Scan(&rows).Error
	return rows, err
}

// OwnedProductIDs returns the one-time products a player owns (granted, not refunded).
func OwnedProductIDs(ctx context.Context, clientID int) ([]string, error) {
	var ids []string
	err := GetDB().WithContext(ctx).Raw(
		`SELECT DISTINCT product_id FROM play_purchases WHERE client_id = ? AND state = 'granted' ORDER BY product_id`,
		clientID).Scan(&ids).Error
	return ids, err
}
