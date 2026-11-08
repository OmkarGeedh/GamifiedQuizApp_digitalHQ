package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// StreakMilestone is a reward for reaching a streak length.
type StreakMilestone struct {
	Days     int
	Currency string
	Reward   int
}

// StreakMilestones, in ascending order of Days. Change the numbers here; nothing
// else needs to move. Each can be earned once per streak: if the streak breaks and
// the player builds a new one, the milestones can be earned again.
var StreakMilestones = []StreakMilestone{
	{Days: 3, Currency: models.CurrencyCoins, Reward: 20},
	{Days: 7, Currency: models.CurrencyCoins, Reward: 50},
	{Days: 14, Currency: models.CurrencyGems, Reward: 5},
	{Days: 30, Currency: models.CurrencyCoins, Reward: 150},
}

// Streak freezes: bought with gems, one is used up automatically for each day the
// player misses, so a short break does not reset the streak.
const (
	StreakFreezePriceGems = 10
	MaxStreakFreezes      = 2 // also the most missed days a streak can survive
)

// streakNow is replaced in tests.
var streakNow = time.Now

// bridgeMissedDays tries to keep a streak alive across missed days by spending
// one freeze per missed day (all or nothing: it never spends freezes on a streak
// it cannot save). todayDate is today at 00:00 UTC.
//
// kept reports that freezes were spent and the streak should continue. When the
// freezes were not spent because another request already handled today, fresh is
// that request's profile. When both are zero the streak simply breaks as before.
// Any freeze problem (for example the column does not exist yet) falls back to
// the old behaviour instead of failing the profile call.
func bridgeMissedDays(ctx context.Context, profile *models.Profile, todayDate time.Time) (kept bool, fresh *models.Profile) {
	if profile.LastActiveDate.IsZero() || profile.CurrentStreak < 1 {
		return false, nil
	}
	y, m, d := profile.LastActiveDate.Date()
	last := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	missed := int(todayDate.Sub(last).Hours()/24) - 1
	if missed < 1 || missed > MaxStreakFreezes {
		return false, nil
	}

	spent, err := repo.UseStreakFreezes(ctx, profile.ClientID, missed, last, todayDate)
	if err != nil {
		log.Printf("streak freeze unavailable for client %d: %v", profile.ClientID, err)
		return false, nil
	}
	if !spent {
		// Not enough freezes, or a parallel request on the same new day got here first.
		if latest, err := repo.GetProfileByClientID(ctx, profile.ClientID); err == nil &&
			latest.LastActiveDate.Format("2006-01-02") == todayDate.Format("2006-01-02") {
			return false, latest
		}
		return false, nil
	}

	// Show the bridged days as active in the week calendar.
	for i := 1; i <= missed; i++ {
		_ = repo.RecordDailyActivity(ctx, profile.ClientID, last.AddDate(0, 0, i).Format("2006-01-02"))
	}
	return true, nil
}

// streakMilestoneRef is the ledger reference for one milestone of one streak. A
// streak is identified by the day it started, so a new streak gets new references
// and its milestones can be claimed again.
func streakMilestoneRef(days int, streakStart time.Time) string {
	return fmt.Sprintf("sm:%d:%s", days, streakStart.UTC().Format("20060102"))
}

// streakStartDay is the first day of a streak that is `current` days long and
// includes `today`.
func streakStartDay(today time.Time, current int) time.Time {
	if current < 1 {
		return today
	}
	return today.AddDate(0, 0, -(current - 1))
}

// buildStreakStatus assembles the response. Pure, so it is easy to test.
func buildStreakStatus(milestones []StreakMilestone, current, highest int, streakStart, dayEnd time.Time,
	claimed map[string]bool) dto.StreakStatusDTO {

	out := dto.StreakStatusDTO{
		Current: current, Highest: highest, DayEndsAt: dayEnd,
		Milestones: make([]dto.StreakMilestoneDTO, len(milestones)),
	}
	for i, m := range milestones {
		reached := current >= m.Days
		isClaimed := claimed[streakMilestoneRef(m.Days, streakStart)]
		out.Milestones[i] = dto.StreakMilestoneDTO{
			Days: m.Days, Currency: m.Currency, Reward: m.Reward,
			Reached: reached, Claimed: reached && isClaimed, Claimable: reached && !isClaimed,
		}
		if !reached && out.NextMilestoneDays == 0 {
			out.NextMilestoneDays = m.Days
			out.DaysToNextMilestone = m.Days - current
		}
	}
	return out
}

