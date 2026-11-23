package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services/playstore"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"google.golang.org/api/idtoken"
	"gorm.io/gorm"
)

const testBillingSecret = "test-billing-secret-0123456789abcdef"

// fakePlay stands in for the Google Play Developer API.
type fakePlay struct {
	mu         sync.Mutex
	products   map[string]*playstore.ProductPurchase // by token
	subs       map[string]*playstore.SubscriptionPurchase
	voided     []playstore.VoidedPurchase
	lookupErr  error // returned by Get* (Google unreachable)
	consumeErr error
	consumes   int
	acks       int
}

func newFakePlay() *fakePlay {
	return &fakePlay{products: map[string]*playstore.ProductPurchase{}, subs: map[string]*playstore.SubscriptionPurchase{}}
}

func (f *fakePlay) GetProduct(_ context.Context, productID, token string) (*playstore.ProductPurchase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	p, ok := f.products[token]
	if !ok || (p.ProductID != "" && p.ProductID != productID && productID != "") {
		return nil, playstore.ErrInvalidToken
	}
	c := *p
	return &c, nil
}

func (f *fakePlay) ConsumeProduct(_ context.Context, _, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.consumeErr != nil {
		return f.consumeErr
	}
	f.consumes++
	if p, ok := f.products[token]; ok {
		p.Consumed = true
	}
	return nil
}

func (f *fakePlay) GetSubscription(_ context.Context, token string) (*playstore.SubscriptionPurchase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	s, ok := f.subs[token]
	if !ok {
		return nil, playstore.ErrInvalidToken
	}
	c := *s
	return &c, nil
}

func (f *fakePlay) AcknowledgeSubscription(_ context.Context, _, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks++
	if s, ok := f.subs[token]; ok {
		s.Acknowledged = true
	}
	return nil
}

func (f *fakePlay) ListVoided(context.Context, time.Time) ([]playstore.VoidedPurchase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]playstore.VoidedPurchase(nil), f.voided...), nil
}

// billingFixture: a database with the billing tables and catalog, the fake Play
// client installed, and two players.
type billingFixture struct {
	db         *gorm.DB
	play       *fakePlay
	alice, bob int
}

