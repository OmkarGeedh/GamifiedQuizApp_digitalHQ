package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services/admob"
)

// TestMonetization_PostgreSQL runs the money paths against a real PostgreSQL
// with migrations up to 000010 (the SQLite tests cannot prove PostgreSQL
// syntax, uuid columns and constraint names). Opt-in and never reads .env:
//
//	MONETIZATION_PG_URL="postgresql://...local.../gamifiedapp?sslmode=disable" go test ./internal/services -run PostgreSQL -v
//
// Never point it at production: it creates and deletes a throwaway player.
func TestMonetization_PostgreSQL(t *testing.T) {
	url := os.Getenv("MONETIZATION_PG_URL")
	if url == "" {
		t.Skip("set MONETIZATION_PG_URL to a local PostgreSQL to run")
	}
	pg, err := db.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	prevDB, prevCfg := config.DB, config.AppConfig
	config.DB = pg
	ctx := context.Background()

	name := fmt.Sprintf("pgsmoke%d", time.Now().UnixNano())
	var clientID int
	if err := pg.Raw(`INSERT INTO clients (username, email, password, status, coins, gems)
		VALUES (?, ?, 'x', 'active', 500, 10) RETURNING id`, name, name+"@example.invalid").Scan(&clientID).Error; err != nil {
		t.Fatalf("seed player: %v", err)
	}
	t.Cleanup(func() {
		pg.Exec(`DELETE FROM billing_audit WHERE client_id = ?`, clientID)
		pg.Exec(`DELETE FROM clients WHERE id = ?`, clientID) // cascades to ledger, rewards, tokens, uses, purchases
		pg.Exec(`DELETE FROM billing_product_subjects WHERE product_id = 'pgsmoke_pack'`)
		pg.Exec(`DELETE FROM billing_products WHERE product_id = 'pgsmoke_pack'`)
		InvalidateTopicPackCache()
		config.DB, config.AppConfig = prevDB, prevCfg
	})
	coins := func() int {
		var c int
		pg.Raw(`SELECT coins FROM clients WHERE id = ?`, clientID).Scan(&c)
		return c
	}

	// Power-ups: coins, retry, token, ledger.
	sid := "pg-" + name
	if _, err := PayForPowerUp(ctx, clientID, sid, "q1", models.PowerUpFiftyFifty, "", "a,c"); err != nil {
		t.Fatalf("charge: %v", err)
	}
	if res, err := PayForPowerUp(ctx, clientID, sid, "q1", models.PowerUpFiftyFifty, "", "b,d"); err != nil || !res.AlreadyPaid || res.Result != "a,c" {
		t.Fatalf("retry: %+v %v", res, err)
	}
	if c := coins(); c != 480 {
		t.Fatalf("coins after one charge = %d, want 480", c)
	}

	// Rewarded ad: request, Google-signed callback, payout logged once.
	config.AppConfig = &config.Config{Server: config.ServerConfig{Env: "test"}, Ads: config.AdsConfig{
		Enabled: true, UserIDSecret: testAdsSecret, RewardedAdUnitAndroid: "ca-app-pub-1/2222222222"}}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t.Cleanup(SetSSVVerifierForTest(admob.NewVerifierWithKeys(map[int64]*ecdsa.PublicKey{3335741209: &key.PublicKey})))
	prevRand := adRandom
	adRandom = func(int) int { return 0 }
	t.Cleanup(func() { adRandom = prevRand })
	r, status, err := CreateAdReward(ctx, clientID, &dto.CreateAdRewardRequestDTO{Placement: models.PowerUpSkip})
	if err != nil || status != http.StatusOK {
		t.Fatalf("ad request: %d %v", status, err)
	}
	a := &adsFixture{key: key, now: time.Now().UTC()}
	userID, _ := AdsUserID(clientID)
	q := a.ssvQuery(t, r.ID, userID, "pg-txn-"+name, "2222222222")
	for i := 0; i < 2; i++ {
		if status, err := HandleAdMobSSV(ctx, q); status != http.StatusOK || err != nil {
			t.Fatalf("callback: %d %v", status, err)
		}
	}
	var tokens, ledger int
	pg.Raw(`SELECT quantity FROM client_power_ups WHERE client_id = ? AND power_up = 'skip'`, clientID).Scan(&tokens)
	pg.Raw(`SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = 'ad_reward'`, clientID).Scan(&ledger)
	if tokens != 1 || ledger != 1 {
		t.Fatalf("ad payout: tokens %d ledger %d, want 1 and 1", tokens, ledger)
	}
	if res, err := PayForPowerUp(ctx, clientID, sid, "*", models.PowerUpSkip, models.PayWithToken, ""); err != nil || res.PaidWith != models.PayWithToken {
		t.Fatalf("token spend: %+v %v", res, err)
	}

	// Topic pack gating and a Quiz Pass claim.
	pg.Exec(`INSERT INTO billing_products (product_id, kind, title) VALUES ('pgsmoke_pack', 'non_consumable', 'PG smoke pack')`)
	var subject string
	pg.Raw(`SELECT code FROM subjects ORDER BY code LIMIT 1`).Scan(&subject)
	if subject != "" {
		pg.Exec(`INSERT INTO billing_product_subjects (product_id, subject) VALUES ('pgsmoke_pack', ?)`, subject)
		InvalidateTopicPackCache()
		if err := CheckTopicAccess(ctx, clientID, subject); err == nil {
			t.Error("pack subject open without the pack")
		}
		pg.Exec(`DELETE FROM billing_product_subjects WHERE product_id = 'pgsmoke_pack'`)
		InvalidateTopicPackCache()
	}
	exp := time.Now().UTC().Add(24 * time.Hour)
	if err := pg.Exec(`INSERT INTO client_subscriptions (purchase_token, client_id, product_id, status, expires_at, acknowledged)
		VALUES (?, ?, 'quiz_pass', 'active', ?, true)`, "pg-sub-"+name, clientID, exp).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	if _, status, err := ClaimPassMonthlyGems(ctx, clientID); err != nil || status != http.StatusOK {
		t.Fatalf("pass claim: %d %v", status, err)
	}
	if _, status, _ := ClaimPassMonthlyGems(ctx, clientID); status != http.StatusConflict {
		t.Errorf("second pass claim: %d, want 409", status)
	}
}
