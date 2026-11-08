package repo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"gorm.io/gorm"
)

var goalDay = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

// goalTestDB adds the two tables the daily goals read and write. They are created
// with plain SQL (the real ones come from the migration repo).
func goalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewDB(t)
	for _, stmt := range []string{
		`CREATE TABLE leaderboard_game_results (
			session_id TEXT PRIMARY KEY, client_id INTEGER NOT NULL, topic_id TEXT NOT NULL,
			game_mode TEXT NOT NULL, final_score INTEGER NOT NULL DEFAULT 0, completed_at DATETIME NOT NULL)`,
		`CREATE TABLE wallet_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT, client_id INTEGER NOT NULL, transaction_type TEXT NOT NULL,
			direction TEXT NOT NULL DEFAULT 'credit', amount INTEGER NOT NULL, currency TEXT NOT NULL,
			balance_after INTEGER NOT NULL DEFAULT 0, reference_id TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

var sessionSeq int

func playGame(t *testing.T, db *gorm.DB, clientID, score int, at time.Time) {
	t.Helper()
	sessionSeq++
	err := db.Exec(
		`INSERT INTO leaderboard_game_results (session_id, client_id, topic_id, game_mode, final_score, completed_at)
		 VALUES (?, ?, 'accounts', 'mcq', ?, ?)`,
		fmt.Sprintf("session-%d", sessionSeq), clientID, score, at).Error
	if err != nil {
		t.Fatalf("seed game: %v", err)
	}
}

func balances(t *testing.T, db *gorm.DB, id int) (coins, gems int) {
	t.Helper()
	var b struct{ Coins, Gems int }
	if err := db.Raw(`SELECT coins, gems FROM clients WHERE id = ?`, id).Scan(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b.Coins, b.Gems
}

func atLeast(games int) func(DailyProgress) bool {
	return func(p DailyProgress) bool { return p.Games >= games }
}

func TestGetDailyProgress_CountsOnlyThisPlayerAndThisDay(t *testing.T) {
	db := goalTestDB(t)
	me := testutil.SeedClient(t, db, "me")
	other := testutil.SeedClient(t, db, "other")
	end := goalDay.AddDate(0, 0, 1)

	playGame(t, db, me, 40, goalDay.Add(1*time.Hour))
	playGame(t, db, me, 60, goalDay.Add(23*time.Hour))
	playGame(t, db, me, 999, goalDay.Add(-time.Second)) // just before midnight: yesterday
	playGame(t, db, me, 999, end)                       // exactly next midnight: tomorrow
	playGame(t, db, other, 500, goalDay.Add(2*time.Hour))

	p, err := GetDailyProgress(context.Background(), me, goalDay, end)
	if err != nil {
		t.Fatal(err)
	}
	if p.Games != 2 || p.Score != 100 {
		t.Errorf("progress = %+v, want 2 games / 100 points", p)
	}
}

func TestClaimDailyGoal_NotMetPaysNothing(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	end := goalDay.AddDate(0, 0, 1)

	_, err := ClaimDailyGoal(context.Background(), id, "dg:play_1:20260310", models.CurrencyCoins, 20, goalDay, end, atLeast(1))
	if !errors.Is(err, ErrDailyGoalNotMet) {
		t.Fatalf("err = %v, want ErrDailyGoalNotMet", err)
	}
	if coins, _ := balances(t, db, id); coins != 100 {
		t.Errorf("coins = %d, want the starting 100 untouched", coins)
	}
}

func TestClaimDailyGoal_PaysOnceAndWritesLedger(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	end := goalDay.AddDate(0, 0, 1)
	playGame(t, db, id, 30, goalDay.Add(time.Hour))
	ref := "dg:play_1:20260310"

	pay, err := ClaimDailyGoal(context.Background(), id, ref, models.CurrencyCoins, 20, goalDay, end, atLeast(1))
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if pay.Coins != 120 {
		t.Errorf("coins after claim = %d, want 120", pay.Coins)
	}

	_, err = ClaimDailyGoal(context.Background(), id, ref, models.CurrencyCoins, 20, goalDay, end, atLeast(1))
	if !errors.Is(err, ErrDailyGoalAlreadyClaimed) {
		t.Fatalf("second claim err = %v, want ErrDailyGoalAlreadyClaimed", err)
	}
	if coins, _ := balances(t, db, id); coins != 120 {
		t.Errorf("coins = %d after a repeat claim, want 120 (paid once)", coins)
	}

	var rows int64
	db.Raw(`SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = ? AND reference_id = ?`,
		id, models.TxTypeDailyGoalReward, ref).Scan(&rows)
	if rows != 1 {
		t.Errorf("ledger rows = %d, want exactly 1", rows)
	}

	claimed, err := ClaimedDailyGoals(context.Background(), id, []string{ref, "dg:play_3:20260310"})
	if err != nil {
		t.Fatal(err)
	}
	if !claimed[ref] || claimed["dg:play_3:20260310"] {
		t.Errorf("claimed = %v", claimed)
	}
}

func TestClaimDailyGoal_PaysGemsToGems(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	end := goalDay.AddDate(0, 0, 1)
	playGame(t, db, id, 150, goalDay.Add(time.Hour))

	pay, err := ClaimDailyGoal(context.Background(), id, "dg:score_100:20260310", models.CurrencyGems, 2, goalDay, end,
		func(p DailyProgress) bool { return p.Score >= 100 })
	if err != nil {
		t.Fatal(err)
	}
	if pay.Gems != 12 || pay.Coins != 100 {
		t.Errorf("balances = %+v, want 100 coins and 12 gems", pay)
	}
}

func TestClaimDailyGoal_NewDayCanBeClaimedAgain(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	day2 := goalDay.AddDate(0, 0, 1)
	playGame(t, db, id, 10, goalDay.Add(time.Hour))
	playGame(t, db, id, 10, day2.Add(time.Hour))

	if _, err := ClaimDailyGoal(context.Background(), id, "dg:play_1:20260310", "coins", 20, goalDay, day2, atLeast(1)); err != nil {
		t.Fatalf("day 1: %v", err)
	}
	if _, err := ClaimDailyGoal(context.Background(), id, "dg:play_1:20260311", "coins", 20, day2, day2.AddDate(0, 0, 1), atLeast(1)); err != nil {
		t.Fatalf("day 2 should be claimable again: %v", err)
	}
	if coins, _ := balances(t, db, id); coins != 140 {
		t.Errorf("coins = %d, want 140 (two daily rewards)", coins)
	}
}

// Two taps at once must pay once. (On PostgreSQL the advisory lock does this; here
// SQLite's single connection serializes the transactions, so this guards the
// check-then-pay logic itself.)
func TestClaimDailyGoal_SimultaneousClaimsPayOnce(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	end := goalDay.AddDate(0, 0, 1)
	playGame(t, db, id, 10, goalDay.Add(time.Hour))

	const taps = 8
	var wg sync.WaitGroup
	results := make(chan error, taps)
	for i := 0; i < taps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ClaimDailyGoal(context.Background(), id, "dg:play_1:20260310", "coins", 20, goalDay, end, atLeast(1))
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
		case errors.Is(err, ErrDailyGoalAlreadyClaimed):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if paid != 1 {
		t.Errorf("%d taps were paid, want exactly 1", paid)
	}
	if coins, _ := balances(t, db, id); coins != 120 {
		t.Errorf("coins = %d, want 120", coins)
	}
}

func TestClaimDailyGoal_RejectsUnknownCurrency(t *testing.T) {
	db := goalTestDB(t)
	id := testutil.SeedClient(t, db, "p")
	_, err := ClaimDailyGoal(context.Background(), id, "dg:x:20260310", "experience; DROP TABLE clients", 5,
		goalDay, goalDay.AddDate(0, 0, 1), atLeast(0))
	if err == nil {
		t.Fatal("expected an error for a currency that is not coins or gems")
	}
}
