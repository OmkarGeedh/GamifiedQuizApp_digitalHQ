package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

func TestLeaderboard_Empty(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()

	filter := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeWeekly,
		Page:      1,
		PageSize:  50,
	}

	res, status, err := GetLeaderboard(ctx, 1, filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 200 {
		t.Errorf("expected status 200, got %d", status)
	}
	if res.TotalCount != 0 {
		t.Errorf("expected total_count 0, got %d", res.TotalCount)
	}
	if len(res.Items) != 0 {
		t.Errorf("expected empty items slice, got %d items", len(res.Items))
	}
	if res.HasMore {
		t.Errorf("expected has_more to be false")
	}
	if res.CurrentUserRank != nil {
		t.Errorf("expected current_user_rank to be nil, got %v", *res.CurrentUserRank)
	}
}

func TestLeaderboard_MultipleUsers_RankedByDescendingScore(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 3 active clients
	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "player1", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 2, Username: "player2", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 3, Username: "player3", Status: "active"})

	repo.SeedInMemoryProfile(&models.Profile{ClientID: 1, UUID: "uuid-1", FullName: "Alice"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 2, UUID: "uuid-2", FullName: "Bob"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 3, UUID: "uuid-3", FullName: "Charlie"})

	// Alice: 150 points, Bob: 200 points, Charlie: 50 points
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s1",
		ClientID:    1,
		TopicID:     "accounting",
		GameMode:    models.GameModeMCQ,
		FinalScore:  150,
		CompletedAt: now,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s2",
		ClientID:    2,
		TopicID:     "accounting",
		GameMode:    models.GameModeMCQ,
		FinalScore:  200,
		CompletedAt: now,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s3",
		ClientID:    3,
		TopicID:     "accounting",
		GameMode:    models.GameModeMCQ,
		FinalScore:  50,
		CompletedAt: now,
	})

	filter := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeWeekly,
		Page:      1,
		PageSize:  50,
	}

	res, status, err := GetLeaderboard(ctx, 1, filter)
	if err != nil {
		t.Fatalf("GetLeaderboard error: %v", err)
	}
	if status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}
	if res.TotalCount != 3 {
		t.Fatalf("expected total_count 3, got %d", res.TotalCount)
	}

	// Verify descending score order
	if len(res.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(res.Items))
	}
	if res.Items[0].UserID != "uuid-2" || res.Items[0].Score != 200 || res.Items[0].Rank != 1 {
		t.Errorf("Rank 1 should be Bob (score 200), got: %+v", res.Items[0])
	}
	if res.Items[1].UserID != "uuid-1" || res.Items[1].Score != 150 || res.Items[1].Rank != 2 {
		t.Errorf("Rank 2 should be Alice (score 150), got: %+v", res.Items[1])
	}
	if res.Items[2].UserID != "uuid-3" || res.Items[2].Score != 50 || res.Items[2].Rank != 3 {
		t.Errorf("Rank 3 should be Charlie (score 50), got: %+v", res.Items[2])
	}

	// Verify is_current_user for Alice (clientID 1)
	if !res.Items[1].IsCurrentUser {
		t.Errorf("Alice should have is_current_user=true")
	}
	if res.Items[0].IsCurrentUser || res.Items[2].IsCurrentUser {
		t.Errorf("Bob and Charlie should have is_current_user=false")
	}
	if res.CurrentUserRank == nil || *res.CurrentUserRank != 2 {
		t.Errorf("expected current_user_rank 2, got %v", res.CurrentUserRank)
	}
}

func TestLeaderboard_ZeroPointUsersAppear(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 2 active clients: player1 has played, player2 has 0 games
	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "player1", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 2, Username: "player2", Status: "active"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 1, UUID: "uuid-1", FullName: "Alice"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 2, UUID: "uuid-2", FullName: "Bob"})

	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s1",
		ClientID:    1,
		TopicID:     "math",
		GameMode:    models.GameModeMCQ,
		FinalScore:  100,
		CompletedAt: now,
	})

	filter := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeWeekly,
		Page:      1,
		PageSize:  50,
	}

	res, _, err := GetLeaderboard(ctx, 2, filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalCount != 2 {
		t.Fatalf("expected total_count 2 including zero point user, got %d", res.TotalCount)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(res.Items))
	}
	if res.Items[1].UserID != "uuid-2" || res.Items[1].Score != 0 || res.Items[1].Rank != 2 {
		t.Errorf("Bob should appear with score 0 at rank 2, got: %+v", res.Items[1])
	}
	if !res.Items[1].IsCurrentUser {
		t.Errorf("Bob (clientID 2) should have is_current_user=true")
	}
	if res.CurrentUserRank == nil || *res.CurrentUserRank != 2 {
		t.Errorf("expected current_user_rank 2 for Bob, got %v", res.CurrentUserRank)
	}
}

