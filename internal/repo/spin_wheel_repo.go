package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNoSpins = errors.New("no spins available")

// SpinOutcome is a committed spin plus the balances after it.
type SpinOutcome struct {
	Result   models.SpinResult
	Replayed bool // true when the idempotency key had already been used
	Spins    int
	Coins    int
	Gems     int
}

// ListActiveSpinSegments returns the active wheel segments in display order.
func ListActiveSpinSegments(ctx context.Context) ([]models.SpinWheelSegment, error) {
	var segs []models.SpinWheelSegment
	err := GetDB().WithContext(ctx).Where("active").Order("sort_order, segment_code").Find(&segs).Error
	return segs, err
}

// GetSpinBalance returns the number of spins a client can still use.
func GetSpinBalance(ctx context.Context, clientID int) (int, error) {
	var spins []int
	err := GetDB().WithContext(ctx).Raw("SELECT spins FROM clients WHERE id = ?", clientID).Scan(&spins).Error
	if err != nil {
		return 0, err
	}
	if len(spins) == 0 {
		return 0, ErrProfileNotFound
	}
	return spins[0], nil
}

// spendSpinSQL spends one spin and pays the prize in one atomic statement.
// The spin is only spent when the idempotency key is new and spins > 0; the
// UNIQUE (client_id, idempotency_key) on spin_results rolls the whole statement
// back if two requests with the same key race.
const spendSpinSQL = `
WITH spent AS (
    UPDATE clients
    SET spins = spins - 1,
        coins = coins + CASE WHEN @currency = 'coins' THEN @amount ELSE 0 END,
        gems  = gems  + CASE WHEN @currency = 'gems'  THEN @amount ELSE 0 END,
        updated_at = now()
    WHERE id = @client AND spins > 0
      AND NOT EXISTS (SELECT 1 FROM spin_results WHERE client_id = @client AND idempotency_key = @key)
    RETURNING id, spins, coins, gems
), result AS (
    INSERT INTO spin_results (client_id, idempotency_key, segment_code, currency, amount)
    SELECT id, @key, @segment, @currency, @amount FROM spent
    RETURNING id, segment_code, currency, amount, created_at
), ledger AS (
    INSERT INTO wallet_ledger (client_id, transaction_type, direction, amount, currency, balance_after, reference_id)
    SELECT s.id, @tx_type, 'credit', @amount, @currency,
           CASE WHEN @currency = 'coins' THEN s.coins ELSE s.gems END, r.id::text
    FROM spent s, result r
), spin AS (
    INSERT INTO spin_ledger (client_id, delta, reason, reference_id)
    SELECT id, -1, @reason, @key FROM spent
)
SELECT r.id, r.segment_code, r.currency, r.amount, r.created_at, s.spins, s.coins, s.gems
FROM result r, spent s`

type spinRow struct {
	models.SpinResult
	Spins int
	Coins int
	Gems  int
}

// SpendSpin spends one spin for the pre-rolled segment. A repeated idempotency key
// returns the stored result without spending or paying again.
func SpendSpin(ctx context.Context, clientID int, key string, seg models.SpinWheelSegment) (*SpinOutcome, error) {
	db := GetDB().WithContext(ctx)

	var rows []spinRow
	err := db.Raw(spendSpinSQL, map[string]any{
		"client":   clientID,
		"key":      key,
		"segment":  seg.SegmentCode,
		"currency": seg.Currency,
		"amount":   seg.Amount,
		"tx_type":  models.TxTypeSpinReward,
		"reason":   models.SpinReasonSpend,
	}).Scan(&rows).Error

	var pgErr *pgconn.PgError
	if err != nil && !(errors.As(err, &pgErr) && pgErr.Code == "23505") {
		return nil, fmt.Errorf("failed to spend spin: %w", err)
	}

	if err == nil && len(rows) == 1 {
		_ = cache.InvalidateCachedProfile(ctx, clientID)
		r := rows[0]
		return &SpinOutcome{Result: r.SpinResult, Spins: r.Spins, Coins: r.Coins, Gems: r.Gems}, nil
	}

	// Nothing spent: either a replay of a used key, or the player has no spins.
	var prev models.SpinResult
	lookup := db.Where("client_id = ? AND idempotency_key = ?", clientID, key).Limit(1).Find(&prev)
	if lookup.Error != nil {
		return nil, lookup.Error
	}
	if lookup.RowsAffected == 0 {
		return nil, ErrNoSpins
	}
	var bal struct{ Spins, Coins, Gems int }
	if err := db.Raw("SELECT spins, coins, gems FROM clients WHERE id = ?", clientID).Scan(&bal).Error; err != nil {
		return nil, err
	}
	return &SpinOutcome{Result: prev, Replayed: true, Spins: bal.Spins, Coins: bal.Coins, Gems: bal.Gems}, nil
}

// GrantSpins adds n spins, idempotent per (client, reason, reference). It reports
// whether the grant was applied (false = this reference was already granted).
func GrantSpins(ctx context.Context, clientID, n int, reason, referenceID string) (bool, error) {
	if n <= 0 {
		return false, nil
	}
	res := GetDB().WithContext(ctx).Exec(`
WITH grant_row AS (
    INSERT INTO spin_ledger (client_id, delta, reason, reference_id)
    VALUES (@client, @n, @reason, @ref)
    ON CONFLICT (client_id, reason, reference_id) DO NOTHING
    RETURNING client_id
)
UPDATE clients c SET spins = spins + @n, updated_at = now()
FROM grant_row g WHERE c.id = g.client_id`,
		map[string]any{"client": clientID, "n": n, "reason": reason, "ref": referenceID})
	if res.Error != nil {
		return false, fmt.Errorf("failed to grant spins: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}
