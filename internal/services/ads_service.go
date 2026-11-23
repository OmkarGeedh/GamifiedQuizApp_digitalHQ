package services

// Rewarded ads (AdMob) with server-side verification.
//
// Flow: the app asks for a reward (POST /ads/rewards), the server checks the
// daily caps and records a pending ad_rewards row, the app shows a rewarded ad
// with ServerSideVerificationOptions(userId = ssv_user_id, customData = row id),
// Google calls GET /ads/ssv with a signed query, and only then the server pays
// the reward (coins, gems or a power-up token) in the same transaction that
// fulfils the row and writes the wallet_ledger entry. The app polls the row.
// Nothing the app says about an ad is trusted.
//
// Not annoying by design: only rewarded ads, only when the player taps "watch",
// never during a live question, a cooldown between ads, small daily caps, and
// Quiz Pass holders get the same rewards without any ad.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services/admob"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AdPlacementRule is one thing a player can get for an ad.
type AdPlacementRule struct {
	ID       string
	Reward   string // "chest" or the power-up token it gives
	DailyCap int
}

// AdPlacementRules: the ad chest and one token of each power-up.
var AdPlacementRules = []AdPlacementRule{
	{ID: models.AdPlacementChest, Reward: "chest", DailyCap: 3},
	{ID: models.PowerUpFiftyFifty, Reward: models.PowerUpFiftyFifty, DailyCap: 3},
	{ID: models.PowerUpAddTime, Reward: models.PowerUpAddTime, DailyCap: 3},
	{ID: models.PowerUpSkip, Reward: models.PowerUpSkip, DailyCap: 2},
}

const (
	// AdTotalDailyCap is the most ad rewards per player per UTC day.
	AdTotalDailyCap = 8
	// AdCooldown is the minimum time between two ad requests.
	AdCooldown = 30 * time.Second
	// AdRewardTTL is how long Google's callback is accepted after the request.
	AdRewardTTL = time.Hour
	// TokenCapFallbackCoins is paid instead of a token the player cannot hold.
	TokenCapFallbackCoins = 20
)

// AdChestPrize is one prize of the ad chest, with its odds in parts per million.
type AdChestPrize struct {
	Currency  string
	Amount    int
	WeightPPM int
}

// AdChestPrizes: mostly coins, sometimes a 50:50 token, rarely a few gems
// (gems are what is sold, so ads give very few).
var AdChestPrizes = []AdChestPrize{
	{Currency: models.CurrencyCoins, Amount: 20, WeightPPM: 400_000},
	{Currency: models.CurrencyCoins, Amount: 40, WeightPPM: 300_000},
	{Currency: models.CurrencyCoins, Amount: 60, WeightPPM: 150_000},
	{Currency: models.PowerUpFiftyFifty, Amount: 1, WeightPPM: 100_000},
	{Currency: models.CurrencyGems, Amount: 3, WeightPPM: 50_000},
}

var (
	ssvMu       sync.Mutex
	ssvVerifier *admob.Verifier
	// adsNow is the clock (tests pin it).
	adsNow = func() time.Time { return time.Now().UTC() }
	// adRandom returns a uniform integer in [0, n) (tests replace it).
	adRandom = cryptoIntn
)

func cryptoIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

// SetSSVVerifierForTest installs a verifier with test keys. Tests only.
func SetSSVVerifierForTest(v *admob.Verifier) func() {
	ssvMu.Lock()
	prev := ssvVerifier
	ssvVerifier = v
	ssvMu.Unlock()
	return func() {
		ssvMu.Lock()
		ssvVerifier = prev
		ssvMu.Unlock()
	}
}

func getSSVVerifier() *admob.Verifier {
	ssvMu.Lock()
	defer ssvMu.Unlock()
	if ssvVerifier == nil && config.AppConfig != nil {
		ssvVerifier = admob.NewVerifier(config.AppConfig.Ads.SSVKeysURL)
	}
	return ssvVerifier
}

func adsConfig() config.AdsConfig {
	if config.AppConfig == nil {
		return config.AdsConfig{}
	}
	return config.AppConfig.Ads
}

func adsReady() bool {
	if config.AppConfig == nil {
		return false
	}
	return len(config.AppConfig.Ads.Problems(config.AppConfig.IsProduction())) == 0
}

