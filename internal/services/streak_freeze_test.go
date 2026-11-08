package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"gorm.io/gorm"
)

// freezeDB is a test database with the tables the freeze code touches.
func freezeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewDB(t)
	err := db.Exec(`CREATE TABLE wallet_ledger (
		id INTEGER PRIMARY KEY AUTOINCREMENT, client_id INTEGER NOT NULL, transaction_type TEXT NOT NULL,
		direction TEXT NOT NULL DEFAULT 'credit', amount INTEGER NOT NULL, currency TEXT NOT NULL,
		balance_after INTEGER NOT NULL DEFAULT 0, reference_id TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`).Error
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// today is "2026-03-10 12:00 UTC" for the test.
func pinToday(t *testing.T) {
	t.Helper()
	prev := streakNow
	streakNow = func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { streakNow = prev })
}

// player seeds a client whose last active day was lastActive, with the given
// streak and number of freezes.
func player(t *testing.T, db *gorm.DB, name string, streak, freezes int, lastActive string) int {
	t.Helper()
	id := testutil.SeedClient(t, db, name)
	// full_name is set, like any player who has loaded their profile before; a
	// brand-new player's first load re-saves the profile, which is unrelated here.
	err := db.Exec(
		`UPDATE clients SET full_name = 'Player', current_streak = ?, highest_streak = ?, streak_freezes = ?, last_active_date = ? WHERE id = ?`,
		streak, streak, freezes, lastActive, id).Error
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type state struct{ Streak, Freezes, Gems int }

func stateOf(t *testing.T, db *gorm.DB, id int) state {
	t.Helper()
	var s state
	if err := db.Raw(`SELECT current_streak AS streak, streak_freezes AS freezes, gems FROM clients WHERE id = ?`, id).Scan(&s).Error; err != nil {
		t.Fatal(err)
	}
	return s
}

func activityDays(t *testing.T, db *gorm.DB, id int) []string {
	t.Helper()
	var days []string
	if err := db.Raw(`SELECT substr(CAST(day AS TEXT), 1, 10) FROM client_activity_days WHERE client_id = ? ORDER BY day`, id).Scan(&days).Error; err != nil {
		t.Fatal(err)
	}
	return days
}

func TestStreak_OpenedYesterdayContinuesWithoutSpendingFreezes(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 5, 2, "2026-03-09")

	if _, err := GetOrCreateProfile(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, db, id); s.Streak != 6 || s.Freezes != 2 {
		t.Errorf("state = %+v, want streak 6 and both freezes kept", s)
	}
}

func TestStreak_OneMissedDayIsBridgedByOneFreeze(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 5, 1, "2026-03-08") // missed the 9th

	p, err := GetOrCreateProfile(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.CurrentStreak != 6 {
		t.Errorf("returned streak = %d, want 6", p.CurrentStreak)
	}
	if s := stateOf(t, db, id); s.Streak != 6 || s.Freezes != 0 {
		t.Errorf("state = %+v, want streak 6 and the freeze used", s)
	}
	// The bridged day and today both show as active in the week calendar.
	days := activityDays(t, db, id)
	if len(days) != 2 || days[0] != "2026-03-09" || days[1] != "2026-03-10" {
		t.Errorf("activity days = %v, want [2026-03-09 2026-03-10]", days)
	}
}

func TestStreak_TwoMissedDaysNeedTwoFreezes(t *testing.T) {
	t.Run("two freezes save it", func(t *testing.T) {
		db := freezeDB(t)
		pinToday(t)
		id := player(t, db, "a", 9, 2, "2026-03-07") // missed the 8th and 9th
		if _, err := GetOrCreateProfile(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if s := stateOf(t, db, id); s.Streak != 10 || s.Freezes != 0 {
			t.Errorf("state = %+v, want streak 10 and both freezes used", s)
		}
	})

	t.Run("one freeze is not enough and is not wasted", func(t *testing.T) {
		db := freezeDB(t)
		pinToday(t)
		id := player(t, db, "a", 9, 1, "2026-03-07")
		if _, err := GetOrCreateProfile(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if s := stateOf(t, db, id); s.Streak != 1 || s.Freezes != 1 {
			t.Errorf("state = %+v, want the streak restarted at 1 with the freeze kept", s)
		}
	})
}

func TestStreak_NoFreezesBreaksAsBefore(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 20, 0, "2026-03-08")
	if _, err := GetOrCreateProfile(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, db, id); s.Streak != 1 {
		t.Errorf("streak = %d, want 1", s.Streak)
	}
}

func TestStreak_LongBreakIsBeyondFreezes(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 20, 2, "2026-03-05") // missed four days
	if _, err := GetOrCreateProfile(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, db, id); s.Streak != 1 || s.Freezes != 2 {
		t.Errorf("state = %+v, want the streak restarted with freezes kept", s)
	}
}