func TestLeaderboard_PaginationWithoutRankResets(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	// Create 5 active players with scores 50, 40, 30, 20, 10
	for i := 1; i <= 5; i++ {
		cID := i
		score := (6 - i) * 10
		repo.SeedInMemoryClient(&models.Client{ID: cID, Username: fmt.Sprintf("player%d", cID), Status: "active"})
		repo.SeedInMemoryProfile(&models.Profile{ClientID: cID, UUID: fmt.Sprintf("uuid-%d", cID), FullName: fmt.Sprintf("Player %d", cID)})
		_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
			SessionID:   fmt.Sprintf("s%d", cID),
			ClientID:    cID,
			TopicID:     "general",
			GameMode:    models.GameModeMCQ,
			FinalScore:  score,
			CompletedAt: now,
		})
	}

	// Page 1 with pageSize 2 (Ranks 1 and 2)
	fPage1 := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeWeekly,
		Page:      1,
		PageSize:  2,
	}
	res1, _, err := GetLeaderboard(ctx, 4, fPage1) // Current user is Player 4 (Rank 4)
	if err != nil {
		t.Fatalf("page 1 error: %v", err)
	}
	if len(res1.Items) != 2 {
		t.Fatalf("expected 2 items on page 1, got %d", len(res1.Items))
	}
	if res1.Items[0].Rank != 1 || res1.Items[1].Rank != 2 {
		t.Errorf("page 1 ranks should be 1 and 2, got %d, %d", res1.Items[0].Rank, res1.Items[1].Rank)
	}
	if !res1.HasMore {
		t.Errorf("page 1 should have has_more=true")
	}
	if res1.CurrentUserRank == nil || *res1.CurrentUserRank != 4 {
		t.Errorf("current_user_rank on page 1 should be 4 even though user is on page 2, got %v", res1.CurrentUserRank)
	}

	// Page 2 with pageSize 2 (Ranks 3 and 4)
	fPage2 := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeWeekly,
		Page:      2,
		PageSize:  2,
	}
	res2, _, err := GetLeaderboard(ctx, 4, fPage2)
	if err != nil {
		t.Fatalf("page 2 error: %v", err)
	}
	if len(res2.Items) != 2 {
		t.Fatalf("expected 2 items on page 2, got %d", len(res2.Items))
	}
	if res2.Items[0].Rank != 3 || res2.Items[1].Rank != 4 {
		t.Errorf("page 2 ranks should be 3 and 4 (NOT reset to 1), got %d, %d", res2.Items[0].Rank, res2.Items[1].Rank)
	}
	if !res2.HasMore {
		t.Errorf("page 2 should have has_more=true")
	}
	if !res2.Items[1].IsCurrentUser {
		t.Errorf("Player 4 on page 2 should have is_current_user=true")
	}

	// Page 3 with pageSize 2 (Rank 5)
	fPage3 := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeWeekly,
		Page:      3,
		PageSize:  2,
	}
	res3, _, err := GetLeaderboard(ctx, 4, fPage3)
	if err != nil {
		t.Fatalf("page 3 error: %v", err)
	}
	if len(res3.Items) != 1 {
		t.Fatalf("expected 1 item on page 3, got %d", len(res3.Items))
	}
	if res3.Items[0].Rank != 5 {
		t.Errorf("page 3 rank should be 5, got %d", res3.Items[0].Rank)
	}
	if res3.HasMore {
		t.Errorf("page 3 should have has_more=false")
	}
}

