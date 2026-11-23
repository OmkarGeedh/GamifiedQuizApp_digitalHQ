package services

// Entitlements: what a player has paid for, always computed on the server.
//   - Topic packs (non-consumable Play products) unlock subjects. A subject in
//     any pack is locked until the player owns one of its packs; the check runs
//     where a game starts, not in the app.
//   - The Quiz Pass (subscription) gives: rewards without watching ads, monthly
//     gems, and an extra streak-freeze slot.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// Quiz Pass perks.
const (
	PassMonthlyGems      = 60
	PassExtraFreezeSlots = 1
)

// ErrTopicLocked is matched by errors.Is for a *TopicLockedError.
var ErrTopicLocked = errors.New("this topic needs a topic pack")

// TopicLockedError names the packs that unlock a locked topic.
type TopicLockedError struct {
	TopicID    string
	ProductIDs []string
}

func (e *TopicLockedError) Error() string {
	return fmt.Sprintf("topic %s is part of a topic pack (%s)", e.TopicID, strings.Join(e.ProductIDs, ", "))
}

// Is lets errors.Is(err, ErrTopicLocked) match.
func (e *TopicLockedError) Is(target error) bool { return target == ErrTopicLocked }

// topicPackIndex caches billing_product_subjects in-process: it changes only
// when packs are seeded, and a game start must not wait on extra queries for
// free subjects.
type topicPackIndex struct {
	subjectPacks map[string][]string
	packSubjects map[string][]string
}

const topicPackCacheTTL = 5 * time.Minute

var (
	topicPackMu     sync.Mutex
	topicPackCache  *topicPackIndex
	topicPackLoaded time.Time
)

// InvalidateTopicPackCache drops the cached pack index (tests, reseeds).
func InvalidateTopicPackCache() {
	topicPackMu.Lock()
	topicPackCache = nil
	topicPackMu.Unlock()
}

func loadTopicPacks(ctx context.Context) (*topicPackIndex, error) {
	topicPackMu.Lock()
	defer topicPackMu.Unlock()
	if topicPackCache != nil && time.Since(topicPackLoaded) < topicPackCacheTTL {
		return topicPackCache, nil
	}
	rows, err := repo.ListTopicPackSubjects(ctx)
	if err != nil {
		if topicPackCache != nil {
			return topicPackCache, nil // stale beats failing a game start
		}
		if isMissingTableErr(err) {
			// Migration 000010 not applied yet: there are no packs, so nothing
			// is locked. Re-check soon instead of failing every game start.
			idx := &topicPackIndex{subjectPacks: map[string][]string{}, packSubjects: map[string][]string{}}
			topicPackCache, topicPackLoaded = idx, time.Now().Add(-topicPackCacheTTL+time.Minute)
			return idx, nil
		}
		return nil, err // any other failure: never serve a pack topic unchecked
	}
	idx := &topicPackIndex{subjectPacks: map[string][]string{}, packSubjects: map[string][]string{}}
	for _, r := range rows {
		idx.subjectPacks[r.Subject] = append(idx.subjectPacks[r.Subject], r.ProductID)
		idx.packSubjects[r.ProductID] = append(idx.packSubjects[r.ProductID], r.Subject)
	}
	topicPackCache, topicPackLoaded = idx, time.Now()
	return idx, nil
}

// isMissingTableErr reports "table billing_product_subjects does not exist"
// (PostgreSQL 42P01 or SQLite "no such table").
func isMissingTableErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "billing_product_subjects") &&
		(strings.Contains(msg, "does not exist") || strings.Contains(msg, "no such table") || strings.Contains(msg, "42P01"))
}

// CheckTopicAccess returns nil when the player may play topicID, or a
// *TopicLockedError naming the packs that unlock it. Free subjects cost no query.
func CheckTopicAccess(ctx context.Context, clientID int, topicID string) error {
	idx, err := loadTopicPacks(ctx)
	if err != nil {
		return fmt.Errorf("failed to load topic packs: %w", err)
	}
	packs := idx.subjectPacks[strings.TrimSpace(topicID)]
	if len(packs) == 0 {
		return nil
	}
	owned, err := repo.OwnedProductIDs(ctx, clientID)
	if err != nil {
		return fmt.Errorf("failed to load purchases: %w", err)
	}
	for _, p := range packs {
		for _, o := range owned {
			if p == o {
				return nil
			}
		}
	}
	return &TopicLockedError{TopicID: topicID, ProductIDs: append([]string(nil), packs...)}
}

