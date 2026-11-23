package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services/admob"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services/playstore"
	"github.com/google/uuid"
)

const testAdsSecret = "test-ads-user-secret-0123456789abcdef"

func (f *billingFixture) setCoins(t *testing.T, clientID, coins int) {
	t.Helper()
	if err := f.db.Exec(`UPDATE clients SET coins = ? WHERE id = ?`, coins, clientID).Error; err != nil {
		t.Fatal(err)
	}
}

func (f *billingFixture) setTokens(t *testing.T, clientID int, powerUp string, n int) {
	t.Helper()
	if err := f.db.Exec(`INSERT INTO client_power_ups (client_id, power_up, quantity) VALUES (?, ?, ?)
		ON CONFLICT (client_id, power_up) DO UPDATE SET quantity = excluded.quantity`, clientID, powerUp, n).Error; err != nil {
		t.Fatal(err)
	}
}

func (f *billingFixture) tokens(t *testing.T, clientID int, powerUp string) int {
	t.Helper()
	return f.count(t, `SELECT COALESCE(SUM(quantity), 0) FROM client_power_ups WHERE client_id = ? AND power_up = ?`, clientID, powerUp)
}

// makePremium gives a player an active Quiz Pass.
func (f *billingFixture) makePremium(t *testing.T, clientID int) {
	t.Helper()
	exp := time.Now().UTC().Add(30 * 24 * time.Hour)
	if err := f.db.Exec(`INSERT INTO client_subscriptions (purchase_token, client_id, product_id, status, expires_at, acknowledged)
		VALUES (?, ?, 'premium', 'active', ?, 1)`, fmt.Sprintf("sub-%d", clientID), clientID, exp).Error; err != nil {
		t.Fatal(err)
	}
}

// ---- Power-up charges ----------------------------------------------------------------

func TestPayForPowerUp_ChargesOnceAndLogs(t *testing.T) {
	f := newBillingFixture(t, false)
	f.setCoins(t, f.alice, 100)

	res, err := PayForPowerUp(context.Background(), f.alice, "sess-1", "q-1", models.PowerUpFiftyFifty, "", "a,c")
	if err != nil || res.AlreadyPaid || res.PaidWith != models.PayWithCoins || res.Amount != 20 || *res.CoinsAfter != 80 {
		t.Fatalf("first charge: %+v %v", res, err)
	}
	// A retry of the same use is free and returns the stored result.
	res, err = PayForPowerUp(context.Background(), f.alice, "sess-1", "q-1", models.PowerUpFiftyFifty, "", "b,d")
	if err != nil || !res.AlreadyPaid || res.Result != "a,c" {
		t.Fatalf("retry: %+v %v", res, err)
	}
	if c, _ := f.balance(t, f.alice); c != 80 {
		t.Errorf("coins = %d, want 80 (charged once)", c)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND direction = 'debit' AND amount = 20 AND balance_after = 80`,
		f.alice, models.TxTypePowerUpCharge); n != 1 {
		t.Errorf("ledger rows = %d, want 1", n)
	}
	// A different question is a new use.
	if res, err := PayForPowerUp(context.Background(), f.alice, "sess-1", "q-2", models.PowerUpFiftyFifty, "", "a,b"); err != nil || res.AlreadyPaid {
		t.Fatalf("second question: %+v %v", res, err)
	}
	if c, _ := f.balance(t, f.alice); c != 60 {
		t.Errorf("coins = %d, want 60", c)
	}
}

func TestPayForPowerUp_InsufficientFundsChangesNothing(t *testing.T) {
	f := newBillingFixture(t, false)
	f.setCoins(t, f.alice, 11)
	_, err := PayForPowerUp(context.Background(), f.alice, "sess", "q", models.PowerUpAddTime, "", "")
	if !errors.Is(err, ErrPowerUpInsufficientFunds) {
		t.Fatalf("err = %v, want insufficient funds", err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM power_up_uses`); n != 0 {
		t.Error("unpaid use was recorded")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger`); n != 0 {
		t.Error("ledger row written for a failed charge")
	}
	// Paying later works (the failed attempt did not burn the once-per-run slot).
	f.setCoins(t, f.alice, 12)
	if _, err := PayForPowerUp(context.Background(), f.alice, "sess", "q", models.PowerUpAddTime, "", ""); err != nil {
		t.Fatalf("after top-up: %v", err)
	}
}

func TestPayForPowerUp_TokensAndPerRunScope(t *testing.T) {
	f := newBillingFixture(t, false)
	f.setCoins(t, f.alice, 100)

	if _, err := PayForPowerUp(context.Background(), f.alice, "s", "q1", models.PowerUpSkip, models.PayWithToken, ""); !errors.Is(err, ErrPowerUpNoToken) {
		t.Fatalf("pay with missing token: %v", err)
	}
	f.setTokens(t, f.alice, models.PowerUpSkip, 2)
	res, err := PayForPowerUp(context.Background(), f.alice, "s", "q1", models.PowerUpSkip, "", "")
	if err != nil || res.PaidWith != models.PayWithToken || *res.TokensAfter != 1 {
		t.Fatalf("auto should use a token: %+v %v", res, err)
	}
	if c, _ := f.balance(t, f.alice); c != 100 {
		t.Errorf("coins changed when a token paid: %d", c)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE transaction_type = ? AND currency = ? AND balance_after = 1`,
		models.TxTypePowerUpTokenUse, models.PowerUpSkip); n != 1 {
		t.Errorf("token spend not logged in the ledger")
	}
	// Skip is once per run: another question of the same run is the same use.
	res, err = PayForPowerUp(context.Background(), f.alice, "s", "q2", models.PowerUpSkip, "", "")
	if err != nil || !res.AlreadyPaid {
		t.Fatalf("per-run power-up charged twice: %+v %v", res, err)
	}
	if f.tokens(t, f.alice, models.PowerUpSkip) != 1 {
		t.Error("second token spent")
	}
	// Coins explicitly, even with a token left.
	res, err = PayForPowerUp(context.Background(), f.alice, "s2", "q1", models.PowerUpSkip, models.PayWithCoins, "")
	if err != nil || res.PaidWith != models.PayWithCoins || *res.CoinsAfter != 65 {
		t.Fatalf("pay with coins: %+v %v", res, err)
	}
}