func TestLeaderboard_DeterministicOrdering_EqualScoresTieBreaker(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 3 active players
	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "playerA", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 2, Username: "playerB", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 3, Username: "playerC", Status: "active"})

	repo.SeedInMemoryProfile(&models.Profile{ClientID: 1, UUID: "uuid-1", FullName: "Zack"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 2, UUID: "uuid-2", FullName: "Aaron"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 3, UUID: "uuid-3", FullName: "Ben"})

	// Player 1 (Zack) and Player 2 (Aaron) have equal score 100, but Zack finished 1 hour later
	t1 := now.Add(-2 * time.Hour)
	t2 := now.Add(-1 * time.Hour) // More recent!
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s1",
		ClientID:    2,
		FinalScore:  100,
		CompletedAt: t1,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s2",
		ClientID:    1,
		FinalScore:  100,
		CompletedAt: t2, // Zack played more recently
	})

	filter := &dto.LeaderboardFilterDTO{
		Scope:     dto.ScopeGlobal,
		TimeRange: dto.TimeRangeAllTime,
		Page:      1,
		PageSize:  50,
	}

	res, _, err := GetLeaderboard(ctx, 1, filter)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Tie-breaker: latest qualifying activity puts Zack (Rank 1) ahead of Aaron (Rank 2)
	if res.Items[0].UserID != "uuid-1" || res.Items[0].Rank != 1 {
		t.Errorf("Zack (uuid-1) with more recent activity should be Rank 1, got %+v", res.Items[0])
	}
	if res.Items[1].UserID != "uuid-2" || res.Items[1].Rank != 2 {
		t.Errorf("Aaron (uuid-2) should be Rank 2, got %+v", res.Items[1])
	}
	// Player 3 (Ben) with 0 score should be Rank 3
	if res.Items[2].UserID != "uuid-3" || res.Items[2].Rank != 3 {
		t.Errorf("Ben with 0 score should be Rank 3, got %+v", res.Items[2])
	}
}

func TestLeaderboard_TimeBoundaries_DailyWeeklyMonthlyAllTime(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "alice", Status: "active"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 1, UUID: "uuid-alice", FullName: "Alice"})

	// Game 1: Completed today (1 hour ago) -> 100 pts
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-today",
		ClientID:    1,
		TopicID:     "math",
		GameMode:    models.GameModeMCQ,
		FinalScore:  100,
		CompletedAt: now.Add(-1 * time.Hour),
	})

	// Game 2: Completed 3 days ago -> 50 pts
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-3days",
		ClientID:    1,
		TopicID:     "math",
		GameMode:    models.GameModeMCQ,
		FinalScore:  50,
		CompletedAt: now.Add(-3 * 24 * time.Hour),
	})

	// Game 3: Completed 15 days ago -> 25 pts
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-15days",
		ClientID:    1,
		TopicID:     "math",
		GameMode:    models.GameModeMCQ,
		FinalScore:  25,
		CompletedAt: now.Add(-15 * 24 * time.Hour),
	})

	// Game 4: Completed 45 days ago -> 10 pts
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-45days",
		ClientID:    1,
		TopicID:     "math",
		GameMode:    models.GameModeMCQ,
		FinalScore:  10,
		CompletedAt: now.Add(-45 * 24 * time.Hour),
	})

	// Daily: only s-today (100)
	resDaily, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeDaily, Page: 1, PageSize: 50})
	if resDaily.Items[0].Score != 100 {
		t.Errorf("Daily score expected 100, got %d", resDaily.Items[0].Score)
	}

	// Weekly: s-today + s-3days (100 + 50 = 150)
	resWeekly, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeWeekly, Page: 1, PageSize: 50})
	if resWeekly.Items[0].Score != 150 {
		t.Errorf("Weekly score expected 150, got %d", resWeekly.Items[0].Score)
	}

	// Monthly: s-today + s-3days + s-15days (100 + 50 + 25 = 175)
	resMonthly, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeMonthly, Page: 1, PageSize: 50})
	if resMonthly.Items[0].Score != 175 {
		t.Errorf("Monthly score expected 175, got %d", resMonthly.Items[0].Score)
	}

	// AllTime: all sessions (100 + 50 + 25 + 10 = 185)
	resAllTime, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime, Page: 1, PageSize: 50})
	if resAllTime.Items[0].Score != 185 {
		t.Errorf("AllTime score expected 185, got %d", resAllTime.Items[0].Score)
	}
}