// AdsUserID is the opaque id the app passes to AdMob as the SSV user id. It is
// an HMAC, so it reveals nothing and cannot be forged for another player.
func AdsUserID(clientID int) (string, error) {
	secret := adsConfig().UserIDSecret
	if len(secret) < 32 {
		return "", errors.New("rewarded ads are not configured")
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("skillverse-ads-user:" + strconv.Itoa(clientID)))
	return hex.EncodeToString(m.Sum(nil)), nil
}

func adPlacement(id string) (AdPlacementRule, bool) {
	for _, p := range AdPlacementRules {
		if p.ID == id {
			return p, true
		}
	}
	return AdPlacementRule{}, false
}

func utcDayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// adRequestPolicy is how the app must request ads for a player. Until the app
// collects and verifies an age band, everyone is treated as under 18 (India's
// DPDP Act counts under-18s as children): non-personalised ads only, content
// rating capped, tagged under the age of consent.
func adRequestPolicy() dto.AdRequestPolicyDTO {
	rating := adsConfig().MaxAdContentRating
	switch rating {
	case "G", "PG", "T", "MA":
	default:
		rating = "G"
	}
	treatment := adsConfig().AgeTreatment
	if treatment != "child" && treatment != "unspecified" {
		treatment = "teen" // the safe default for an unverified age
	}
	return dto.AdRequestPolicyDTO{
		NonPersonalized:              true,
		MaxAdContentRating:           rating,
		AgeTreatment:                 treatment,
		TagForUnderAgeOfConsent:      true,
		TagForChildDirectedTreatment: treatment == "child",
	}
}

// GetAdsConfig tells the app whether rewarded ads are on, which ad units to
// load, how to request them, and what is left today.
func GetAdsConfig(ctx context.Context, clientID int) (*dto.AdsConfigDTO, int, error) {
	now := adsNow()
	premium, err := IsPremium(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read subscription: %w", err)
	}
	counts, err := repo.AdRewardCountsToday(ctx, clientID, utcDayStart(now), now)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read ad rewards: %w", err)
	}
	tokens, err := repo.GetPowerUpTokens(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read power-ups: %w", err)
	}
	ready := adsReady()
	total := 0
	for _, n := range counts {
		total += n
	}
	totalLeft := max(0, AdTotalDailyCap-total)
	cfg := adsConfig()
	out := &dto.AdsConfigDTO{
		RewardedAdsEnabled: ready,
		AdFree:             premium,
		RewardedAdUnitIDs:  map[string]string{"android": cfg.RewardedAdUnitAndroid, "ios": cfg.RewardedAdUnitIOS},
		Policy:             adRequestPolicy(),
		TotalDailyCap:      AdTotalDailyCap,
		RemainingToday:     totalLeft,
		CooldownSeconds:    int(AdCooldown.Seconds()),
	}
	if ready {
		out.SSVUserID, _ = AdsUserID(clientID)
	}
	for _, p := range AdChestPrizes {
		out.ChestOdds = append(out.ChestOdds, dto.ChestOddsDTO{Currency: p.Currency, Amount: p.Amount, Percent: float64(p.WeightPPM) / 10_000})
	}
	for _, p := range AdPlacementRules {
		left := max(0, p.DailyCap-counts[p.ID])
		pl := dto.AdPlacementDTO{ID: p.ID, Reward: p.Reward, DailyCap: p.DailyCap, RemainingToday: left, Available: true}
		switch {
		case !ready && !premium:
			pl.Available, pl.Reason = false, "ads_unavailable"
		case left == 0 || totalLeft == 0:
			pl.Available, pl.Reason = false, "daily_limit"
		case models.IsPowerUp(p.Reward) && tokens[p.Reward] >= MaxPowerUpTokens:
			pl.Available, pl.Reason = false, "token_limit"
		}
		out.Placements = append(out.Placements, pl)
	}
	return out, http.StatusOK, nil
}

// decideAdReward picks what a fulfilled ad pays: an ad chest prize (crypto
// random) or one token. A token the player cannot hold becomes coins.
func decideAdReward(tx *gorm.DB, clientID int, placement string) (string, int, error) {
	currency, amount := placement, 1
	if placement == models.AdPlacementChest {
		roll := adRandom(1_000_000)
		acc := 0
		currency, amount = AdChestPrizes[0].Currency, AdChestPrizes[0].Amount
		for _, p := range AdChestPrizes {
			acc += p.WeightPPM
			if roll < acc {
				currency, amount = p.Currency, p.Amount
				break
			}
		}
	}
	if models.IsPowerUp(currency) {
		held, err := repo.TokenQuantityInTx(tx, clientID, currency)
		if err != nil {
			return "", 0, err
		}
		if held+amount > MaxPowerUpTokens {
			return models.CurrencyCoins, TokenCapFallbackCoins, nil
		}
	}
	return currency, amount, nil
}

