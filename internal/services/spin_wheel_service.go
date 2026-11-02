package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

const (
	spinSegmentsCacheKey = "quiz:catalog:spin_wheel_segments"
	spinSegmentsCacheTTL = 1 * time.Minute
)

var (
	ErrSpinWheelMisconfigured = errors.New("spin wheel is temporarily unavailable")
	errNoSpins                = errors.New("no spins available")
)

// spinRoller returns a uniform integer in [0, total). Tests replace it to force outcomes.
var spinRoller = func(total int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err)) // never fall back to a weak source
	}
	return int(n.Int64())
}

// ValidateSpinSegments checks the odds table: at least one segment and weights that
// total exactly 1,000,000 ppm. A bad table must never be used to pay prizes.
func ValidateSpinSegments(segs []models.SpinWheelSegment) error {
	if len(segs) == 0 {
		return errors.New("no active spin wheel segments")
	}
	total := 0
	for _, s := range segs {
		if s.WeightPPM <= 0 || s.Amount <= 0 || (s.Currency != models.CurrencyCoins && s.Currency != models.CurrencyGems) {
			return fmt.Errorf("invalid spin wheel segment %q", s.SegmentCode)
		}
		total += s.WeightPPM
	}
	if total != models.SpinWheelTotalPPM {
		return fmt.Errorf("spin wheel weights total %d ppm, want %d", total, models.SpinWheelTotalPPM)
	}
	return nil
}

// PickSpinSegment maps a roll in [0, 1_000_000) onto the cumulative weights.
// segs must be in a stable order (sort_order) and already validated.
func PickSpinSegment(segs []models.SpinWheelSegment, roll int) models.SpinWheelSegment {
	cum := 0
	for _, s := range segs {
		cum += s.WeightPPM
		if roll < cum {
			return s
		}
	}
	return segs[len(segs)-1]
}

func loadSpinSegments(ctx context.Context) ([]models.SpinWheelSegment, error) {
	var segs []models.SpinWheelSegment
	if !cache.GetJSON(ctx, spinSegmentsCacheKey, &segs) {
		var err error
		if segs, err = repo.ListActiveSpinSegments(ctx); err != nil {
			return nil, err
		}
	}
	if err := ValidateSpinSegments(segs); err != nil {
		log.Printf("spin wheel misconfigured: %v", err)
		return nil, ErrSpinWheelMisconfigured
	}
	_ = cache.SetJSON(ctx, spinSegmentsCacheKey, segs, spinSegmentsCacheTTL)
	return segs, nil
}

// GetSpinWheelSegments lists the wheel with published odds.
func GetSpinWheelSegments(ctx context.Context) ([]dto.SpinSegmentDTO, int, error) {
	segs, err := loadSpinSegments(ctx)
	if err != nil {
		return nil, spinLoadStatus(err), err
	}
	out := make([]dto.SpinSegmentDTO, len(segs))
	for i, s := range segs {
		out[i] = dto.SpinSegmentDTO{
			SegmentCode: s.SegmentCode, Currency: s.Currency, Amount: s.Amount,
			SortOrder: s.SortOrder, IsJackpot: s.IsJackpot,
			OddsPercent: float64(s.WeightPPM) / float64(models.SpinWheelTotalPPM) * 100,
		}
	}
	return out, http.StatusOK, nil
}

// GetSpinStatus returns the player's spin balance.
func GetSpinStatus(ctx context.Context, clientID int) (*dto.SpinStatusDTO, int, error) {
	spins, err := repo.GetSpinBalance(ctx, clientID)
	if err != nil {
		if errors.Is(err, repo.ErrProfileNotFound) {
			return nil, http.StatusNotFound, err
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch spins: %w", err)
	}
	return &dto.SpinStatusDTO{Spins: spins}, http.StatusOK, nil
}

// Spin rolls a prize server-side and commits it atomically with the spin spend.
// 409 means the player has no spins; replays of an idempotency key return the first result.
func Spin(ctx context.Context, clientID int, req *dto.SpinRequestDTO) (*dto.SpinResponseDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	segs, err := loadSpinSegments(ctx)
	if err != nil {
		return nil, spinLoadStatus(err), err
	}

	picked := PickSpinSegment(segs, spinRoller(models.SpinWheelTotalPPM))
	out, err := repo.SpendSpin(ctx, clientID, req.IdempotencyKey, picked)
	if err != nil {
		if errors.Is(err, repo.ErrNoSpins) {
			return nil, http.StatusConflict, errNoSpins
		}
		return nil, http.StatusInternalServerError, err
	}

	isJackpot := false
	for _, s := range segs {
		if s.SegmentCode == out.Result.SegmentCode {
			isJackpot = s.IsJackpot
		}
	}
	if isJackpot && !out.Replayed {
		log.Printf("spin wheel JACKPOT: client=%d segment=%s %d %s", clientID, out.Result.SegmentCode, out.Result.Amount, out.Result.Currency)
	}

	return &dto.SpinResponseDTO{
		SegmentCode: out.Result.SegmentCode, Currency: out.Result.Currency, Amount: out.Result.Amount,
		IsJackpot: isJackpot, SpinsRemaining: out.Spins, Coins: out.Coins, Gems: out.Gems,
		Replayed: out.Replayed,
	}, http.StatusOK, nil
}

// GrantSpins awards spins idempotently per (reason, referenceID), e.g. a game session id.
func GrantSpins(ctx context.Context, clientID, n int, reason, referenceID string) (bool, error) {
	return repo.GrantSpins(ctx, clientID, n, reason, referenceID)
}

func spinLoadStatus(err error) int {
	if errors.Is(err, ErrSpinWheelMisconfigured) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