// TopicAccessStatus maps CheckTopicAccess to an HTTP status: 402 Payment
// Required when locked (the app shows the pack; 403 would read as "signed
// out"), 503 when ownership cannot be checked (never serve a pack topic unchecked).
func TopicAccessStatus(err error) int {
	if errors.Is(err, ErrTopicLocked) {
		return http.StatusPaymentRequired
	}
	return http.StatusServiceUnavailable
}

// AnnotateSubjectAccess marks the subjects the player cannot play yet.
func AnnotateSubjectAccess(ctx context.Context, clientID int, subjects []dto.SubjectDTO) error {
	idx, err := loadTopicPacks(ctx)
	if err != nil {
		return err
	}
	if len(idx.subjectPacks) == 0 {
		return nil
	}
	owned, err := repo.OwnedProductIDs(ctx, clientID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, o := range owned {
		have[o] = true
	}
	for i := range subjects {
		packs := idx.subjectPacks[subjects[i].TopicID]
		if len(packs) == 0 {
			continue
		}
		subjects[i].PackProductIDs = append([]string(nil), packs...)
		subjects[i].Locked = true
		for _, p := range packs {
			if have[p] {
				subjects[i].Locked = false
				break
			}
		}
	}
	return nil
}

// packSubjects returns the subjects a pack unlocks (sorted).
func packSubjects(ctx context.Context, productID string) []string {
	idx, err := loadTopicPacks(ctx)
	if err != nil {
		return nil
	}
	out := append([]string(nil), idx.packSubjects[productID]...)
	sort.Strings(out)
	return out
}

// passPeriod is the calendar month (UTC) a monthly perk belongs to.
func passPeriod(now time.Time) string { return now.UTC().Format("2006-01") }

func passGemsRef(period string) string { return "pass_gems:" + period }

// GetEntitlements returns the Quiz Pass state, owned topic packs and perks.
func GetEntitlements(ctx context.Context, clientID int) (*dto.EntitlementsDTO, int, error) {
	sub, err := subscriptionStatus(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read subscription: %w", err)
	}
	idx, err := loadTopicPacks(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load topic packs: %w", err)
	}
	owned, err := repo.OwnedProductIDs(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load purchases: %w", err)
	}
	out := &dto.EntitlementsDTO{Subscription: *sub, TopicPacks: []dto.TopicPackDTO{}}
	for _, p := range owned {
		if subs, ok := idx.packSubjects[p]; ok {
			s := append([]string(nil), subs...)
			sort.Strings(s)
			out.TopicPacks = append(out.TopicPacks, dto.TopicPackDTO{ProductID: p, Subjects: s})
		}
	}
	if sub.IsPremium {
		claimed, err := repo.PassRewardClaimed(ctx, clientID, models.CurrencyGems, passGemsRef(passPeriod(billingNow())))
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to read perks: %w", err)
		}
		out.Perks = dto.PassPerksDTO{AdFreeRewards: true, MonthlyGems: PassMonthlyGems, MonthlyGemsClaimed: claimed, ExtraStreakFreezeSlots: PassExtraFreezeSlots}
	}
	return out, http.StatusOK, nil
}

// ClaimPassMonthlyGems pays the Quiz Pass monthly gems, once per calendar month.
func ClaimPassMonthlyGems(ctx context.Context, clientID int) (*dto.PassClaimResponseDTO, int, error) {
	premium, err := IsPremium(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read subscription: %w", err)
	}
	if !premium {
		return nil, http.StatusPaymentRequired, errors.New("the Quiz Pass is required")
	}
	period := passPeriod(billingNow())
	gems, err := repo.ClaimPassReward(ctx, clientID, models.CurrencyGems, PassMonthlyGems, passGemsRef(period))
	switch {
	case errors.Is(err, repo.ErrRewardAlreadyClaimed):
		return nil, http.StatusConflict, errors.New("this month's gems are already claimed")
	case errors.Is(err, repo.ErrProfileNotFound):
		return nil, http.StatusNotFound, err
	case err != nil:
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to claim gems: %w", err)
	}
	return &dto.PassClaimResponseDTO{GemsAdded: PassMonthlyGems, Gems: gems, Period: period}, http.StatusOK, nil
}

// maxStreakFreezesFor is how many freezes a player may hold.
func maxStreakFreezesFor(ctx context.Context, clientID int) int {
	if premium, err := IsPremium(ctx, clientID); err == nil && premium {
		return MaxStreakFreezes + PassExtraFreezeSlots
	}
	return MaxStreakFreezes
}
