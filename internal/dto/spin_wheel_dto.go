package dto

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// SpinRequestDTO carries the Idempotency-Key header of POST /spin-wheel/spin.
// One key per user tap; the client reuses it when retrying after a timeout.
type SpinRequestDTO struct {
	IdempotencyKey string
}

// Validate requires a UUID so keys cannot collide with free-form client strings.
func (r *SpinRequestDTO) Validate() error {
	r.IdempotencyKey = strings.ToLower(strings.TrimSpace(r.IdempotencyKey))
	if r.IdempotencyKey == "" {
		return errors.New("Idempotency-Key header is required")
	}
	if _, err := uuid.Parse(r.IdempotencyKey); err != nil {
		return errors.New("Idempotency-Key must be a UUID")
	}
	return nil
}

// SpinSegmentDTO is one wheel segment as shown to the client.
type SpinSegmentDTO struct {
	SegmentCode string `json:"segment_code"`
	Currency    string `json:"currency"`
	Amount      int    `json:"amount"`
	SortOrder   int    `json:"sort_order"`
	IsJackpot   bool   `json:"is_jackpot"`
	// OddsPercent is the published chance of landing on this segment.
	OddsPercent float64 `json:"odds_percent"`
}

// SpinStatusDTO reports how many spins the player can still use.
type SpinStatusDTO struct {
	Spins int `json:"spins"`
}

// SpinResponseDTO is the outcome of a spin and the balances after it.
type SpinResponseDTO struct {
	SegmentCode    string `json:"segment_code"`
	Currency       string `json:"currency"`
	Amount         int    `json:"amount"`
	IsJackpot      bool   `json:"is_jackpot"`
	SpinsRemaining int    `json:"spins_remaining"`
	Coins          int    `json:"coins"`
	Gems           int    `json:"gems"`
	Replayed       bool   `json:"replayed"`
}
