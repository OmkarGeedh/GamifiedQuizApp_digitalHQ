package services

// Power-ups charged on the server. The price is set here, never by the app:
// the power-up endpoint itself takes the coins (or a token) in the same
// database transaction that records the use and its wallet_ledger row, and a
// retried request is never charged twice (power_up_uses primary key).

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// PowerUpRule is one server-charged power-up.
type PowerUpRule struct {
	ID         string
	Mode       string // game mode it works in
	PriceCoins int
	PerRun     bool // once per run (else once per question)
}

// PowerUpRules are the prices the app shows (GET /power-ups) and the server charges.
var PowerUpRules = map[string]PowerUpRule{
	models.PowerUpFiftyFifty: {ID: models.PowerUpFiftyFifty, Mode: models.GameModeMCQ, PriceCoins: 20},
	models.PowerUpAddTime:    {ID: models.PowerUpAddTime, Mode: models.GameModeSuddenDeath, PriceCoins: 12, PerRun: true},
	models.PowerUpSkip:       {ID: models.PowerUpSkip, Mode: models.GameModeSuddenDeath, PriceCoins: 35, PerRun: true},
}

// powerUpOrder is the display order.
var powerUpOrder = []string{models.PowerUpFiftyFifty, models.PowerUpAddTime, models.PowerUpSkip}

// MaxPowerUpTokens is the most tokens of one kind a player may hold, so ads
// cannot be farmed into a stockpile.
const MaxPowerUpTokens = 5

// Errors game code turns into player-facing messages.
var (
	ErrPowerUpInsufficientFunds = errors.New("not enough coins for this power-up")
	ErrPowerUpNoToken           = errors.New("you have no token for this power-up")
)

// PowerUpErrorCode maps a charge error to the error code the game protocol sends.
func PowerUpErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrPowerUpInsufficientFunds):
		return "insufficient_funds"
	case errors.Is(err, ErrPowerUpNoToken):
		return "no_power_up_token"
	default:
		return "power_up_payment_failed"
	}
}

// PayForPowerUp charges one use of powerUp in a session, once. questionCode is
// ignored for once-per-run power-ups. result is stored with the use and comes
// back unchanged on a retry (50:50 hides the same options).
func PayForPowerUp(ctx context.Context, clientID int, sessionID, questionCode, powerUp, payWith, result string) (*repo.PowerUpChargeResult, error) {
	rule, ok := PowerUpRules[powerUp]
	if !ok {
		return nil, fmt.Errorf("unknown power-up %q", powerUp)
	}
	if payWith != "" && payWith != models.PayWithCoins && payWith != models.PayWithToken {
		return nil, fmt.Errorf("pay_with must be coins or token")
	}
	key := questionCode
	if rule.PerRun {
		key = "*"
	}
	res, err := repo.ChargePowerUp(ctx, repo.PowerUpCharge{
		ClientID: clientID, SessionID: sessionID, QuestionKey: key, PowerUp: powerUp,
		PayWith: payWith, Price: rule.PriceCoins, Result: result,
	})
	switch {
	case errors.Is(err, repo.ErrInsufficientFunds):
		return nil, ErrPowerUpInsufficientFunds
	case errors.Is(err, repo.ErrNoPowerUpToken):
		return nil, ErrPowerUpNoToken
	case err != nil:
		return nil, err
	}
	return res, nil
}

// GetPowerUpsStatus returns prices, the player's tokens and coins.
func GetPowerUpsStatus(ctx context.Context, clientID int) (*dto.PowerUpsStatusDTO, int, error) {
	tokens, err := repo.GetPowerUpTokens(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load power-ups: %w", err)
	}
	coins, _, err := repo.GetWalletBalance(ctx, clientID)
	if err != nil && !errors.Is(err, repo.ErrProfileNotFound) {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load balance: %w", err)
	}
	out := &dto.PowerUpsStatusDTO{MaxTokens: MaxPowerUpTokens, Coins: coins}
	for _, id := range powerUpOrder {
		r := PowerUpRules[id]
		limit := "per_question"
		if r.PerRun {
			limit = "per_run"
		}
		out.PowerUps = append(out.PowerUps, dto.PowerUpPriceDTO{ID: id, Mode: r.Mode, PriceCoins: r.PriceCoins, Limit: limit, Tokens: tokens[id]})
	}
	return out, http.StatusOK, nil
}