func TestPayForPowerUp_ConcurrentRetriesChargeOnce(t *testing.T) {
	f := newBillingFixture(t, false)
	f.setCoins(t, f.alice, 1000)
	var wg sync.WaitGroup
	charged := 0
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := PayForPowerUp(context.Background(), f.alice, "race", "q", models.PowerUpFiftyFifty, "", "a,b")
			if err == nil && !res.AlreadyPaid {
				mu.Lock()
				charged++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if charged != 1 {
		t.Errorf("charged %d times, want 1", charged)
	}
	if c, _ := f.balance(t, f.alice); c != 980 {
		t.Errorf("coins = %d, want 980", c)
	}
}

// ---- 50:50 in an MCQ game -------------------------------------------------------------

func cacheMCQSession(t *testing.T, clientID int, id string) {
	t.Helper()
	s := &models.GameSession{ID: id, ClientID: clientID, TopicID: "accounts", GameMode: models.GameModeMCQ,
		Status: models.SessionStatusInProgress, TotalQuestions: 1, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.SetQuestions([]models.SessionQuestion{{QuestionCode: "acc11-01-mcq-0001", Prompt: "?",
		OptionA: "w1", OptionB: "right", OptionC: "w2", OptionD: "w3", CorrectOption: "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CacheSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.InvalidateCachedSession(context.Background(), id) })
}

func TestApplyFiftyFifty_ChargedOnServerAndStable(t *testing.T) {
	f := newBillingFixture(t, false)
	f.setCoins(t, f.alice, 30)
	sid := uuid.NewString()
	cacheMCQSession(t, f.alice, sid)
	req := func() (*dto.FiftyFiftyResponseDTO, int, error) {
		return ApplyFiftyFifty(context.Background(), f.alice, &dto.FiftyFiftyRequestDTO{Session: sid, Question: "acc11-01-mcq-0001"})
	}
	res, status, err := req()
	if err != nil || status != http.StatusOK || len(res.HiddenOptions) != 2 || res.Cost != 20 {
		t.Fatalf("%+v %d %v", res, status, err)
	}
	for _, h := range res.HiddenOptions {
		if h == "b" {
			t.Fatal("hid the correct option")
		}
	}
	again, _, err := req()
	if err != nil || !again.AlreadyApplied || strings.Join(again.HiddenOptions, ",") != strings.Join(res.HiddenOptions, ",") {
		t.Fatalf("retry changed the result or charged: %+v %v", again, err)
	}
	if c, _ := f.balance(t, f.alice); c != 10 {
		t.Errorf("coins = %d, want 10", c)
	}
	// Not this session's question, someone else's session, not enough coins.
	if _, status, _ := ApplyFiftyFifty(context.Background(), f.alice, &dto.FiftyFiftyRequestDTO{Session: sid, Question: "other-q"}); status != http.StatusNotFound {
		t.Errorf("foreign question: %d, want 404", status)
	}
	if _, status, _ := ApplyFiftyFifty(context.Background(), f.bob, &dto.FiftyFiftyRequestDTO{Session: sid, Question: "acc11-01-mcq-0001"}); status != http.StatusForbidden {
		t.Errorf("other player's session: %d, want 403", status)
	}
	sid2 := uuid.NewString()
	cacheMCQSession(t, f.alice, sid2)
	if _, status, _ := ApplyFiftyFifty(context.Background(), f.alice, &dto.FiftyFiftyRequestDTO{Session: sid2, Question: "acc11-01-mcq-0001"}); status != http.StatusPaymentRequired {
		t.Errorf("without coins: %d, want 402", status)
	}
}

// ---- Rewarded ads -------------------------------------------------------------------------

type adsFixture struct {
	*billingFixture
	key *ecdsa.PrivateKey
	now time.Time
}

func newAdsFixture(t *testing.T) *adsFixture {
	t.Helper()
	f := newBillingFixture(t, false)
	prevCfg := config.AppConfig
	config.AppConfig = &config.Config{Server: config.ServerConfig{Env: "test"}, Ads: config.AdsConfig{
		Enabled: true, UserIDSecret: testAdsSecret,
		RewardedAdUnitAndroid: "ca-app-pub-1111111111111111/2222222222", MaxAdContentRating: "G",
	}}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	restoreVerifier := SetSSVVerifierForTest(admob.NewVerifierWithKeys(map[int64]*ecdsa.PublicKey{3335741209: &key.PublicKey}))
	a := &adsFixture{billingFixture: f, key: key, now: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)}
	prevNow, prevRand := adsNow, adRandom
	adsNow = func() time.Time { return a.now }
	adRandom = func(int) int { return 0 } // ad chest: first prize (20 coins)
	t.Cleanup(func() {
		config.AppConfig = prevCfg
		restoreVerifier()
		adsNow, adRandom = prevNow, prevRand
	})
	return a
}

// ssvQuery builds an AdMob SSV query string signed with the fixture's key.
func (a *adsFixture) ssvQuery(t *testing.T, rewardID, userID, txn, adUnit string) string {
	t.Helper()
	q := "ad_network=5450213213286189855&ad_unit=" + adUnit + "&custom_data=" + url.QueryEscape(rewardID) +
		"&reward_amount=1&reward_item=Reward&timestamp=" + fmt.Sprint(a.now.UnixMilli()) +
		"&transaction_id=" + txn + "&user_id=" + userID
	digest := sha256.Sum256([]byte(q))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return q + "&signature=" + base64.RawURLEncoding.EncodeToString(sig) + "&key_id=3335741209"
}

func (a *adsFixture) start(t *testing.T, clientID int, placement string) *dto.AdRewardDTO {
	t.Helper()
	res, status, err := CreateAdReward(context.Background(), clientID, &dto.CreateAdRewardRequestDTO{Placement: placement})
	if err != nil || status != http.StatusOK {
		t.Fatalf("start %s: %d %v", placement, status, err)
	}
	return res
}

func TestAdReward_PaidOnlyForGoogleSignedCallbackOnce(t *testing.T) {
	a := newAdsFixture(t)
	coins, _ := a.balance(t, a.alice)
	r := a.start(t, a.alice, models.AdPlacementChest)
	if r.Status != models.AdStatusPending || !r.AdRequired || r.SSVCustomData != r.ID || r.SSVUserID == "" {
		t.Fatalf("new reward: %+v", r)
	}
	userID, _ := AdsUserID(a.alice)

	// A forged callback (signature over different data) pays nothing.
	forged := strings.Replace(a.ssvQuery(t, r.ID, userID, "txn-1", "2222222222"), "reward_amount=1", "reward_amount=9", 1)
	if status, _ := HandleAdMobSSV(context.Background(), forged); status != http.StatusForbidden {
		t.Errorf("forged callback: %d, want 403", status)
	}
	if c, _ := a.balance(t, a.alice); c != coins {
		t.Fatal("forged callback paid")
	}

	q := a.ssvQuery(t, r.ID, userID, "txn-1", "2222222222")
	for i := 0; i < 2; i++ { // Google may deliver twice
		if status, err := HandleAdMobSSV(context.Background(), q); status != http.StatusOK || err != nil {
			t.Fatalf("callback %d: %d %v", i, status, err)
		}
	}
	if c, _ := a.balance(t, a.alice); c != coins+20 {
		t.Errorf("coins = %d, want %d (paid once)", c, coins+20)
	}
	got, _, _ := GetAdRewardStatus(context.Background(), a.alice, r.ID)
	if got.Status != models.AdStatusFulfilled || got.RewardCurrency != models.CurrencyCoins || got.RewardAmount != 20 {
		t.Errorf("reward after callback: %+v", got)
	}
	if n := a.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND reference_id = ?`,
		a.alice, models.TxTypeAdReward, "ad:"+r.ID); n != 1 {
		t.Errorf("ad reward ledger rows = %d, want 1", n)
	}
	if n := a.count(t, `SELECT COUNT(*) FROM ad_rewards WHERE id = ? AND ssv_transaction_id = 'txn-1' AND ad_unit = '2222222222'`, r.ID); n != 1 {
		t.Error("callback details not stored on the reward row")
	}
	// Bob cannot read Alice's reward.
	if _, status, _ := GetAdRewardStatus(context.Background(), a.bob, r.ID); status != http.StatusNotFound {
		t.Errorf("other player's reward: %d, want 404", status)
	}
}

func TestAdReward_CallbackChecks(t *testing.T) {
	a := newAdsFixture(t)
	coins, _ := a.balance(t, a.alice)
	r := a.start(t, a.alice, models.AdPlacementChest)
	bobID, _ := AdsUserID(a.bob)
	aliceID, _ := AdsUserID(a.alice)

	// Signed, but for another player's user id: not paid.
	if status, _ := HandleAdMobSSV(context.Background(), a.ssvQuery(t, r.ID, bobID, "t-wrong-user", "2222222222")); status != http.StatusOK {
		t.Errorf("wrong user status %d", status)
	}
	// Signed, but from an ad unit that is not ours: not paid.
	_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, r.ID, aliceID, "t-unit", "9999999999"))
	// Signed, but no reward id (Google's console test): acknowledged.
	if status, _ := HandleAdMobSSV(context.Background(), a.ssvQuery(t, "", aliceID, "t-test", "2222222222")); status != http.StatusOK {
		t.Errorf("console test callback: %d, want 200", status)
	}
	if c, _ := a.balance(t, a.alice); c != coins {
		t.Fatalf("a rejected callback paid: %d -> %d", coins, c)
	}
	// After expiry nothing is paid.
	a.now = a.now.Add(AdRewardTTL + time.Minute)
	_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, r.ID, aliceID, "t-late", "2222222222"))
	if c, _ := a.balance(t, a.alice); c != coins {
		t.Error("expired reward paid")
	}
	if got, _, _ := GetAdRewardStatus(context.Background(), a.alice, r.ID); got.Status != models.AdStatusExpired {
		t.Errorf("status = %s, want expired", got.Status)
	}
	// One Google transaction cannot pay two rewards.
	r2 := a.start(t, a.alice, models.AdPlacementChest)
	a.now = a.now.Add(AdCooldown + time.Second)
	r3 := a.start(t, a.alice, models.PowerUpFiftyFifty)
	_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, r2.ID, aliceID, "same-txn", "2222222222"))
	_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, r3.ID, aliceID, "same-txn", "2222222222"))
	if n := a.count(t, `SELECT COUNT(*) FROM ad_rewards WHERE status = 'fulfilled'`); n != 1 {
		t.Errorf("fulfilled rewards = %d, want 1 (transaction id reused)", n)
	}
}

func TestAdReward_CapsCooldownAndReuse(t *testing.T) {
	a := newAdsFixture(t)
	aliceID, _ := AdsUserID(a.alice)
	first := a.start(t, a.alice, models.AdPlacementChest)
	// Tapping again before watching reuses the same request (no new row, no cooldown error).
	if again := a.start(t, a.alice, models.AdPlacementChest); again.ID != first.ID {
		t.Errorf("pending request not reused")
	}
	// Another placement right away hits the cooldown.
	if _, status, _ := CreateAdReward(context.Background(), a.alice, &dto.CreateAdRewardRequestDTO{Placement: models.PowerUpAddTime}); status != http.StatusTooManyRequests {
		t.Errorf("cooldown: %d, want 429", status)
	}
	// Watch three chests (the daily cap), each paid by a callback.
	id := first.ID
	for i := 0; i < 3; i++ {
		if i > 0 {
			a.now = a.now.Add(AdCooldown + time.Second)
			id = a.start(t, a.alice, models.AdPlacementChest).ID
		}
		_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, id, aliceID, fmt.Sprintf("cap-%d", i), "2222222222"))
	}
	a.now = a.now.Add(AdCooldown + time.Second)
	if _, status, _ := CreateAdReward(context.Background(), a.alice, &dto.CreateAdRewardRequestDTO{Placement: models.AdPlacementChest}); status != http.StatusTooManyRequests {
		t.Errorf("fourth chest today: %d, want 429", status)
	}
	cfg, _, err := GetAdsConfig(context.Background(), a.alice)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Placements {
		if p.ID == models.AdPlacementChest && (p.Available || p.RemainingToday != 0 || p.Reason != "daily_limit") {
			t.Errorf("chest placement after cap: %+v", p)
		}
	}
	if !cfg.Policy.NonPersonalized || cfg.Policy.MaxAdContentRating != "G" || !cfg.Policy.TagForUnderAgeOfConsent || cfg.Policy.AgeTreatment != "teen" {
		t.Errorf("ad policy must default to under-18 settings: %+v", cfg.Policy)
	}
	// A new UTC day resets the caps.
	a.now = a.now.Add(24 * time.Hour)
	a.start(t, a.alice, models.AdPlacementChest)
	// Unknown placements are refused.
	if _, status, _ := CreateAdReward(context.Background(), a.alice, &dto.CreateAdRewardRequestDTO{Placement: "free_gems"}); status != http.StatusBadRequest {
		t.Errorf("unknown placement: %d, want 400", status)
	}
}

func TestAdReward_TokenRewardsAndCap(t *testing.T) {
	a := newAdsFixture(t)
	aliceID, _ := AdsUserID(a.alice)
	r := a.start(t, a.alice, models.PowerUpAddTime)
	_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, r.ID, aliceID, "tok-1", "2222222222"))
	if n := a.tokens(t, a.alice, models.PowerUpAddTime); n != 1 {
		t.Fatalf("add_time tokens = %d, want 1", n)
	}
	if n := a.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE transaction_type = ? AND currency = ? AND direction = 'credit' AND balance_after = 1`,
		models.TxTypeAdReward, models.PowerUpAddTime); n != 1 {
		t.Error("token grant not logged")
	}
	// Holding the maximum: no more token ads offered.
	a.setTokens(t, a.alice, models.PowerUpSkip, MaxPowerUpTokens)
	a.now = a.now.Add(AdCooldown + time.Second)
	if _, status, _ := CreateAdReward(context.Background(), a.alice, &dto.CreateAdRewardRequestDTO{Placement: models.PowerUpSkip}); status != http.StatusConflict {
		t.Errorf("token cap: %d, want 409", status)
	}
	// Reaching the cap between request and callback pays coins instead.
	r2 := a.start(t, a.alice, models.PowerUpFiftyFifty)
	a.setTokens(t, a.alice, models.PowerUpFiftyFifty, MaxPowerUpTokens)
	coins, _ := a.balance(t, a.alice)
	_, _ = HandleAdMobSSV(context.Background(), a.ssvQuery(t, r2.ID, aliceID, "tok-2", "2222222222"))
	if c, _ := a.balance(t, a.alice); c != coins+TokenCapFallbackCoins {
		t.Errorf("fallback coins: %d -> %d", coins, c)
	}
	if a.tokens(t, a.alice, models.PowerUpFiftyFifty) != MaxPowerUpTokens {
		t.Error("token cap exceeded")
	}
}

func TestAdReward_QuizPassGetsRewardsWithoutAds(t *testing.T) {
	a := newAdsFixture(t)
	a.makePremium(t, a.alice)
	coins, _ := a.balance(t, a.alice)
	r := a.start(t, a.alice, models.AdPlacementChest)
	if r.AdRequired || r.Status != models.AdStatusFulfilled || r.RewardAmount != 20 {
		t.Fatalf("pass reward: %+v", r)
	}
	if c, _ := a.balance(t, a.alice); c != coins+20 {
		t.Errorf("coins = %d, want %d", c, coins+20)
	}
	if n := a.count(t, `SELECT COUNT(*) FROM ad_rewards WHERE id = ? AND source = 'pass'`, r.ID); n != 1 {
		t.Error("pass claim not recorded in ad_rewards")
	}
	// Same daily caps apply, without any cooldown.
	a.start(t, a.alice, models.AdPlacementChest)
	a.start(t, a.alice, models.AdPlacementChest)
	if _, status, _ := CreateAdReward(context.Background(), a.alice, &dto.CreateAdRewardRequestDTO{Placement: models.AdPlacementChest}); status != http.StatusTooManyRequests {
		t.Errorf("pass cap: %d, want 429", status)
	}
	cfg, _, _ := GetAdsConfig(context.Background(), a.alice)
	if !cfg.AdFree {
		t.Error("pass holder should be ad-free")
	}
}

func TestAdReward_DisabledAndDevCompletion(t *testing.T) {
	a := newAdsFixture(t)
	r := a.start(t, a.alice, models.AdPlacementChest)
	if _, status, _ := DevCompleteAdReward(context.Background(), a.alice, r.ID); status != http.StatusNotFound {
		t.Errorf("dev completion must be off by default: %d", status)
	}
	config.AppConfig.Ads.DevAutoFulfill = true
	if got, status, err := DevCompleteAdReward(context.Background(), a.alice, r.ID); err != nil || status != http.StatusOK || got.Status != models.AdStatusFulfilled {
		t.Errorf("dev completion: %+v %d %v", got, status, err)
	}
	config.AppConfig.Server.Env = "production"
	r2 := a.billingFixture // keep fixture alive
	_ = r2
	if _, status, _ := DevCompleteAdReward(context.Background(), a.alice, r.ID); status != http.StatusNotFound {
		t.Errorf("dev completion in production: %d, want 404", status)
	}
	config.AppConfig.Server.Env = "test"
	config.AppConfig.Ads.Enabled = false
	if _, status, _ := CreateAdReward(context.Background(), a.bob, &dto.CreateAdRewardRequestDTO{Placement: models.AdPlacementChest}); status != http.StatusServiceUnavailable {
		t.Errorf("kill switch: %d, want 503", status)
	}
	if status, _ := HandleAdMobSSV(context.Background(), "a=b"); status != http.StatusServiceUnavailable {
		t.Errorf("callback while disabled: %d, want 503", status)
	}
}

// ---- Topic packs -------------------------------------------------------------------------

func (f *billingFixture) addPack(t *testing.T, productID string, subjects ...string) {
	t.Helper()
	if err := f.db.Exec(`INSERT INTO billing_products (product_id, kind, title, active, sort_order) VALUES (?, 'non_consumable', 'Pack', 1, 20)`, productID).Error; err != nil {
		t.Fatal(err)
	}
	for _, s := range subjects {
		if err := f.db.Exec(`INSERT INTO billing_product_subjects (product_id, subject) VALUES (?, ?)`, productID, s).Error; err != nil {
			t.Fatal(err)
		}
	}
	InvalidateTopicPackCache()
}

func TestTopicPack_LockedUntilBoughtAndAfterRefund(t *testing.T) {
	f := newBillingFixture(t, false)
	t.Cleanup(InvalidateTopicPackCache)
	f.addPack(t, "pack_gk", "gk_world", "gk_india")
	f.addPack(t, "pack_empty")
	ctx := context.Background()

	if err := CheckTopicAccess(ctx, f.alice, "accounts"); err != nil {
		t.Fatalf("free subject locked: %v", err)
	}
	err := CheckTopicAccess(ctx, f.alice, "gk_world")
	if !errors.Is(err, ErrTopicLocked) || TopicAccessStatus(err) != http.StatusPaymentRequired {
		t.Fatalf("pack subject before purchase: %v", err)
	}
	subjects := []dto.SubjectDTO{{TopicID: "accounts"}, {TopicID: "gk_world"}}
	if err := AnnotateSubjectAccess(ctx, f.alice, subjects); err != nil || subjects[0].Locked || !subjects[1].Locked || subjects[1].PackProductIDs[0] != "pack_gk" {
		t.Fatalf("annotate: %+v %v", subjects, err)
	}
	products, _, _ := ListBillingProducts(ctx, f.alice)
	var listed []string
	for _, p := range products {
		listed = append(listed, p.ProductID)
		if p.ProductID == "pack_gk" && (len(p.Subjects) != 2 || p.Owned) {
			t.Errorf("pack listing: %+v", p)
		}
	}
	if strings.Contains(strings.Join(listed, ","), "pack_empty") {
		t.Error("an empty pack is for sale")
	}

	// Buy it: verified with Google, acknowledged (not consumed), unlocks both subjects.
	f.paidPack("tok-pack", "pack_gk", f.alice)
	res, status, err := verify(f.alice, "pack_gk", "tok-pack")
	if err != nil || status != http.StatusOK || strings.Join(res.UnlockedSubjects, ",") != "gk_india,gk_world" {
		t.Fatalf("buy pack: %+v %d %v", res, status, err)
	}
	if f.play.acks != 1 || f.play.consumes != 0 {
		t.Errorf("pack must be acknowledged, not consumed (acks %d, consumes %d)", f.play.acks, f.play.consumes)
	}
	if err := CheckTopicAccess(ctx, f.alice, "gk_india"); err != nil {
		t.Fatalf("owned pack still locked: %v", err)
	}
	if err := CheckTopicAccess(ctx, f.bob, "gk_india"); !errors.Is(err, ErrTopicLocked) {
		t.Error("another player got the pack")
	}
	ent, _, _ := GetEntitlements(ctx, f.alice)
	if len(ent.TopicPacks) != 1 || ent.TopicPacks[0].ProductID != "pack_gk" {
		t.Errorf("entitlements: %+v", ent)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM billing_audit WHERE client_id = ? AND action = 'grant' AND result = 'ok'`, f.alice); n != 1 {
		t.Errorf("grant audit rows = %d, want 1 (written with the grant)", n)
	}

	// Refund: access ends.
	if err := revokePurchase(ctx, "tok-pack", "test"); err != nil {
		t.Fatal(err)
	}
	if err := CheckTopicAccess(ctx, f.alice, "gk_world"); !errors.Is(err, ErrTopicLocked) {
		t.Error("refunded pack still unlocks")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM billing_audit WHERE client_id = ? AND action = 'refund' AND result = 'ok'`, f.alice); n != 1 {
		t.Errorf("refund audit rows = %d, want 1", n)
	}
}

func TestTopicPack_QuantityAndAcknowledgedElsewhere(t *testing.T) {
	f := newBillingFixture(t, false)
	t.Cleanup(InvalidateTopicPackCache)
	f.addPack(t, "pack_sci", "science_basics")
	f.paidPack("tok-q", "pack_sci", f.alice).Quantity = 2
	if _, status, _ := verify(f.alice, "pack_sci", "tok-q"); status != http.StatusBadRequest {
		t.Errorf("pack with quantity 2: %d, want 400", status)
	}
	f.paidPack("tok-ack", "pack_sci", f.alice).Acknowledged = true
	if _, status, _ := verify(f.alice, "pack_sci", "tok-ack"); status != http.StatusConflict {
		t.Errorf("acknowledged elsewhere: %d, want 409", status)
	}
}

func TestSweeper_AcknowledgesPacksAndConsumesGems(t *testing.T) {
	f := newBillingFixture(t, false)
	t.Cleanup(InvalidateTopicPackCache)
	f.addPack(t, "pack_h", "history_1")
	f.play.consumeErr = errors.New("google down")
	f.paidPack("tok-h", "pack_h", f.alice)
	f.paidPack("tok-g", "gems_50", f.alice)
	_, _, _ = verify(f.alice, "pack_h", "tok-h")
	_, _, _ = verify(f.alice, "gems_50", "tok-g")
	f.play.consumeErr = nil
	f.db.Exec(`UPDATE play_purchases SET created_at = ?`, time.Now().UTC().Add(-10*time.Minute))
	RunBillingSweep(context.Background())
	if f.play.acks != 1 || f.play.consumes != 1 {
		t.Errorf("sweeper: acks %d (want 1), consumes %d (want 1)", f.play.acks, f.play.consumes)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM play_purchases WHERE consumed = 0`); n != 0 {
		t.Errorf("%d purchases left unconfirmed", n)
	}
}

// ---- Quiz Pass perks -------------------------------------------------------------------------

func TestPassMonthlyGems_OncePerMonthForPassHolders(t *testing.T) {
	f := newBillingFixture(t, false)
	ctx := context.Background()
	if _, status, _ := ClaimPassMonthlyGems(ctx, f.alice); status != http.StatusPaymentRequired {
		t.Errorf("without pass: %d, want 402", status)
	}
	f.makePremium(t, f.alice)
	_, gems := f.balance(t, f.alice)
	res, status, err := ClaimPassMonthlyGems(ctx, f.alice)
	if err != nil || status != http.StatusOK || res.GemsAdded != PassMonthlyGems || res.Gems != gems+PassMonthlyGems {
		t.Fatalf("claim: %+v %d %v", res, status, err)
	}
	if _, status, _ := ClaimPassMonthlyGems(ctx, f.alice); status != http.StatusConflict {
		t.Errorf("second claim: %d, want 409", status)
	}
	ent, _, _ := GetEntitlements(ctx, f.alice)
	if !ent.Perks.MonthlyGemsClaimed || !ent.Perks.AdFreeRewards || ent.Perks.ExtraStreakFreezeSlots != PassExtraFreezeSlots {
		t.Errorf("perks: %+v", ent.Perks)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = ?`, f.alice, models.TxTypePassReward); n != 1 {
		t.Errorf("pass ledger rows = %d, want 1", n)
	}
}

func TestQuizPass_ExtraStreakFreezeSlot(t *testing.T) {
	f := newBillingFixture(t, false)
	ctx := context.Background()
	f.db.Exec(`UPDATE clients SET gems = 100, streak_freezes = ? WHERE id IN (?, ?)`, MaxStreakFreezes, f.alice, f.bob)
	if _, status, _ := BuyStreakFreeze(ctx, f.bob); status != http.StatusConflict {
		t.Errorf("without pass at max: %d, want 409", status)
	}
	f.makePremium(t, f.alice)
	if res, status, err := BuyStreakFreeze(ctx, f.alice); err != nil || status != http.StatusOK || res.Freezes != MaxStreakFreezes+1 {
		t.Errorf("pass extra slot: %+v %d %v", res, status, err)
	}
}

// ---- Production secrets ------------------------------------------------------------------------

func TestProductionSecretProblems(t *testing.T) {
	c := &config.Config{Server: config.ServerConfig{JWTSecret: "super_secret_jwt_key_quizapp_2026", PasswordSecret: "super_secret_pepper_salt"}}
	fatal, warnings := c.ProductionSecretProblems()
	if len(fatal) != 1 || len(warnings) != 1 {
		t.Errorf("public defaults: fatal %v warnings %v", fatal, warnings)
	}
	c.Server.JWTSecret = strings.Repeat("k", 40)
	c.Server.PasswordSecret = "private-pepper"
	if fatal, warnings := c.ProductionSecretProblems(); len(fatal) != 0 || len(warnings) != 0 {
		t.Errorf("private secrets: fatal %v warnings %v", fatal, warnings)
	}
}

var _ = playstore.PurchaseStatePurchased

func TestAdReward_ConcurrentDuplicateCallbacksPayOnce(t *testing.T) {
	a := newAdsFixture(t)
	coins, _ := a.balance(t, a.alice)
	r := a.start(t, a.alice, models.AdPlacementChest)
	userID, _ := AdsUserID(a.alice)
	q := a.ssvQuery(t, r.ID, userID, "txn-race", "2222222222")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = HandleAdMobSSV(context.Background(), q)
		}()
	}
	wg.Wait()
	if c, _ := a.balance(t, a.alice); c != coins+20 {
		t.Errorf("coins = %d, want %d (one payout)", c, coins+20)
	}
	if n := a.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE transaction_type = ?`, models.TxTypeAdReward); n != 1 {
		t.Errorf("ad reward ledger rows = %d, want 1", n)
	}
}
