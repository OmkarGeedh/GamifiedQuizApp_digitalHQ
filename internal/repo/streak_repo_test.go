package repo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"gorm.io/gorm"
)

func setStreak(t *testing.T, db *gorm.DB, id, streak int) {
	t.Helper()
	if err := db.Exec(`UPDATE clients SET current_streak = ? WHERE id = ?`, streak, id).Error; err != nil {
		t.Fatal(err)
	}
}

func TestClaimStreakMilestone_NotReachedPaysNothing(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	setStreak(t, db, id, 2)

	_, err := ClaimStreakMilestone(context.Background(), id, "sm:3:20260308", models.CurrencyCoins, 20, 3)
	if !errors.Is(err, ErrStreakMilestoneNotReached) {
		t.Fatalf("err = %v, want ErrStreakMilestoneNotReached", err)
	}
	if coins, _ := balances(t, db, id); coins != 100 {
		t.Errorf("coins = %d, want the starting 100 untouched", coins)
	}
}

func TestClaimStreakMilestone_PaysOnceAndWritesLedger(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	setStreak(t, db, id, 3)
	ref := "sm:3:20260308"

	pay, err := ClaimStreakMilestone(context.Background(), id, ref, models.CurrencyCoins, 20, 3)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if pay.Coins != 120 {
		t.Errorf("coins = %d, want 120", pay.Coins)
	}

	_, err = ClaimStreakMilestone(context.Background(), id, ref, models.CurrencyCoins, 20, 3)
	if !errors.Is(err, ErrRewardAlreadyClaimed) {
		t.Fatalf("repeat err = %v, want ErrRewardAlreadyClaimed", err)
	}
	if coins, _ := balances(t, db, id); coins != 120 {
		t.Errorf("coins = %d after a repeat, want 120", coins)
	}

	var rows int64
	db.Raw(`SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND reference_id = ?`,
		id, models.TxTypeStreakBonus, ref).Scan(&rows)
	if rows != 1 {
		t.Errorf("ledger rows = %d, want 1", rows)
	}

	claimed, err := ClaimedStreakMilestones(context.Background(), id, []string{ref, "sm:7:20260308"})
	if err != nil {
		t.Fatal(err)
	}
	if !claimed[ref] || claimed["sm:7:20260308"] {
		t.Errorf("claimed = %v", claimed)
	}
}

func TestClaimStreakMilestone_NewStreakCanClaimAgain(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	setStreak(t, db, id, 3)

	if _, err := ClaimStreakMilestone(context.Background(), id, "sm:3:20260101", "coins", 20, 3); err != nil {
		t.Fatalf("first streak: %v", err)
	}
	// The streak broke and a new one (a different start day) reached 3 days again.
	if _, err := ClaimStreakMilestone(context.Background(), id, "sm:3:20260301", "coins", 20, 3); err != nil {
		t.Fatalf("second streak should be claimable: %v", err)
	}
	if coins, _ := balances(t, db, id); coins != 140 {
		t.Errorf("coins = %d, want 140", coins)
	}
}

func TestClaimStreakMilestone_PaysGemsToGems(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	setStreak(t, db, id, 14)

	pay, err := ClaimStreakMilestone(context.Background(), id, "sm:14:20260225", models.CurrencyGems, 5, 14)
	if err != nil {
		t.Fatal(err)
	}
	if pay.Gems != 15 || pay.Coins != 100 {
		t.Errorf("balances = %+v, want 100 coins and 15 gems", pay)
	}
}

func TestClaimStreakMilestone_SimultaneousClaimsPayOnce(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	setStreak(t, db, id, 7)

	const taps = 8
	var wg sync.WaitGroup
	results := make(chan error, taps)
	for i := 0; i < taps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ClaimStreakMilestone(context.Background(), id, "sm:7:20260304", "coins", 50, 7)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	paid := 0
	for err := range results {
		switch {
		case err == nil:
			paid++
		case errors.Is(err, ErrRewardAlreadyClaimed):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if paid != 1 {
		t.Errorf("%d taps were paid, want exactly 1", paid)
	}
	if coins, _ := balances(t, db, id); coins != 150 {
		t.Errorf("coins = %d, want 150", coins)
	}
}

// A daily goal and a streak reward with the same reference text must not block
// each other: they are different kinds of reward.
func TestClaimedRewards_AreSeparatedByType(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	setStreak(t, db, id, 3)

	if _, err := ClaimStreakMilestone(context.Background(), id, "shared-ref", "coins", 20, 3); err != nil {
		t.Fatal(err)
	}
	claimed, err := ClaimedDailyGoals(context.Background(), id, []string{"shared-ref"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed["shared-ref"] {
		t.Error("a streak reward must not show up as a claimed daily goal")
	}
}