func adRewardDTO(row *models.AdReward, now time.Time) *dto.AdRewardDTO {
	out := &dto.AdRewardDTO{
		ID: row.ID, Placement: row.Placement, Status: row.Status,
		AdRequired: row.Source == models.AdSourceAd, ExpiresAt: row.ExpiresAt.UTC(), FulfilledAt: row.FulfilledAt,
	}
	if out.Status == models.AdStatusPending && !now.Before(row.ExpiresAt.UTC()) {
		out.Status = models.AdStatusExpired
	}
	if row.RewardCurrency != nil {
		out.RewardCurrency = *row.RewardCurrency
	}
	if row.RewardAmount != nil {
		out.RewardAmount = *row.RewardAmount
	}
	if out.AdRequired && out.Status == models.AdStatusPending {
		out.SSVCustomData = row.ID
		out.SSVUserID, _ = AdsUserID(row.ClientID)
	}
	return out
}

// CreateAdReward starts an ad reward (or, for Quiz Pass holders, pays it now).
func CreateAdReward(ctx context.Context, clientID int, req *dto.CreateAdRewardRequestDTO) (*dto.AdRewardDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	rule, ok := adPlacement(req.Placement)
	if !ok {
		return nil, http.StatusBadRequest, errors.New("unknown reward")
	}
	premium, err := IsPremium(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read subscription: %w", err)
	}
	if !premium && !adsReady() {
		return nil, http.StatusServiceUnavailable, errors.New("rewarded ads are not available right now")
	}
	tokenCap := 0
	if models.IsPowerUp(rule.Reward) {
		tokenCap = MaxPowerUpTokens
	}
	now := adsNow()
	row, _, err := repo.CreateAdReward(ctx, repo.AdRewardRequest{
		ID: uuid.NewString(), ClientID: clientID, Placement: rule.ID,
		PlacementCap: rule.DailyCap, TotalCap: AdTotalDailyCap, Cooldown: AdCooldown, TTL: AdRewardTTL,
		TokenCap: tokenCap, Instant: premium, Decide: decideAdReward,
		Now: now, DayStartUTC: utcDayStart(now), ReusePending: true,
	})
	switch {
	case errors.Is(err, repo.ErrAdDailyCap):
		return nil, http.StatusTooManyRequests, errors.New("you have collected today's rewards for this; come back tomorrow")
	case errors.Is(err, repo.ErrAdTotalCap):
		return nil, http.StatusTooManyRequests, errors.New("that's all the ad rewards for today; come back tomorrow")
	case errors.Is(err, repo.ErrAdCooldown):
		return nil, http.StatusTooManyRequests, errors.New("please wait a moment before the next ad")
	case errors.Is(err, repo.ErrPowerUpTokenCap):
		return nil, http.StatusConflict, fmt.Errorf("you already hold %d of these; use one first", MaxPowerUpTokens)
	case errors.Is(err, repo.ErrProfileNotFound):
		return nil, http.StatusNotFound, err
	case err != nil:
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to start ad reward: %w", err)
	}
	return adRewardDTO(row, now), http.StatusOK, nil
}

// GetAdRewardStatus returns one of the player's ad rewards (the app polls it).
func GetAdRewardStatus(ctx context.Context, clientID int, id string) (*dto.AdRewardDTO, int, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, http.StatusNotFound, errors.New("reward not found")
	}
	row, err := repo.GetAdReward(ctx, id)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read reward: %w", err)
	}
	if row == nil || row.ClientID != clientID {
		return nil, http.StatusNotFound, errors.New("reward not found")
	}
	return adRewardDTO(row, adsNow()), http.StatusOK, nil
}

// adUnitAllowed checks the callback's ad unit against the configured units.
// AdMob sends only the numeric part after the slash.
func adUnitAllowed(adUnit string) bool {
	adUnit = strings.TrimSpace(adUnit)
	if adUnit == "" {
		return false
	}
	cfg := adsConfig()
	for _, u := range []string{cfg.RewardedAdUnitAndroid, cfg.RewardedAdUnitIOS} {
		if u == "" {
			continue
		}
		if u == adUnit || (strings.Contains(u, "/") && u[strings.LastIndex(u, "/")+1:] == adUnit) {
			return true
		}
	}
	return false
}