func TestLeaderboard_GameModeAndSubjectFiltering(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "player1", Status: "active"})
	repo.SeedInMemoryProfile(&models.Profile{ClientID: 1, UUID: "uuid-1", FullName: "Alice"})

	// Games across modes and subjects
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID: "s-mcq-acc", ClientID: 1, TopicID: "accounting", GameMode: models.GameModeMCQ, FinalScore: 100, CompletedAt: now,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID: "s-sd-acc", ClientID: 1, TopicID: "accounting", GameMode: models.GameModeSuddenDeath, FinalScore: 115, CompletedAt: now,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID: "s-sio-math", ClientID: 1, TopicID: "math", GameMode: models.GameModeSortItOut, FinalScore: 80, CompletedAt: now,
	})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID: "s-mtf-acc", ClientID: 1, TopicID: "accounting", GameMode: models.GameModeMatchTheFollowing, FinalScore: 120, CompletedAt: now,
	})

	// 1. Filter by GameMode = mcq
	resMCQ, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{GameMode: models.GameModeMCQ, TimeRange: dto.TimeRangeAllTime})
	if resMCQ.Items[0].Score != 100 {
		t.Errorf("MCQ score expected 100, got %d", resMCQ.Items[0].Score)
	}

	// 2. Filter by GameMode = sudden_death
	resSD, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{GameMode: models.GameModeSuddenDeath, TimeRange: dto.TimeRangeAllTime})
	if resSD.Items[0].Score != 115 {
		t.Errorf("Sudden Death score expected 115, got %d", resSD.Items[0].Score)
	}

	// 3. Filter by Subject = accounting (MCQ 100 + SD 115 + MTF 120 = 335)
	resAcc, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{SubjectID: "accounting", TimeRange: dto.TimeRangeAllTime})
	if resAcc.Items[0].Score != 335 {
		t.Errorf("Accounting subject score expected 335, got %d", resAcc.Items[0].Score)
	}

	// 4. Combined: match_the_following + accounting + all_time (120)
	resCombined, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{
		GameMode:  models.GameModeMatchTheFollowing,
		SubjectID: "accounting",
		TimeRange: dto.TimeRangeAllTime,
	})
	if resCombined.Items[0].Score != 120 {
		t.Errorf("Combined filter score expected 120, got %d", resCombined.Items[0].Score)
	}

	// 5. Combined mode that doesn't match subject: sort_it_out + accounting -> 0
	resZero, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{
		GameMode:  models.GameModeSortItOut,
		SubjectID: "accounting",
		TimeRange: dto.TimeRangeAllTime,
	})
	if resZero.Items[0].Score != 0 {
		t.Errorf("Expected 0 score when mode and subject have no matches, got %d", resZero.Items[0].Score)
	}
}

func TestLeaderboard_InactiveAndDeletedAccountsExcluded(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()

	// 1 active, 1 deleted, 1 blocked, 1 pending
	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "activeUser", Status: "active"})
	repo.SeedInMemoryClient(&models.Client{ID: 2, Username: "deletedUser", Status: "deleted"})
	repo.SeedInMemoryClient(&models.Client{ID: 3, Username: "blockedUser", Status: "blocked"})
	repo.SeedInMemoryClient(&models.Client{ID: 4, Username: "pendingUser", Status: "pending"})

	res, _, err := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalCount != 1 {
		t.Errorf("expected only 1 active user, got %d", res.TotalCount)
	}
	if len(res.Items) != 1 || res.Items[0].DisplayName != "activeUser" {
		t.Errorf("expected only activeUser on leaderboard, got: %+v", res.Items)
	}

	// Attempting to look up rank for blocked user returns nil
	resBlocked, _, _ := GetLeaderboard(ctx, 3, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime})
	if resBlocked.CurrentUserRank != nil {
		t.Errorf("expected nil rank for blocked user, got %v", *resBlocked.CurrentUserRank)
	}
}

func TestLeaderboard_DuplicateSessionFinalizationIdempotency(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()
	now := time.Now().UTC()

	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "player1", Status: "active"})

	result := &models.LeaderboardGameResult{
		SessionID:   "session-unique-xyz",
		ClientID:    1,
		TopicID:     "math",
		GameMode:    models.GameModeMCQ,
		FinalScore:  100,
		CompletedAt: now,
	}

	// First record
	if err := repo.RecordLeaderboardResult(ctx, result); err != nil {
		t.Fatalf("first record failed: %v", err)
	}
	// Second record with same session ID
	if err := repo.RecordLeaderboardResult(ctx, result); err != nil {
		t.Fatalf("second record failed: %v", err)
	}

	res, _, _ := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime})
	if res.Items[0].Score != 100 {
		t.Errorf("score duplicated! Expected 100, got %d", res.Items[0].Score)
	}
}

