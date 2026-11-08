package services

import (
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

var streakToday = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

func TestStreakStartDay(t *testing.T) {
	// A 1-day streak started today; a 7-day streak started six days ago.
	if got := streakStartDay(streakToday, 1); !got.Equal(streakToday) {
		t.Errorf("1-day streak starts %v, want today", got)
	}
	if got, want := streakStartDay(streakToday, 7), time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("7-day streak starts %v, want %v", got, want)
	}
	if got := streakStartDay(streakToday, 0); !got.Equal(streakToday) {
		t.Errorf("no streak should fall back to today, got %v", got)
	}
}

func TestStreakMilestoneRef_IsPerStreak(t *testing.T) {
	a := streakMilestoneRef(7, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC))
	b := streakMilestoneRef(7, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if a == b {
		t.Error("a new streak must get a new reference, or the old claim would block it")
	}
	if a != "sm:7:20260304" {
		t.Errorf("ref = %q", a)
	}
}

func TestStreakMilestoneDefinitions(t *testing.T) {
	prev := 0
	for _, m := range StreakMilestones {
		if m.Days <= prev {
			t.Errorf("milestones must be in ascending order of days, got %d after %d", m.Days, prev)
		}
		prev = m.Days
		if m.Reward <= 0 {
			t.Errorf("milestone %d pays %d", m.Days, m.Reward)
		}
		if m.Currency != models.CurrencyCoins && m.Currency != models.CurrencyGems {
			t.Errorf("milestone %d pays in %q", m.Days, m.Currency)
		}
		if m.Currency == models.CurrencyCoins && m.Reward > 500 {
			t.Errorf("milestone %d pays %d coins, too generous", m.Days, m.Reward)
		}
	}
}

func TestBuildStreakStatus(t *testing.T) {
	ms := []StreakMilestone{
		{Days: 3, Currency: "coins", Reward: 20},
		{Days: 7, Currency: "coins", Reward: 50},
		{Days: 14, Currency: "gems", Reward: 5},
	}
	end := streakToday.AddDate(0, 0, 1)

	t.Run("day one: first milestone is the target", func(t *testing.T) {
		s := buildStreakStatus(ms, 1, 1, streakToday, end, map[string]bool{})
		if s.NextMilestoneDays != 3 || s.DaysToNextMilestone != 2 {
			t.Errorf("next = %d in %d days, want 3 in 2", s.NextMilestoneDays, s.DaysToNextMilestone)
		}
		for _, m := range s.Milestones {
			if m.Reached || m.Claimable || m.Claimed {
				t.Errorf("nothing should be reached yet: %+v", m)
			}
		}
	})

	t.Run("reached but not collected", func(t *testing.T) {
		start := streakStartDay(streakToday, 7)
		s := buildStreakStatus(ms, 7, 7, start, end, map[string]bool{})
		if !s.Milestones[0].Claimable || !s.Milestones[1].Claimable || s.Milestones[2].Reached {
			t.Errorf("3 and 7 should be claimable, 14 not reached: %+v", s.Milestones)
		}
		if s.NextMilestoneDays != 14 || s.DaysToNextMilestone != 7 {
			t.Errorf("next = %d in %d days, want 14 in 7", s.NextMilestoneDays, s.DaysToNextMilestone)
		}
	})

	t.Run("collected ones are not claimable again", func(t *testing.T) {
		start := streakStartDay(streakToday, 7)
		claimed := map[string]bool{streakMilestoneRef(3, start): true}
		s := buildStreakStatus(ms, 7, 7, start, end, claimed)
		if !s.Milestones[0].Claimed || s.Milestones[0].Claimable {
			t.Errorf("3-day should be claimed: %+v", s.Milestones[0])
		}
		if !s.Milestones[1].Claimable {
			t.Errorf("7-day should still be claimable: %+v", s.Milestones[1])
		}
	})

	t.Run("a new streak can collect again", func(t *testing.T) {
		oldStart := streakStartDay(streakToday.AddDate(0, 0, -30), 3)
		claimed := map[string]bool{streakMilestoneRef(3, oldStart): true}
		newStart := streakStartDay(streakToday, 3)
		s := buildStreakStatus(ms, 3, 9, newStart, end, claimed)
		if s.Milestones[0].Claimed || !s.Milestones[0].Claimable {
			t.Errorf("the new streak's 3-day reward must be claimable: %+v", s.Milestones[0])
		}
		if s.Highest != 9 {
			t.Errorf("highest = %d, want 9", s.Highest)
		}
	})

	t.Run("everything reached: no next milestone", func(t *testing.T) {
		start := streakStartDay(streakToday, 20)
		s := buildStreakStatus(ms, 20, 20, start, end, map[string]bool{})
		if s.NextMilestoneDays != 0 || s.DaysToNextMilestone != 0 {
			t.Errorf("next = %d in %d, want none", s.NextMilestoneDays, s.DaysToNextMilestone)
		}
	})
}