// GetStreakStatus returns the player's streak and their milestone rewards. Like
// every profile call it also counts today as an active day.
func GetStreakStatus(ctx context.Context, clientID int) (*dto.StreakStatusDTO, int, error) {
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve streak: %w", err)
	}
	today, dayEnd := dayBoundsUTC(streakNow())
	start := streakStartDay(today, profile.CurrentStreak)

	refs := make([]string, len(StreakMilestones))
	for i, m := range StreakMilestones {
		refs[i] = streakMilestoneRef(m.Days, start)
	}
	claimed, err := repo.ClaimedStreakMilestones(ctx, clientID, refs)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch claimed streak rewards: %w", err)
	}

	status := buildStreakStatus(StreakMilestones, profile.CurrentStreak, profile.HighestStreak, start, dayEnd, claimed)

	// Freezes: if the database has no freeze column yet, leave them switched off.
	if freezes, err := repo.GetStreakFreezes(ctx, clientID); err != nil {
		log.Printf("streak freezes unavailable for client %d: %v", clientID, err)
	} else {
		status.FreezesEnabled = true
		status.Freezes = freezes
		status.MaxFreezes = MaxStreakFreezes
		status.FreezePriceGems = StreakFreezePriceGems
	}
	return &status, http.StatusOK, nil
}

// BuyStreakFreeze sells the player one streak freeze for gems.
// 400 not enough gems, 409 already holding the maximum.
func BuyStreakFreeze(ctx context.Context, clientID int) (*dto.StreakFreezeBuyResponseDTO, int, error) {
	out, err := repo.BuyStreakFreeze(ctx, clientID, StreakFreezePriceGems, MaxStreakFreezes)
	switch {
	case errors.Is(err, repo.ErrInsufficientFunds):
		return nil, http.StatusBadRequest, fmt.Errorf("not enough gems: a streak freeze costs %d", StreakFreezePriceGems)
	case errors.Is(err, repo.ErrStreakFreezeLimit):
		return nil, http.StatusConflict, err
	case errors.Is(err, repo.ErrProfileNotFound):
		return nil, http.StatusNotFound, err
	case err != nil:
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to buy streak freeze: %w", err)
	}
	return &dto.StreakFreezeBuyResponseDTO{Freezes: out.Freezes, Gems: out.Gems}, http.StatusOK, nil
}

// ClaimStreakMilestone pays a reached milestone's reward once per streak.
// 400 unknown or not-yet-reached milestone, 409 already claimed.
func ClaimStreakMilestone(ctx context.Context, clientID int, req *dto.StreakClaimRequestDTO) (*dto.StreakClaimResponseDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	var milestone *StreakMilestone
	for i := range StreakMilestones {
		if StreakMilestones[i].Days == req.Days {
			milestone = &StreakMilestones[i]
		}
	}
	if milestone == nil {
		return nil, http.StatusBadRequest, errors.New("unknown streak milestone")
	}

	// Brings the streak up to date first (a streak the player let lapse resets).
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve streak: %w", err)
	}
	today, _ := dayBoundsUTC(streakNow())
	start := streakStartDay(today, profile.CurrentStreak)

	pay, err := repo.ClaimStreakMilestone(ctx, clientID, streakMilestoneRef(milestone.Days, start),
		milestone.Currency, milestone.Reward, milestone.Days)
	switch {
	case errors.Is(err, repo.ErrStreakMilestoneNotReached):
		return nil, http.StatusBadRequest, err
	case errors.Is(err, repo.ErrRewardAlreadyClaimed):
		return nil, http.StatusConflict, err
	case errors.Is(err, repo.ErrProfileNotFound):
		return nil, http.StatusNotFound, err
	case err != nil:
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to claim streak reward: %w", err)
	}

	return &dto.StreakClaimResponseDTO{
		Days: milestone.Days, Currency: milestone.Currency, Reward: milestone.Reward,
		Coins: pay.Coins, Gems: pay.Gems,
	}, http.StatusOK, nil
}
