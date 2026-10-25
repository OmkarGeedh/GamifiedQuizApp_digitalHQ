package dto

import (
	"strings"
	"testing"
	"time"
)

func TestLeaderboardFilterDTO_DefaultsAndValidation(t *testing.T) {
	// Test default assignment
	filter := LeaderboardFilterDTO{}
	if err := filter.Validate(); err != nil {
		t.Fatalf("unexpected error for empty filter: %v", err)
	}
	if filter.Scope != ScopeGlobal {
		t.Errorf("expected default scope '%s', got '%s'", ScopeGlobal, filter.Scope)
	}
	if filter.TimeRange != TimeRangeWeekly {
		t.Errorf("expected default time_range '%s', got '%s'", TimeRangeWeekly, filter.TimeRange)
	}
	if filter.Page != DefaultLeaderboardPage {
		t.Errorf("expected default page %d, got %d", DefaultLeaderboardPage, filter.Page)
	}
	if filter.PageSize != DefaultLeaderboardPageSize {
		t.Errorf("expected default page_size %d, got %d", DefaultLeaderboardPageSize, filter.PageSize)
	}
}

func TestLeaderboardFilterDTO_ScopeValidation(t *testing.T) {
	// Global is valid
	f := LeaderboardFilterDTO{Scope: "GLOBAL"}
	if err := f.Validate(); err != nil {
		t.Fatalf("expected GLOBAL to be valid, got: %v", err)
	}
	if f.Scope != ScopeGlobal {
		t.Errorf("expected lowercase '%s', got '%s'", ScopeGlobal, f.Scope)
	}

	// Friends scope is explicitly unsupported
	fFriends := LeaderboardFilterDTO{Scope: "friends"}
	err := fFriends.Validate()
	if err == nil {
		t.Fatal("expected error for scope 'friends', got nil")
	}
	if !strings.Contains(err.Error(), "currently unsupported") {
		t.Errorf("expected unsupported message for friends, got: %v", err)
	}

	// School scope is explicitly unsupported
	fSchool := LeaderboardFilterDTO{Scope: "school"}
	err = fSchool.Validate()
	if err == nil {
		t.Fatal("expected error for scope 'school', got nil")
	}
	if !strings.Contains(err.Error(), "currently unsupported") {
		t.Errorf("expected unsupported message for school, got: %v", err)
	}

	// Unknown scope
	fUnknown := LeaderboardFilterDTO{Scope: "unknown"}
	err = fUnknown.Validate()
	if err == nil {
		t.Fatal("expected error for unknown scope, got nil")
	}
}

func TestLeaderboardFilterDTO_TimeRangeValidation(t *testing.T) {
	validRanges := []string{"daily", "weekly", "monthly", "all_time", "DAILY", "Weekly", "ALL_TIME"}
	for _, tr := range validRanges {
		f := LeaderboardFilterDTO{TimeRange: tr}
		if err := f.Validate(); err != nil {
			t.Errorf("expected time_range '%s' to be valid, got: %v", tr, err)
		}
	}

	invalidRanges := []string{"yearly", "hourly", "yesterday", "invalid"}
	for _, tr := range invalidRanges {
		f := LeaderboardFilterDTO{TimeRange: tr}
		if err := f.Validate(); err == nil {
			t.Errorf("expected time_range '%s' to be rejected, got nil", tr)
		}
	}
}

func TestLeaderboardFilterDTO_GameModeValidation(t *testing.T) {
	// Empty game mode is valid (all modes)
	fEmpty := LeaderboardFilterDTO{}
	if err := fEmpty.Validate(); err != nil {
		t.Fatalf("expected empty game_mode to be valid, got: %v", err)
	}

	validModes := []string{"mcq", "sudden_death", "sort_it_out", "match_the_following", "MCQ", "Sort_It_Out"}
	for _, gm := range validModes {
		f := LeaderboardFilterDTO{GameMode: gm}
		if err := f.Validate(); err != nil {
			t.Errorf("expected game_mode '%s' to be valid, got: %v", gm, err)
		}
	}

	invalidModes := []string{"battle_royale", "chess", "trivia", "unknown"}
	for _, gm := range invalidModes {
		f := LeaderboardFilterDTO{GameMode: gm}
		if err := f.Validate(); err == nil {
			t.Errorf("expected game_mode '%s' to be rejected, got nil", gm)
		}
	}
}

