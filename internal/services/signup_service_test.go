package services

import (
	"context"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
)

func TestSignupCooldown_InMemory(t *testing.T) {
	ctx := context.Background()
	testEmail := "cooldown_test@example.com"

	// Initial check: not on cooldown
	rem, onCooldown := checkSignupCooldown(ctx, testEmail)
	if onCooldown || rem != 0 {
		t.Fatalf("expected not on cooldown initially, got rem=%d, onCooldown=%v", rem, onCooldown)
	}

	// Set 60-second cooldown
	setSignupCooldown(ctx, testEmail, 60*time.Second)

	// Second check: on cooldown
	rem, onCooldown = checkSignupCooldown(ctx, testEmail)
	if !onCooldown || rem <= 0 || rem > 60 {
		t.Fatalf("expected on cooldown with rem between 1 and 60, got rem=%d, onCooldown=%v", rem, onCooldown)
	}

	// Different email should not be affected
	otherEmail := "other@example.com"
	remOther, onCooldownOther := checkSignupCooldown(ctx, otherEmail)
	if onCooldownOther || remOther != 0 {
		t.Fatalf("other email should not be on cooldown, got rem=%d, onCooldown=%v", remOther, onCooldownOther)
	}
}

func TestSignupRateLimit_InMemory(t *testing.T) {
	ctx := context.Background()
	testEmail := "ratelimit_test@example.com"

	// Window of 1 minute, max 3 requests for testing
	maxReqs := 3
	window := 1 * time.Minute

	for i := 1; i <= maxReqs; i++ {
		allowed := checkAndIncrementSignupRateLimit(ctx, testEmail, maxReqs, window)
		if !allowed {
			t.Fatalf("request %d should have been allowed", i)
		}
	}

	// 4th request must be rejected
	blocked := checkAndIncrementSignupRateLimit(ctx, testEmail, maxReqs, window)
	if blocked {
		t.Fatalf("request beyond limit should have been blocked")
	}
}

func TestPendingSignup_StorageAndAttemptLimits(t *testing.T) {
	ctx := context.Background()
	testEmail := "pending_test@example.com"

	data := &dto.PendingSignupData{
		Username:     "testuser",
		DisplayName:  "testuser",
		Email:        testEmail,
		PasswordHash: "hashed_secret_pw",
		OTP:          "123456",
		Attempts:     0,
		CreatedAt:    time.Now().UTC(),
	}

	// 1. Store pending registration
	err := storePendingSignup(ctx, data, 5*time.Minute)
	if err != nil {
		t.Fatalf("storePendingSignup failed: %v", err)
	}

	// 2. Retrieve pending registration
	retrieved, found, err := getPendingSignup(ctx, testEmail)
	if err != nil || !found || retrieved == nil {
		t.Fatalf("expected to find stored pending signup, err=%v, found=%v", err, found)
	}
	if retrieved.OTP != "123456" || retrieved.Username != "testuser" {
		t.Errorf("retrieved data mismatch: %+v", retrieved)
	}

	// 3. Increment attempts 1..4
	for i := 1; i <= 4; i++ {
		attempts := incrementPendingSignupAttempts(ctx, testEmail)
		if attempts != i {
			t.Errorf("expected attempts=%d, got %d", i, attempts)
		}
	}

	// 4. 5th failed attempt should invalidate and delete the session
	attempts := incrementPendingSignupAttempts(ctx, testEmail)
	if attempts != 5 {
		t.Errorf("expected attempts=5, got %d", attempts)
	}

	// 5. Subsequent retrieve must not find the invalidated session
	_, foundAfterMax, _ := getPendingSignup(ctx, testEmail)
	if foundAfterMax {
		t.Fatalf("expected pending signup to be deleted after 5 failed attempts")
	}
}