// HandleAdMobSSV processes one AdMob server-side verification callback (raw,
// still-encoded query string). Only a callback signed by Google can pay a
// reward. Valid but unusable callbacks (unknown, expired, duplicate) are
// acknowledged with 200 so Google does not retry them.
func HandleAdMobSSV(ctx context.Context, rawQuery string) (int, error) {
	if !adsReady() {
		return http.StatusServiceUnavailable, errors.New("rewarded ads are not configured")
	}
	v := getSSVVerifier()
	cb, err := v.Verify(ctx, rawQuery)
	switch {
	case errors.Is(err, admob.ErrMalformed):
		return http.StatusBadRequest, err
	case errors.Is(err, admob.ErrBadSignature), errors.Is(err, admob.ErrUnknownKey):
		log.Printf("[Ads] SSV rejected: %v", err)
		return http.StatusForbidden, errors.New("signature rejected")
	case err != nil:
		log.Printf("[Ads] SSV verification unavailable: %v", err)
		return http.StatusServiceUnavailable, errors.New("verification unavailable")
	}
	if !adUnitAllowed(cb.AdUnit) {
		log.Printf("[Ads] SSV for unexpected ad unit %q ignored", cb.AdUnit)
		return http.StatusOK, nil
	}
	if _, err := uuid.Parse(cb.CustomData); err != nil {
		// Google's console "verify callback" test and foreign callbacks land here.
		log.Printf("[Ads] SSV without a reward id acknowledged (txn %.12s)", cb.TransactionID)
		return http.StatusOK, nil
	}
	now := adsNow()
	row, outcome, err := repo.FulfillAdReward(ctx, repo.SSVFulfillment{
		ID: cb.CustomData, TransactionID: cb.TransactionID, AdNetwork: cb.AdNetwork, AdUnit: cb.AdUnit,
		KeyID: cb.KeyID, RewardedAt: cb.RewardedAt, Now: now,
		OwnerMatches: func(clientID int) bool {
			want, err := AdsUserID(clientID)
			return err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(cb.UserID)) == 1
		},
		Decide: decideAdReward,
	})
	if err != nil {
		log.Printf("[Ads] SSV fulfilment failed for %s: %v", cb.CustomData, err)
		return http.StatusInternalServerError, errors.New("reward not processed")
	}
	if outcome != repo.SSVFulfilled {
		client := 0
		if row != nil {
			client = row.ClientID
		}
		log.Printf("[Ads] SSV %s for reward %s (client %d, txn %.12s)", outcome, cb.CustomData, client, cb.TransactionID)
	}
	return http.StatusOK, nil
}

// DevCompleteAdReward completes a pending ad reward without Google's callback.
// Local testing only: refused unless ADS_DEV_AUTO_FULFILL=true outside production.
func DevCompleteAdReward(ctx context.Context, clientID int, id string) (*dto.AdRewardDTO, int, error) {
	if !adsConfig().DevAutoFulfill || (config.AppConfig != nil && config.AppConfig.IsProduction()) {
		return nil, http.StatusNotFound, errors.New("not found")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, http.StatusNotFound, errors.New("reward not found")
	}
	row, err := repo.GetAdReward(ctx, id)
	if err != nil || row == nil || row.ClientID != clientID {
		return nil, http.StatusNotFound, errors.New("reward not found")
	}
	now := adsNow()
	if _, _, err := repo.FulfillAdReward(ctx, repo.SSVFulfillment{
		ID: id, TransactionID: "dev-" + id, AdNetwork: "dev", AdUnit: "dev", Now: now, RewardedAt: now,
		OwnerMatches: func(c int) bool { return c == clientID }, Decide: decideAdReward,
	}); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return GetAdRewardStatus(ctx, clientID, id)
}

// StartMonetizationSweeper periodically expires ad rewards nobody finished.
func StartMonetizationSweeper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if config.DB == nil {
					continue
				}
				if n, err := repo.ExpireAdRewards(ctx, adsNow()); err != nil {
					log.Printf("[MonetizationSweeper] expire ad rewards: %v", err)
				} else if n > 0 {
					log.Printf("[MonetizationSweeper] expired %d unfinished ad rewards", n)
				}
			}
		}
	}()
}