func TestLeaderboard_NoSensitiveDataExposed(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()

	phone := "+1234567890"
	repo.SeedInMemoryClient(&models.Client{
		ID:       1,
		Username: "secure_player",
		Email:    "private@example.com",
		Phone:    &phone,
		Password: "hashed_super_secret_password",
		Status:   "active",
	})
	repo.SeedInMemoryProfile(&models.Profile{
		ClientID: 1,
		UUID:     "pub-uuid-1234",
		FullName: "Secret Agent",
	})

	res, _, err := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	jsonBytes, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to serialize: %v", err)
	}
	jsonStr := string(jsonBytes)

	// Assert sensitive terms never appear in the JSON output
	sensitiveTerms := []string{
		"private@example.com",
		"1234567890",
		"hashed_super_secret_password",
		"password",
		"email",
		"phone",
		"refresh_token",
	}
	for _, term := range sensitiveTerms {
		if strings.Contains(strings.ToLower(jsonStr), term) {
			t.Errorf("SECURITY RISK: JSON output contains sensitive field '%s': %s", term, jsonStr)
		}
	}

	// Verify required contract fields exist
	if !strings.Contains(jsonStr, `"user_id":"pub-uuid-1234"`) {
		t.Errorf("expected user_id to be public uuid: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"display_name":"Secret Agent"`) {
		t.Errorf("expected display_name to be Secret Agent: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"is_current_user":true`) {
		t.Errorf("expected is_current_user:true: %s", jsonStr)
	}
}

func TestLeaderboard_RedisUnavailableResilience(t *testing.T) {
	// Simulate Redis being nil
	orig := config.RedisClient
	config.RedisClient = nil
	defer func() { config.RedisClient = orig }()

	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()

	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "player1", Status: "active"})
	_ = repo.RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
		SessionID:   "s-redis-nil",
		ClientID:    1,
		FinalScore:  75,
		CompletedAt: time.Now().UTC(),
	})

	res, status, err := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime})
	if err != nil {
		t.Fatalf("expected nil error when Redis is unavailable, got %v", err)
	}
	if status != 200 {
		t.Errorf("expected status 200, got %d", status)
	}
	if res.Items[0].Score != 75 {
		t.Errorf("expected score 75, got %d", res.Items[0].Score)
	}
}

func TestLeaderboard_PersistenceForEveryGameMode(t *testing.T) {
	repo.ResetInMemoryLeaderboard()
	ctx := context.Background()

	repo.SeedInMemoryClient(&models.Client{ID: 1, Username: "all_mode_player", Status: "active"})

	modes := []struct {
		mode   string
		topic  string
		score  int
		sessID string
	}{
		{models.GameModeMCQ, "accounting", 100, "sess-mcq"},
		{models.GameModeSuddenDeath, "accounting", 115, "sess-sd"},
		{models.GameModeSortItOut, "finance", 90, "sess-sio"},
		{models.GameModeMatchTheFollowing, "economics", 120, "sess-mtf"},
	}

	for _, m := range modes {
		sess := &models.GameSession{
			ID:       m.sessID,
			ClientID: 1,
			TopicID:  m.topic,
			GameMode: m.mode,
			Score:    m.score,
			Status:   models.SessionStatusFinished,
		}
		// FinalizeSession must persist LeaderboardGameResult
		if err := repo.FinalizeSession(ctx, sess, 10, 10, 1); err != nil {
			t.Fatalf("FinalizeSession failed for mode %s: %v", m.mode, err)
		}
	}

	// Verify All Time has sum of all 4 modes: 100 + 115 + 90 + 120 = 425
	res, _, err := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{TimeRange: dto.TimeRangeAllTime})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if res.Items[0].Score != 425 {
		t.Errorf("expected total score 425 across all 4 modes, got %d", res.Items[0].Score)
	}

	// Verify each individual mode has its persisted score
	for _, m := range modes {
		mRes, _, err := GetLeaderboard(ctx, 1, &dto.LeaderboardFilterDTO{
			GameMode:  m.mode,
			TimeRange: dto.TimeRangeAllTime,
		})
		if err != nil {
			t.Fatalf("error for mode %s: %v", m.mode, err)
		}
		if mRes.Items[0].Score != m.score {
			t.Errorf("mode %s expected score %d, got %d", m.mode, m.score, mRes.Items[0].Score)
		}
	}
}