func newBillingFixture(t *testing.T, allowTest bool) *billingFixture {
	t.Helper()
	db := testutil.NewDB(t)
	testutil.AddBillingTables(t, db)
	for _, q := range []string{
		`INSERT INTO billing_products (product_id, kind, title, coins, gems, active, sort_order) VALUES ('coins_500', 'consumable', '500 Coins', 500, 0, 1, 1)`,
		`INSERT INTO billing_products (product_id, kind, title, coins, gems, active, sort_order) VALUES ('gems_50', 'consumable', '50 Gems', 0, 50, 1, 2)`,
		`INSERT INTO billing_products (product_id, kind, title, coins, gems, active, sort_order) VALUES ('old_pack', 'consumable', 'Retired', 10, 0, 0, 3)`,
		`INSERT INTO billing_products (product_id, kind, title, coins, gems, active, sort_order) VALUES ('premium', 'subscription', 'Premium', 0, 0, 1, 10)`,
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	play := newFakePlay()
	t.Cleanup(SetBillingForTest(play, testBillingSecret, allowTest))
	f := &billingFixture{db: db, play: play}
	f.alice = testutil.SeedClient(t, db, "alice")
	f.bob = testutil.SeedClient(t, db, "bob")
	return f
}

func (f *billingFixture) accountID(clientID int) string {
	return ObfuscatedAccountID([]byte(testBillingSecret), clientID)
}

// paidPack registers a genuine, paid, unconsumed purchase by clientID.
func (f *billingFixture) paidPack(token, productID string, clientID int) *playstore.ProductPurchase {
	p := &playstore.ProductPurchase{
		ProductID: productID, OrderID: "GPA.1234-" + token, PurchaseState: playstore.PurchaseStatePurchased,
		ObfuscatedAccountID: f.accountID(clientID), PurchaseTime: time.Now().UTC(), Quantity: 1,
	}
	f.play.mu.Lock()
	f.play.products[token] = p
	f.play.mu.Unlock()
	return p
}

func (f *billingFixture) balance(t *testing.T, clientID int) (coins, gems int) {
	t.Helper()
	var b struct{ Coins, Gems int }
	if err := f.db.Raw(`SELECT coins, gems FROM clients WHERE id = ?`, clientID).Scan(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b.Coins, b.Gems
}

func (f *billingFixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.Raw(q, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func verify(clientID int, productID, token string) (*dto.VerifyPurchaseResponseDTO, int, error) {
	return VerifyPurchase(context.Background(), clientID, &dto.VerifyPurchaseRequestDTO{ProductID: productID, PurchaseToken: token})
}

func TestVerifyConsumable_GrantsOnceAndConsumes(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-a1", "coins_500", f.alice)
	before, _ := f.balance(t, f.alice)

	res, status, err := verify(f.alice, "coins_500", "tok-a1")
	if err != nil || status != http.StatusOK || res.Status != dto.VerifyStatusGranted || res.CoinsAdded != 500 {
		t.Fatalf("first verify: %+v %d %v", res, status, err)
	}
	if res.Balance.Coins != before+500 {
		t.Errorf("balance in response = %d, want %d", res.Balance.Coins, before+500)
	}
	if f.play.consumes != 1 {
		t.Errorf("consumes = %d, want 1", f.play.consumes)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM play_purchases WHERE purchase_token = 'tok-a1' AND consumed = 1`); n != 1 {
		t.Errorf("purchase not recorded as consumed")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND amount = 500`, f.alice, models.TxTypeIAPPurchase); n != 1 {
		t.Errorf("ledger rows = %d, want 1", n)
	}

	// Sending it again (app retry) pays nothing more.
	res, status, err = verify(f.alice, "coins_500", "tok-a1")
	if err != nil || status != http.StatusOK || res.Status != dto.VerifyStatusAlreadyGranted || res.CoinsAdded != 0 {
		t.Fatalf("repeat verify: %+v %d %v", res, status, err)
	}
	if c, _ := f.balance(t, f.alice); c != before+500 {
		t.Errorf("coins after repeat = %d, want %d", c, before+500)
	}
}

func TestVerifyConsumable_ConcurrentSameTokenPaysOnce(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-race", "coins_500", f.alice)
	before, _ := f.balance(t, f.alice)

	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _, err := verify(f.alice, "coins_500", "tok-race")
			if err == nil && res.Status == dto.VerifyStatusGranted {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 1 {
		t.Errorf("granted %d times, want exactly 1", granted)
	}
	if c, _ := f.balance(t, f.alice); c != before+500 {
		t.Errorf("coins = %d, want %d", c, before+500)
	}
}

func TestVerifyConsumable_RejectsAnotherPlayersToken(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-alice", "coins_500", f.alice)
	bobBefore, _ := f.balance(t, f.bob)

	_, status, err := verify(f.bob, "coins_500", "tok-alice")
	if err == nil || status != http.StatusForbidden {
		t.Fatalf("bob redeeming alice's token: status %d err %v, want 403", status, err)
	}
	if c, _ := f.balance(t, f.bob); c != bobBefore {
		t.Errorf("bob was credited")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM billing_audit WHERE client_id = ? AND result = 'fraud'`, f.bob); n != 1 {
		t.Errorf("fraud audit rows = %d, want 1", n)
	}
	// Raw tokens never reach the audit log.
	if n := f.count(t, `SELECT COUNT(*) FROM billing_audit WHERE purchase_token_hash = 'tok-alice' OR reason LIKE '%tok-alice%'`); n != 0 {
		t.Errorf("raw token stored in audit log")
	}
}

func TestVerifyConsumable_TokenAlreadyGrantedToOtherIs409(t *testing.T) {
	f := newBillingFixture(t, false)
	p := f.paidPack("tok-shared", "coins_500", f.alice)
	if _, _, err := verify(f.alice, "coins_500", "tok-shared"); err != nil {
		t.Fatal(err)
	}
	// Even if Google somehow reported bob's account id, the token is alice's.
	f.play.mu.Lock()
	p.ObfuscatedAccountID = f.accountID(f.bob)
	p.Consumed = false
	f.play.mu.Unlock()
	_, status, err := verify(f.bob, "coins_500", "tok-shared")
	if err == nil || status != http.StatusConflict {
		t.Fatalf("status %d err %v, want 409", status, err)
	}
}

func TestVerifyConsumable_Rejections(t *testing.T) {
	cases := []struct {
		name       string
		allowTest  bool
		setup      func(f *billingFixture)
		productID  string
		token      string
		wantStatus int
	}{
		{"missing account id", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice).ObfuscatedAccountID = ""
		}, "coins_500", "t", http.StatusForbidden},
		{"forged account id", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice).ObfuscatedAccountID = ObfuscatedAccountID([]byte("wrong-secret"), f.alice)
		}, "coins_500", "t", http.StatusForbidden},
		{"cancelled purchase", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice).PurchaseState = playstore.PurchaseStateCanceled
		}, "coins_500", "t", http.StatusBadRequest},
		{"token for another product", false, func(f *billingFixture) {
			f.paidPack("t", "gems_50", f.alice)
		}, "coins_500", "t", http.StatusBadRequest},
		{"unknown token", false, func(*billingFixture) {}, "coins_500", "nope", http.StatusBadRequest},
		{"unknown product", false, func(f *billingFixture) { f.paidPack("t", "coins_500", f.alice) }, "coins_9999", "t", http.StatusBadRequest},
		{"test purchase in production", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice).IsTest = true
		}, "coins_500", "t", http.StatusForbidden},
		{"consumed at Google but never granted", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice).Consumed = true
		}, "coins_500", "t", http.StatusConflict},
		{"absurd quantity", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice).Quantity = 1000
		}, "coins_500", "t", http.StatusBadRequest},
		{"Google unreachable", false, func(f *billingFixture) {
			f.paidPack("t", "coins_500", f.alice)
			f.play.lookupErr = errors.New("timeout")
		}, "coins_500", "t", http.StatusBadGateway},
		{"malformed token", false, func(*billingFixture) {}, "coins_500", "bad token with spaces", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBillingFixture(t, tc.allowTest)
			tc.setup(f)
			before, _ := f.balance(t, f.alice)
			_, status, err := verify(f.alice, tc.productID, tc.token)
			if err == nil || status != tc.wantStatus {
				t.Fatalf("status %d err %v, want %d", status, err, tc.wantStatus)
			}
			if c, _ := f.balance(t, f.alice); c != before {
				t.Errorf("coins changed on a rejected purchase: %d -> %d", before, c)
			}
			if n := f.count(t, `SELECT COUNT(*) FROM play_purchases`); n != 0 {
				t.Errorf("purchase recorded on rejection")
			}
		})
	}
}

func TestVerifyConsumable_PendingGrantsNothing(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-p", "coins_500", f.alice).PurchaseState = playstore.PurchaseStatePending
	res, status, err := verify(f.alice, "coins_500", "tok-p")
	if err != nil || status != http.StatusAccepted || res.Status != dto.VerifyStatusPending {
		t.Fatalf("%+v %d %v", res, status, err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM play_purchases`); n != 0 {
		t.Errorf("pending purchase was recorded")
	}
}

func TestVerifyConsumable_TestPurchaseAllowedOutsideProduction(t *testing.T) {
	f := newBillingFixture(t, true)
	f.paidPack("tok-t", "gems_50", f.alice).IsTest = true
	_, gemsBefore := f.balance(t, f.alice)
	res, status, err := verify(f.alice, "gems_50", "tok-t")
	if err != nil || status != http.StatusOK || res.GemsAdded != 50 {
		t.Fatalf("%+v %d %v", res, status, err)
	}
	if _, g := f.balance(t, f.alice); g != gemsBefore+50 {
		t.Errorf("gems = %d, want %d", g, gemsBefore+50)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM play_purchases WHERE test_purchase = 1`); n != 1 {
		t.Errorf("test purchase not flagged")
	}
}

func TestVerifyConsumable_RetiredProductStillHonoured(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-old", "old_pack", f.alice)
	if _, status, err := verify(f.alice, "old_pack", "tok-old"); err != nil || status != http.StatusOK {
		t.Fatalf("a paid purchase of a retired product must be granted: %d %v", status, err)
	}
	products, _, err := ListBillingProducts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		if p.ProductID == "old_pack" {
			t.Error("retired product listed for sale")
		}
	}
}

func TestVerifyConsumable_ConsumeFailureIsRetriedWithoutSecondCredit(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-c", "coins_500", f.alice)
	f.play.consumeErr = errors.New("google 503")
	before, _ := f.balance(t, f.alice)

	if res, _, err := verify(f.alice, "coins_500", "tok-c"); err != nil || res.Status != dto.VerifyStatusGranted {
		t.Fatalf("grant must stand even if consume fails: %+v %v", res, err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM play_purchases WHERE consumed = 0`); n != 1 {
		t.Fatalf("purchase should be waiting for consume")
	}

	// Google recovers; the sweeper consumes it (backdate so it is past the cutoff).
	f.play.consumeErr = nil
	f.db.Exec(`UPDATE play_purchases SET created_at = ?`, time.Now().UTC().Add(-10*time.Minute))
	RunBillingSweep(context.Background())
	if n := f.count(t, `SELECT COUNT(*) FROM play_purchases WHERE consumed = 1`); n != 1 {
		t.Errorf("sweeper did not consume")
	}
	if c, _ := f.balance(t, f.alice); c != before+500 {
		t.Errorf("coins = %d, want %d (exactly one credit)", c, before+500)
	}
}

func TestRefund_TakesBackClampedAndFlagsShortfall(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-r", "coins_500", f.alice)
	if _, _, err := verify(f.alice, "coins_500", "tok-r"); err != nil {
		t.Fatal(err)
	}
	// The player spends most of it before refunding.
	f.db.Exec(`UPDATE clients SET coins = 120 WHERE id = ?`, f.alice)

	if err := revokePurchase(context.Background(), "tok-r", "test"); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.balance(t, f.alice); c != 0 {
		t.Errorf("coins = %d, want 0 (clamped, never negative)", c)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE transaction_type = ? AND direction = 'debit' AND amount = 120`, models.TxTypeIAPRefund); n != 1 {
		t.Errorf("refund ledger row missing")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM billing_audit WHERE client_id = ? AND result = 'shortfall'`, f.alice); n != 1 {
		t.Errorf("shortfall not flagged")
	}

	// Repeating the refund (duplicate notification + sweeper) takes nothing more.
	f.db.Exec(`UPDATE clients SET coins = 50 WHERE id = ?`, f.alice)
	if err := revokePurchase(context.Background(), "tok-r", "test"); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.balance(t, f.alice); c != 50 {
		t.Errorf("second refund took coins again: %d", c)
	}
	// And a refunded token cannot be re-verified into a new credit.
	res, _, err := verify(f.alice, "coins_500", "tok-r")
	if err != nil || res.CoinsAdded != 0 {
		t.Errorf("refunded token paid again: %+v %v", res, err)
	}
}

func TestSweeper_RevokesFromVoidedList(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-v", "gems_50", f.alice)
	if _, _, err := verify(f.alice, "gems_50", "tok-v"); err != nil {
		t.Fatal(err)
	}
	_, gems := f.balance(t, f.alice)
	f.play.voided = []playstore.VoidedPurchase{{PurchaseToken: "tok-v"}}
	RunBillingSweep(context.Background())
	if _, g := f.balance(t, f.alice); g != gems-50 {
		t.Errorf("gems = %d, want %d", g, gems-50)
	}
}

// ---- Subscriptions ----------------------------------------------------------------

func (f *billingFixture) paidSub(token string, clientID int, expires time.Time) *playstore.SubscriptionPurchase {
	s := &playstore.SubscriptionPurchase{
		ProductIDs: []string{"premium"}, State: "SUBSCRIPTION_STATE_ACTIVE", ExpiresAt: &expires,
		AutoRenewing: true, ObfuscatedAccountID: f.accountID(clientID), LatestOrderID: "GPA.9",
	}
	f.play.mu.Lock()
	f.play.subs[token] = s
	f.play.mu.Unlock()
	return s
}

func TestSubscription_LifeCycle(t *testing.T) {
	f := newBillingFixture(t, false)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	prevNow := billingNow
	billingNow = func() time.Time { return now }
	t.Cleanup(func() { billingNow = prevNow })

	f.paidSub("sub-1", f.alice, now.Add(30*24*time.Hour))
	res, status, err := verify(f.alice, "premium", "sub-1")
	if err != nil || status != http.StatusOK || res.Subscription == nil || !res.Subscription.IsPremium {
		t.Fatalf("subscribe: %+v %d %v", res, status, err)
	}
	if f.play.acks != 1 {
		t.Errorf("acks = %d, want 1", f.play.acks)
	}
	if premium, _ := IsPremium(context.Background(), f.bob); premium {
		t.Error("bob is premium without paying")
	}

	// Bob cannot claim alice's subscription.
	if _, status, _ := verify(f.bob, "premium", "sub-1"); status != http.StatusForbidden {
		t.Errorf("bob claiming alice's subscription: %d, want 403", status)
	}

	// Upgrade/resubscribe: a new token replaces the old one.
	s2 := f.paidSub("sub-2", f.alice, now.Add(365*24*time.Hour))
	s2.LinkedPurchaseToken = "sub-1"
	if _, _, err := verify(f.alice, "premium", "sub-2"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM client_subscriptions WHERE purchase_token = 'sub-1' AND status = 'superseded'`); n != 1 {
		t.Error("old token not superseded")
	}

	// Renewal notification arrives after expiry with state EXPIRED: access ends.
	f.play.mu.Lock()
	f.play.subs["sub-2"].State = "SUBSCRIPTION_STATE_EXPIRED"
	f.play.mu.Unlock()
	rt, _ := getBilling()
	if err := refreshSubscription(context.Background(), rt, "sub-2"); err != nil {
		t.Fatal(err)
	}
	if premium, _ := IsPremium(context.Background(), f.alice); premium {
		t.Error("expired subscription still premium")
	}
}

func TestSubscription_ExpiryAndRevoke(t *testing.T) {
	f := newBillingFixture(t, false)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	prevNow := billingNow
	billingNow = func() time.Time { return now }
	t.Cleanup(func() { billingNow = prevNow })

	f.paidSub("sub-x", f.alice, now.Add(time.Hour))
	if _, _, err := verify(f.alice, "premium", "sub-x"); err != nil {
		t.Fatal(err)
	}
	if p, _ := IsPremium(context.Background(), f.alice); !p {
		t.Fatal("should be premium")
	}
	// Time passes beyond expiry without any notification: no access.
	billingNow = func() time.Time { return now.Add(2 * time.Hour) }
	if p, _ := IsPremium(context.Background(), f.alice); p {
		t.Error("premium past expires_at")
	}
	billingNow = func() time.Time { return now }

	// Refund: revoked, and a later refresh cannot reactivate it.
	if err := revokePurchase(context.Background(), "sub-x", "test"); err != nil {
		t.Fatal(err)
	}
	if p, _ := IsPremium(context.Background(), f.alice); p {
		t.Error("revoked subscription still premium")
	}
	rt, _ := getBilling()
	_ = refreshSubscription(context.Background(), rt, "sub-x")
	if n := f.count(t, `SELECT COUNT(*) FROM client_subscriptions WHERE purchase_token = 'sub-x' AND status = 'revoked'`); n != 1 {
		t.Error("refresh overwrote revoked status")
	}
}

func TestSubscription_PendingAndUnknownStatesGiveNoAccess(t *testing.T) {
	if mapSubscriptionState("SUBSCRIPTION_STATE_SOMETHING_NEW") != models.SubStatusExpired {
		t.Error("unknown state must fail closed")
	}
	f := newBillingFixture(t, false)
	f.paidSub("sub-p", f.alice, time.Now().Add(time.Hour)).State = "SUBSCRIPTION_STATE_PENDING"
	res, status, err := verify(f.alice, "premium", "sub-p")
	if err != nil || status != http.StatusAccepted || res.Status != dto.VerifyStatusPending {
		t.Fatalf("%+v %d %v", res, status, err)
	}
	if p, _ := IsPremium(context.Background(), f.alice); p {
		t.Error("pending subscription gave premium")
	}
}

// ---- Disabled billing and RTDN ------------------------------------------------------

func TestBilling_DisabledFailsClosed(t *testing.T) {
	billingMu.Lock()
	prevRT := billingRT
	billingRT = nil
	billingMu.Unlock()
	prevCfg := config.AppConfig
	config.AppConfig = &config.Config{} // nothing configured
	t.Cleanup(func() {
		billingMu.Lock()
		billingRT = prevRT
		billingMu.Unlock()
		config.AppConfig = prevCfg
	})

	if _, status, err := verify(1, "coins_500", "tok"); status != http.StatusServiceUnavailable || err == nil {
		t.Errorf("verify while disabled: %d %v, want 503", status, err)
	}
	if _, status, _ := GetBillingAccount(context.Background(), 1); status != http.StatusServiceUnavailable {
		t.Errorf("account id while disabled: %d, want 503", status)
	}
	if status, _ := AuthenticateRTDN(context.Background(), "Bearer x", "y"); status != http.StatusServiceUnavailable {
		t.Errorf("rtdn while disabled: %d, want 503", status)
	}
}

func withRTDNConfig(t *testing.T) {
	t.Helper()
	prevCfg := config.AppConfig
	config.AppConfig = &config.Config{Billing: config.BillingConfig{
		PackageName: "com.skillverse.skillverse_app", ServiceAccountJSON: []byte(`{}`), AccountSecret: testBillingSecret,
		RTDNToken: "rtdn-token-0123456789abcdef0123456789", RTDNAudience: "https://api.example/billing/rtdn",
		RTDNServiceAccountEmail: "pubsub-push@proj.iam.gserviceaccount.com",
	}}
	prevValidator := rtdnTokenValidator
	rtdnTokenValidator = func(_ context.Context, tok, aud string) (*idtoken.Payload, error) {
		if aud != "https://api.example/billing/rtdn" {
			return nil, errors.New("audience mismatch")
		}
		switch tok {
		case "good":
			return &idtoken.Payload{Issuer: "https://accounts.google.com", Claims: map[string]any{
				"email": "pubsub-push@proj.iam.gserviceaccount.com", "email_verified": true}}, nil
		case "other-sa":
			return &idtoken.Payload{Issuer: "https://accounts.google.com", Claims: map[string]any{
				"email": "attacker@evil.iam.gserviceaccount.com", "email_verified": true}}, nil
		case "bad-issuer":
			return &idtoken.Payload{Issuer: "https://evil.example", Claims: map[string]any{
				"email": "pubsub-push@proj.iam.gserviceaccount.com", "email_verified": true}}, nil
		}
		return nil, errors.New("invalid signature")
	}
	t.Cleanup(func() {
		config.AppConfig = prevCfg
		rtdnTokenValidator = prevValidator
	})
}

func TestAuthenticateRTDN(t *testing.T) {
	withRTDNConfig(t)
	const tok = "rtdn-token-0123456789abcdef0123456789"
	cases := []struct {
		name, auth, query string
		want              int
	}{
		{"valid", "Bearer good", tok, http.StatusOK},
		{"wrong query token", "Bearer good", "guess", http.StatusUnauthorized},
		{"missing query token", "Bearer good", "", http.StatusUnauthorized},
		{"missing bearer", "", tok, http.StatusUnauthorized},
		{"forged jwt", "Bearer forged", tok, http.StatusUnauthorized},
		{"wrong service account", "Bearer other-sa", tok, http.StatusUnauthorized},
		{"wrong issuer", "Bearer bad-issuer", tok, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := AuthenticateRTDN(context.Background(), tc.auth, tc.query); got != tc.want {
				t.Errorf("status %d, want %d", got, tc.want)
			}
		})
	}
}

func pushBody(t *testing.T, msgID string, n map[string]any) []byte {
	t.Helper()
	data, _ := json.Marshal(n)
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"messageId": msgID, "data": base64.StdEncoding.EncodeToString(data)}})
	return body
}

func TestHandleRTDN_VoidedIsAppliedOnceAndWrongPackageIgnored(t *testing.T) {
	f := newBillingFixture(t, false)
	f.paidPack("tok-n", "coins_500", f.alice)
	if _, _, err := verify(f.alice, "coins_500", "tok-n"); err != nil {
		t.Fatal(err)
	}
	coins, _ := f.balance(t, f.alice)

	// A notification for another app changes nothing.
	body := pushBody(t, "m0", map[string]any{"packageName": "com.evil.app",
		"voidedPurchaseNotification": map[string]any{"purchaseToken": "tok-n"}})
	if status, err := HandleRTDN(context.Background(), body); status != http.StatusOK || err != nil {
		t.Fatalf("%d %v", status, err)
	}
	if c, _ := f.balance(t, f.alice); c != coins {
		t.Fatal("wrong-package notification was applied")
	}

	body = pushBody(t, "m1", map[string]any{"packageName": "com.skillverse.skillverse_app",
		"voidedPurchaseNotification": map[string]any{"purchaseToken": "tok-n", "orderId": "GPA.1"}})
	for i := 0; i < 2; i++ { // Pub/Sub redelivery
		if status, err := HandleRTDN(context.Background(), body); status != http.StatusOK || err != nil {
			t.Fatalf("%d %v", status, err)
		}
	}
	if c, _ := f.balance(t, f.alice); c != coins-500 {
		t.Errorf("coins = %d, want %d", c, coins-500)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM billing_rtdn_messages WHERE message_id = 'm1'`); n != 1 {
		t.Error("message not recorded for dedupe")
	}
	if status, _ := HandleRTDN(context.Background(), []byte(`not json`)); status != http.StatusBadRequest {
		t.Errorf("garbage body: %d, want 400", status)
	}
}

// ---- Client-reported reward credits (POST /wallet/credit) ---------------------------

func TestCreditWallet_DailyCapAndAllowlist(t *testing.T) {
	f := newBillingFixture(t, false)
	ctx := context.Background()
	credit := func(reason, currency string, amount int) int {
		_, status, _ := CreditWallet(ctx, f.alice, &dto.WalletCreditRequestDTO{Currency: currency, Amount: amount, Reason: reason})
		return status
	}
	for i := 0; i < 3; i++ {
		if s := credit("spin_wheel", "coins", 100); s != http.StatusOK {
			t.Fatalf("spin %d: status %d", i, s)
		}
	}
	if s := credit("spin_wheel", "coins", 10); s != http.StatusTooManyRequests {
		t.Errorf("over the daily cap: %d, want 429", s)
	}
	// Other rewards have their own cap.
	if s := credit("chest_reward_galaxy", "gems", 25); s != http.StatusOK {
		t.Errorf("galaxy chest: %d", s)
	}
	if s := credit("coins_pack", "coins", 500); s != http.StatusBadRequest {
		t.Errorf("coin pack self-credit: %d, want 400", s)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = 'spin_wheel'`, f.alice); n != 3 {
		t.Errorf("spin ledger rows = %d, want 3", n)
	}
}
