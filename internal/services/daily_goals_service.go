package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// dailyGoalKind says what a goal counts.
type dailyGoalKind int

const (
	goalGames dailyGoalKind = iota // finished games, any mode
	goalScore                      // points scored in finished games
)

// DailyGoal is one of today's goals. The list is the same for every player.
type DailyGoal struct {
	ID       string
	Title    string
	Kind     dailyGoalKind
	Target   int
	Currency string
	Reward   int
}

// DailyGoals are the goals every player gets each (UTC) day. Change the numbers
// here; nothing else needs to move.
var DailyGoals = []DailyGoal{
	{ID: "play_1", Title: "Finish 1 game", Kind: goalGames, Target: 1, Currency: models.CurrencyCoins, Reward: 20},
	{ID: "play_3", Title: "Finish 3 games", Kind: goalGames, Target: 3, Currency: models.CurrencyCoins, Reward: 40},
	{ID: "score_100", Title: "Score 100 points", Kind: goalScore, Target: 100, Currency: models.CurrencyGems, Reward: 2},
}

// dailyGoalsNow is replaced in tests.
var dailyGoalsNow = time.Now

// dayBoundsUTC returns [start, end) of the UTC day containing t.
func dayBoundsUTC(t time.Time) (time.Time, time.Time) {
	t = t.UTC()
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 0, 1)
}

// dailyGoalRef is the ledger reference for one goal on one day. It is what makes
// "claimed today" a lookup, and it changes at midnight so the goal can be claimed again.
func dailyGoalRef(goalID string, day time.Time) string {
	return fmt.Sprintf("dg:%s:%s", goalID, day.UTC().Format("20060102"))
}

func (g DailyGoal) value(p repo.DailyProgress) int {
	if g.Kind == goalScore {
		return p.Score
	}
	return p.Games
}

func (g DailyGoal) met(p repo.DailyProgress) bool { return g.value(p) >= g.Target }

// buildDailyGoalStatuses turns progress and the set of claimed references into
// the response rows. Pure, so it is easy to test.
func buildDailyGoalStatuses(goals []DailyGoal, p repo.DailyProgress, day time.Time, claimed map[string]bool) []dto.DailyGoalStatusDTO {
	out := make([]dto.DailyGoalStatusDTO, len(goals))
	for i, g := range goals {
		progress := g.value(p)
		if progress > g.Target {
			progress = g.Target
		}
		done := g.met(p)
		isClaimed := claimed[dailyGoalRef(g.ID, day)]
		out[i] = dto.DailyGoalStatusDTO{
			ID: g.ID, Title: g.Title, Target: g.Target, Progress: progress,
			Currency: g.Currency, Reward: g.Reward,
			Completed: done, Claimed: isClaimed, Claimable: done && !isClaimed,
		}
	}
	return out
}

// GetDailyGoals returns today's goals with the player's progress.
func GetDailyGoals(ctx context.Context, clientID int) (*dto.DailyGoalsResponseDTO, int, error) {
	now := dailyGoalsNow()
	start, end := dayBoundsUTC(now)

	p, err := repo.GetDailyProgress(ctx, clientID, start, end)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch daily progress: %w", err)
	}
	refs := make([]string, len(DailyGoals))
	for i, g := range DailyGoals {
		refs[i] = dailyGoalRef(g.ID, start)
	}
	claimed, err := repo.ClaimedDailyGoals(ctx, clientID, refs)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch claimed goals: %w", err)
	}

	return &dto.DailyGoalsResponseDTO{
		Date:     start.Format("2006-01-02"),
		ResetsAt: end,
		Goals:    buildDailyGoalStatuses(DailyGoals, p, start, claimed),
	}, http.StatusOK, nil
}

// ClaimDailyGoal pays a completed goal's reward once per day.
// 400 unknown or not-yet-completed goal, 409 already claimed.
func ClaimDailyGoal(ctx context.Context, clientID int, req *dto.DailyGoalClaimRequestDTO) (*dto.DailyGoalClaimResponseDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	var goal *DailyGoal
	for i := range DailyGoals {
		if DailyGoals[i].ID == req.GoalID {
			goal = &DailyGoals[i]
		}
	}
	if goal == nil {
		return nil, http.StatusBadRequest, errors.New("unknown daily goal")
	}

	start, end := dayBoundsUTC(dailyGoalsNow())
	pay, err := repo.ClaimDailyGoal(ctx, clientID, dailyGoalRef(goal.ID, start),
		goal.Currency, goal.Reward, start, end, goal.met)
	switch {
	case errors.Is(err, repo.ErrDailyGoalNotMet):
		return nil, http.StatusBadRequest, err
	case errors.Is(err, repo.ErrDailyGoalAlreadyClaimed):
		return nil, http.StatusConflict, err
	case errors.Is(err, repo.ErrProfileNotFound):
		return nil, http.StatusNotFound, err
	case err != nil:
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to claim daily goal: %w", err)
	}

	return &dto.DailyGoalClaimResponseDTO{
		GoalID: goal.ID, Currency: goal.Currency, Reward: goal.Reward,
		Coins: pay.Coins, Gems: pay.Gems,
	}, http.StatusOK, nil
}
