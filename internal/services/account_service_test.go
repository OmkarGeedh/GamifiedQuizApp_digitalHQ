package services

import (
	"context"
	"net/http"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestDeleteAccount_NeedsThePasswordAndDeletesOnce(t *testing.T) {
	db := testutil.NewDB(t)
	id := testutil.SeedClient(t, db, "leaver")
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	db.Exec(`UPDATE clients SET password = ? WHERE id = ?`, string(hash), id)
	ctx := context.Background()
	exists := func() bool {
		var n int
		db.Raw(`SELECT COUNT(*) FROM clients WHERE id = ?`, id).Scan(&n)
		return n == 1
	}

	if status, _ := DeleteAccount(ctx, id, "", 0, &dto.DeleteAccountRequestDTO{}); status != http.StatusBadRequest {
		t.Errorf("no password: %d, want 400", status)
	}
	if status, _ := DeleteAccount(ctx, id, "", 0, &dto.DeleteAccountRequestDTO{Password: "wrong"}); status != http.StatusForbidden || !exists() {
		t.Fatalf("wrong password: %d, account kept: %v", status, exists())
	}
	if status, err := DeleteAccount(ctx, id, "", 0, &dto.DeleteAccountRequestDTO{Password: "correct horse"}); status != http.StatusOK || err != nil || exists() {
		t.Fatalf("delete: %d %v, still exists: %v", status, err, exists())
	}
	if status, _ := DeleteAccount(ctx, id, "", 0, &dto.DeleteAccountRequestDTO{Password: "correct horse"}); status != http.StatusNotFound {
		t.Errorf("second delete: %d, want 404", status)
	}
}

func TestAdsConfig_ChestOddsAreDisclosedAndAddUp(t *testing.T) {
	a := newAdsFixture(t)
	cfg, _, err := GetAdsConfig(context.Background(), a.alice)
	if err != nil {
		t.Fatal(err)
	}
	total := 0.0
	for _, o := range cfg.ChestOdds {
		total += o.Percent
	}
	if len(cfg.ChestOdds) != len(AdChestPrizes) || total < 99.999 || total > 100.001 {
		t.Errorf("chest odds %+v add up to %v, want 100", cfg.ChestOdds, total)
	}
}