func TestLeaderboardFilterDTO_PaginationBounding(t *testing.T) {
	// Page <= 0 clamped to 1
	f1 := LeaderboardFilterDTO{Page: -5, PageSize: 20}
	if err := f1.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if f1.Page != 1 {
		t.Errorf("expected page clamped to 1, got %d", f1.Page)
	}

	// PageSize <= 0 clamped to DefaultLeaderboardPageSize
	f2 := LeaderboardFilterDTO{Page: 1, PageSize: 0}
	if err := f2.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if f2.PageSize != DefaultLeaderboardPageSize {
		t.Errorf("expected page_size clamped to %d, got %d", DefaultLeaderboardPageSize, f2.PageSize)
	}

	// PageSize > MaxLeaderboardPageSize clamped to MaxLeaderboardPageSize
	f3 := LeaderboardFilterDTO{Page: 1, PageSize: 250}
	if err := f3.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if f3.PageSize != MaxLeaderboardPageSize {
		t.Errorf("expected page_size clamped to %d, got %d", MaxLeaderboardPageSize, f3.PageSize)
	}
}

func TestLeaderboardFilterDTO_CalculateTimeBounds(t *testing.T) {
	refTime := time.Date(2026, 10, 25, 14, 30, 0, 0, time.UTC)

	// Daily: [2026-10-25 00:00:00, 2026-10-26 00:00:00)
	fDaily := LeaderboardFilterDTO{TimeRange: TimeRangeDaily}
	start, end := fDaily.CalculateTimeBounds(refTime)
	if start == nil || end == nil {
		t.Fatal("expected non-nil daily bounds")
	}
	expectedDailyStart := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	expectedDailyEnd := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	if !start.Equal(expectedDailyStart) {
		t.Errorf("expected daily start %v, got %v", expectedDailyStart, start)
	}
	if !end.Equal(expectedDailyEnd) {
		t.Errorf("expected daily end %v, got %v", expectedDailyEnd, end)
	}

	// Weekly: rolling last 7 days: completed_at >= refTime - 7 days (start-inclusive, end unbounded)
	fWeekly := LeaderboardFilterDTO{TimeRange: TimeRangeWeekly}
	start, end = fWeekly.CalculateTimeBounds(refTime)
	if start == nil {
		t.Fatal("expected non-nil weekly start")
	}
	if end != nil {
		t.Errorf("expected nil weekly end (no artificial upper-bound workaround), got %v", end)
	}
	expectedWeeklyStart := refTime.AddDate(0, 0, -7)
	if !start.Equal(expectedWeeklyStart) {
		t.Errorf("expected weekly start %v, got %v", expectedWeeklyStart, start)
	}

	// Monthly: rolling last 30 days: completed_at >= refTime - 30 days (start-inclusive, end unbounded)
	fMonthly := LeaderboardFilterDTO{TimeRange: TimeRangeMonthly}
	start, end = fMonthly.CalculateTimeBounds(refTime)
	if start == nil {
		t.Fatal("expected non-nil monthly start")
	}
	if end != nil {
		t.Errorf("expected nil monthly end (no artificial upper-bound workaround), got %v", end)
	}
	expectedMonthlyStart := refTime.AddDate(0, 0, -30)
	if !start.Equal(expectedMonthlyStart) {
		t.Errorf("expected monthly start %v, got %v", expectedMonthlyStart, start)
	}

	// All Time: nil, nil
	fAllTime := LeaderboardFilterDTO{TimeRange: TimeRangeAllTime}
	start, end = fAllTime.CalculateTimeBounds(refTime)
	if start != nil || end != nil {
		t.Errorf("expected nil bounds for all_time, got start=%v, end=%v", start, end)
	}
}
