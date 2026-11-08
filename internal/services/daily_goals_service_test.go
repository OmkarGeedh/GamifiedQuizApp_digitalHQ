package services

import (
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

func TestDayBoundsUTC(t *testing.T) {
	// 23:30 in New York is already the next day in UTC; the day is always the UTC one.
	ny := time.FixedZone("NY", -5*3600)
	start, end := dayBoundsUTC(time.Date(2026, 3, 9, 23, 30, 0, 0, ny))
	if want := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Errorf("start = %v, want %v", start, want)
	}
	if want := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestDailyGoalRef_ChangesEachDay(t *testing.T) {
	d1 := time.Date(2026, 3, 10, 23, 59, 0, 0, time.UTC)
	d2 := d1.Add(2 * time.Minute)
	if dailyGoalRef("play_1", d1) == dailyGoalRef("play_1", d2) {
		t.Error("a new day must produce a new reference, or yesterday's claim would block today")
	}
	if got := dailyGoalRef("play_1", d1); got != "dg:play_1:20260310" {
		t.Errorf("ref = %q", got)
	}
	if len(dailyGoalRef("a_long_goal_identifier_here", d1)) > 64 {
		t.Error("reference must fit wallet_ledger.reference_id varchar(64)")
	}
}

func TestDailyGoalDefinitions(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range DailyGoals {
		if g.ID == "" || g.Title == "" || g.Target <= 0 || g.Reward <= 0 {
			t.Errorf("goal %+v is incomplete", g)
		}
		if g.Currency != models.CurrencyCoins && g.Currency != models.CurrencyGems {
			t.Errorf("goal %s pays in %q", g.ID, g.Currency)
		}
		if seen[g.ID] {
			t.Errorf("duplicate goal id %s", g.ID)
		}
		seen[g.ID] = true
		// Rewards must stay far below the wallet credit ceiling.
		if g.Currency == models.CurrencyCoins && g.Reward > 500 {
			t.Errorf("goal %s pays %d coins, which is too generous for a daily goal", g.ID, g.Reward)
		}
	}
}

func TestBuildDailyGoalStatuses(t *testing.T) {
	day := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	goals := []DailyGoal{
		{ID: "play_1", Kind: goalGames, Target: 1, Currency: "coins", Reward: 20},
		{ID: "play_3", Kind: goalGames, Target: 3, Currency: "coins", Reward: 40},
		{ID: "score_100", Kind: goalScore, Target: 100, Currency: "gems", Reward: 2},
	}

	t.Run("nothing played", func(t *testing.T) {
		got := buildDailyGoalStatuses(goals, repo.DailyProgress{}, day, map[string]bool{})
		for _, g := range got {
			if g.Completed || g.Claimable || g.Claimed || g.Progress != 0 {
				t.Errorf("%s should be untouched: %+v", g.ID, g)
			}
		}
	})

	t.Run("two games and 250 points", func(t *testing.T) {
		got := buildDailyGoalStatuses(goals, repo.DailyProgress{Games: 2, Score: 250}, day, map[string]bool{})
		if !got[0].Claimable || got[0].Progress != 1 {
			t.Errorf("play_1 should be claimable at progress 1: %+v", got[0])
		}
		if got[1].Completed || got[1].Progress != 2 {
			t.Errorf("play_3 should be 2/3 and not complete: %+v", got[1])
		}
		if !got[2].Claimable || got[2].Progress != 100 {
			t.Errorf("score_100 should be complete and capped at 100: %+v", got[2])
		}
	})

	t.Run("claimed goal is not claimable again", func(t *testing.T) {
		claimed := map[string]bool{dailyGoalRef("play_1", day): true}
		got := buildDailyGoalStatuses(goals, repo.DailyProgress{Games: 1}, day, claimed)
		if !got[0].Completed || !got[0].Claimed || got[0].Claimable {
			t.Errorf("play_1 should be completed + claimed, not claimable: %+v", got[0])
		}
	})

	t.Run("yesterday's claim does not count today", func(t *testing.T) {
		yesterday := day.AddDate(0, 0, -1)
		claimed := map[string]bool{dailyGoalRef("play_1", yesterday): true}
		got := buildDailyGoalStatuses(goals, repo.DailyProgress{Games: 1}, day, claimed)
		if got[0].Claimed || !got[0].Claimable {
			t.Errorf("a new day must be claimable again: %+v", got[0])
		}
	})
}
