package dto

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// ---- Rewarded ads ------------------------------------------------------------------

// AdRequestPolicyDTO tells the app how to request ads for this player. The
// server decides it: until a player is confirmed adult, ads are
// non-personalised with a G content rating and tagged under the age of consent.
type AdRequestPolicyDTO struct {
	NonPersonalized    bool   `json:"non_personalized"`
	MaxAdContentRating string `json:"max_ad_content_rating"`
	// AgeTreatment is the AdMob age-restricted treatment: "teen" (under 18,
	// the default until age is verified), "child" (under 13) or "unspecified".
	AgeTreatment                 string `json:"age_treatment"`
	TagForUnderAgeOfConsent      bool   `json:"tag_for_under_age_of_consent"`
	TagForChildDirectedTreatment bool   `json:"tag_for_child_directed_treatment"`
}

// AdPlacementDTO is one thing a player can get for watching an ad.
type AdPlacementDTO struct {
	ID             string `json:"id"`
	Reward         string `json:"reward"` // "chest" or a power-up token id
	DailyCap       int    `json:"daily_cap"`
	RemainingToday int    `json:"remaining_today"`
	Available      bool   `json:"available"`
	Reason         string `json:"reason,omitempty"` // why not available
}

// ChestOddsDTO is one ad chest prize and its chance, shown before opening.
type ChestOddsDTO struct {
	Currency string  `json:"currency"`
	Amount   int     `json:"amount"`
	Percent  float64 `json:"percent"`
}

// AdsConfigDTO is GET /ads/config.
type AdsConfigDTO struct {
	RewardedAdsEnabled bool               `json:"rewarded_ads_enabled"`
	AdFree             bool               `json:"ad_free"` // Quiz Pass: rewards without watching
	RewardedAdUnitIDs  map[string]string  `json:"rewarded_ad_unit_ids"`
	SSVUserID          string             `json:"ssv_user_id"`
	Policy             AdRequestPolicyDTO `json:"policy"`
	Placements         []AdPlacementDTO   `json:"placements"`
	TotalDailyCap      int                `json:"total_daily_cap"`
	RemainingToday     int                `json:"remaining_today"`
	CooldownSeconds    int                `json:"cooldown_seconds"`
	// ChestOdds are the ad chest's prizes and chances, disclosed before opening.
	ChestOdds []ChestOddsDTO `json:"chest_odds"`
}

var adPlacementPattern = regexp.MustCompile(`^[a-z_]{1,32}$`)

// CreateAdRewardRequestDTO asks to watch an ad (or, with the Quiz Pass, to
// claim the reward directly).
type CreateAdRewardRequestDTO struct {
	Placement string `json:"placement"`
}

// Validate checks the placement's shape (the service checks it exists).
func (r *CreateAdRewardRequestDTO) Validate() error {
	r.Placement = strings.ToLower(strings.TrimSpace(r.Placement))
	if !adPlacementPattern.MatchString(r.Placement) {
		return errors.New("placement is invalid")
	}
	return nil
}

// AdRewardDTO is one ad reward and its state.
type AdRewardDTO struct {
	ID             string     `json:"id"`
	Placement      string     `json:"placement"`
	Status         string     `json:"status"` // pending, fulfilled, expired
	AdRequired     bool       `json:"ad_required"`
	SSVCustomData  string     `json:"ssv_custom_data,omitempty"`
	SSVUserID      string     `json:"ssv_user_id,omitempty"`
	RewardCurrency string     `json:"reward_currency,omitempty"`
	RewardAmount   int        `json:"reward_amount,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
	FulfilledAt    *time.Time `json:"fulfilled_at,omitempty"`
}

// ---- Power-ups ---------------------------------------------------------------------

// PowerUpPriceDTO is what one power-up costs and where it can be used.
type PowerUpPriceDTO struct {
	ID         string `json:"id"`
	Mode       string `json:"mode"`
	PriceCoins int    `json:"price_coins"`
	Limit      string `json:"limit"` // "per_question" or "per_run"
	Tokens     int    `json:"tokens"`
}

// PowerUpsStatusDTO is GET /power-ups.
type PowerUpsStatusDTO struct {
	PowerUps  []PowerUpPriceDTO `json:"power_ups"`
	MaxTokens int               `json:"max_tokens"`
	Coins     int               `json:"coins"`
}

// ---- Entitlements and the Quiz Pass ---------------------------------------------------

// TopicPackDTO is one owned topic pack.
type TopicPackDTO struct {
	ProductID string   `json:"product_id"`
	Subjects  []string `json:"subjects"`
}

// PassPerksDTO lists the Quiz Pass perks and their state for this player.
type PassPerksDTO struct {
	AdFreeRewards          bool `json:"ad_free_rewards"`
	MonthlyGems            int  `json:"monthly_gems"`
	MonthlyGemsClaimed     bool `json:"monthly_gems_claimed"`
	ExtraStreakFreezeSlots int  `json:"extra_streak_freeze_slots"`
}

// EntitlementsDTO is GET /billing/entitlements: everything the player has
// paid for, computed on the server.
type EntitlementsDTO struct {
	Subscription SubscriptionStatusDTO `json:"subscription"`
	TopicPacks   []TopicPackDTO        `json:"topic_packs"`
	Perks        PassPerksDTO          `json:"perks"`
}

// PassClaimResponseDTO is POST /pass/monthly-gems.
type PassClaimResponseDTO struct {
	GemsAdded int    `json:"gems_added"`
	Gems      int    `json:"gems"`
	Period    string `json:"period"`
}