func TestStreak_ParallelFirstLoadsOfTheDaySpendOneFreeze(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 5, 2, "2026-03-08") // missed one day, holds two freezes

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = GetOrCreateProfile(context.Background(), id)
		}()
	}
	wg.Wait()

	if s := stateOf(t, db, id); s.Freezes != 1 || s.Streak != 6 {
		t.Errorf("state = %+v, want exactly one freeze spent and streak 6", s)
	}
}

func TestBuyStreakFreeze(t *testing.T) {
	t.Run("takes gems and adds a freeze", func(t *testing.T) {
		db := freezeDB(t)
		id := player(t, db, "a", 1, 0, "2026-03-10") // starts with 10 gems
		res, status, err := BuyStreakFreeze(context.Background(), id)
		if err != nil || status != 200 {
			t.Fatalf("status %d err %v", status, err)
		}
		if res.Freezes != 1 || res.Gems != 0 {
			t.Errorf("result = %+v, want 1 freeze and 0 gems left", res)
		}
		var rows int64
		db.Raw(`SELECT COUNT(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = 'streak_freeze_purchase' AND direction = 'debit' AND amount = ? AND currency = 'gems'`,
			id, StreakFreezePriceGems).Scan(&rows)
		if rows != 1 {
			t.Errorf("ledger rows = %d, want 1 gem debit", rows)
		}
	})

	t.Run("not enough gems", func(t *testing.T) {
		db := freezeDB(t)
		id := player(t, db, "a", 1, 0, "2026-03-10")
		db.Exec(`UPDATE clients SET gems = 9 WHERE id = ?`, id)
		_, status, err := BuyStreakFreeze(context.Background(), id)
		if err == nil || status != 400 {
			t.Fatalf("status %d err %v, want 400", status, err)
		}
		if s := stateOf(t, db, id); s.Gems != 9 || s.Freezes != 0 {
			t.Errorf("state = %+v, nothing should change", s)
		}
	})

	t.Run("already at the maximum", func(t *testing.T) {
		db := freezeDB(t)
		id := player(t, db, "a", 1, MaxStreakFreezes, "2026-03-10")
		_, status, err := BuyStreakFreeze(context.Background(), id)
		if err == nil || status != 409 {
			t.Fatalf("status %d err %v, want 409", status, err)
		}
		if s := stateOf(t, db, id); s.Gems != 10 || s.Freezes != MaxStreakFreezes {
			t.Errorf("state = %+v, no gems should be taken", s)
		}
	})

	t.Run("simultaneous buys with gems for one", func(t *testing.T) {
		db := freezeDB(t)
		id := player(t, db, "a", 1, 0, "2026-03-10")
		var wg sync.WaitGroup
		bought := make(chan bool, 6)
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := BuyStreakFreeze(context.Background(), id)
				bought <- err == nil
			}()
		}
		wg.Wait()
		close(bought)
		n := 0
		for ok := range bought {
			if ok {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d buys succeeded, want exactly 1", n)
		}
		if s := stateOf(t, db, id); s.Gems != 0 || s.Freezes != 1 {
			t.Errorf("state = %+v, want 0 gems and 1 freeze", s)
		}
	})
}

func TestGetStreakStatus_ReportsFreezes(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 4, 1, "2026-03-09")

	var status *dto.StreakStatusDTO
	status, code, err := GetStreakStatus(context.Background(), id)
	if err != nil || code != 200 {
		t.Fatalf("status %d err %v", code, err)
	}
	if !status.FreezesEnabled || status.Freezes != 1 || status.MaxFreezes != MaxStreakFreezes || status.FreezePriceGems != StreakFreezePriceGems {
		t.Errorf("freeze fields = %+v", status)
	}
}

func TestGetStreakStatus_FreezesOffWhenColumnIsMissing(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 4, 1, "2026-03-09")
	// An old database that has not had migration 000007 applied yet.
	if err := db.Exec(`ALTER TABLE clients DROP COLUMN streak_freezes`).Error; err != nil {
		t.Skipf("this SQLite cannot drop columns: %v", err)
	}

	status, code, err := GetStreakStatus(context.Background(), id)
	if err != nil || code != 200 {
		t.Fatalf("the streak must still load without the column: status %d err %v", code, err)
	}
	if status.FreezesEnabled || status.Freezes != 0 {
		t.Errorf("freezes should be switched off: %+v", status)
	}
}

func TestStreak_MissingFreezeColumnFallsBackToOldBehaviour(t *testing.T) {
	db := freezeDB(t)
	pinToday(t)
	id := player(t, db, "a", 5, 1, "2026-03-08")
	if err := db.Exec(`ALTER TABLE clients DROP COLUMN streak_freezes`).Error; err != nil {
		t.Skipf("this SQLite cannot drop columns: %v", err)
	}

	p, err := GetOrCreateProfile(context.Background(), id)
	if err != nil {
		t.Fatalf("the profile must still load without the column: %v", err)
	}
	if p.CurrentStreak != 1 {
		t.Errorf("streak = %d, want the old behaviour (restart at 1)", p.CurrentStreak)
	}
}
