package services

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils"
	"golang.org/x/crypto/bcrypt"
)

// -----------------------------------------------------------------------------
// SIGNUP FLOW STORAGE & RATE LIMITING HELPERS (Redis with In-Memory Fallback)
// -----------------------------------------------------------------------------

var signupMemStore = struct {
	sync.RWMutex
	cooldowns      map[string]time.Time
	rateLimits     map[string][]time.Time
	pendingSignups map[string]dto.PendingSignupData
	pendingExpires map[string]time.Time
}{
	cooldowns:      make(map[string]time.Time),
	rateLimits:     make(map[string][]time.Time),
	pendingSignups: make(map[string]dto.PendingSignupData),
	pendingExpires: make(map[string]time.Time),
}

func checkSignupCooldown(ctx context.Context, email string) (int, bool) {
	if config.RedisClient != nil {
		ttl := config.RedisClient.TTL(ctx, "signup:cooldown:"+email).Val()
		if ttl > 0 {
			secs := int(math.Ceil(ttl.Seconds()))
			if secs <= 0 {
				secs = 1
			}
			return secs, true
		}
		return 0, false
	}

	signupMemStore.RLock()
	defer signupMemStore.RUnlock()
	if exp, exists := signupMemStore.cooldowns[email]; exists {
		if time.Now().Before(exp) {
			secs := int(math.Ceil(time.Until(exp).Seconds()))
			if secs <= 0 {
				secs = 1
			}
			return secs, true
		}
	}
	return 0, false
}

func setSignupCooldown(ctx context.Context, email string, d time.Duration) {
	if config.RedisClient != nil {
		_ = config.RedisClient.Set(ctx, "signup:cooldown:"+email, "1", d).Err()
		return
	}

	signupMemStore.Lock()
	defer signupMemStore.Unlock()
	signupMemStore.cooldowns[email] = time.Now().Add(d)
}

func checkAndIncrementSignupRateLimit(ctx context.Context, email string, maxRequests int, window time.Duration) bool {
	if config.RedisClient != nil {
		key := "signup:ratelimit:" + email
		count, err := config.RedisClient.Incr(ctx, key).Result()
		if err == nil {
			if count == 1 {
				config.RedisClient.Expire(ctx, key, window)
			}
			return count <= int64(maxRequests)
		}
	}

	signupMemStore.Lock()
	defer signupMemStore.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)

	var valid []time.Time
	for _, t := range signupMemStore.rateLimits[email] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= maxRequests {
		signupMemStore.rateLimits[email] = valid
		return false
	}
	valid = append(valid, now)
	signupMemStore.rateLimits[email] = valid
	return true
}

func storePendingSignup(ctx context.Context, data *dto.PendingSignupData, ttl time.Duration) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if config.RedisClient != nil {
		return config.RedisClient.Set(ctx, "signup:pending:"+data.Email, string(bytes), ttl).Err()
	}

	signupMemStore.Lock()
	defer signupMemStore.Unlock()
	signupMemStore.pendingSignups[data.Email] = *data
	signupMemStore.pendingExpires[data.Email] = time.Now().Add(ttl)
	return nil
}

func getPendingSignup(ctx context.Context, email string) (*dto.PendingSignupData, bool, error) {
	if config.RedisClient != nil {
		val, err := config.RedisClient.Get(ctx, "signup:pending:"+email).Result()
		if err != nil {
			return nil, false, nil
		}
		var data dto.PendingSignupData
		if err := json.Unmarshal([]byte(val), &data); err != nil {
			return nil, false, err
		}
		return &data, true, nil
	}

	signupMemStore.RLock()
	defer signupMemStore.RUnlock()
	data, exists := signupMemStore.pendingSignups[email]
	if !exists {
		return nil, false, nil
	}
	exp, ok := signupMemStore.pendingExpires[email]
	if !ok || time.Now().After(exp) {
		return nil, false, nil
	}
	return &data, true, nil
}

func incrementPendingSignupAttempts(ctx context.Context, email string) int {
	data, found, err := getPendingSignup(ctx, email)
	if err != nil || !found || data == nil {
		return 0
	}
	data.Attempts++
	if data.Attempts >= 5 {
		deletePendingSignup(ctx, email)
		return data.Attempts
	}

	ttl := 10 * time.Minute
	if config.RedisClient != nil {
		remTTL := config.RedisClient.TTL(ctx, "signup:pending:"+email).Val()
		if remTTL > 0 {
			ttl = remTTL
		}
	} else {
		signupMemStore.RLock()
		if exp, ok := signupMemStore.pendingExpires[email]; ok {
			rem := time.Until(exp)
			if rem > 0 {
				ttl = rem
			}
		}
		signupMemStore.RUnlock()
	}
	_ = storePendingSignup(ctx, data, ttl)
	return data.Attempts
}

func deletePendingSignup(ctx context.Context, email string) {
	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, "signup:pending:"+email).Err()
		return
	}

	signupMemStore.Lock()
	defer signupMemStore.Unlock()
	delete(signupMemStore.pendingSignups, email)
	delete(signupMemStore.pendingExpires, email)
}

// -----------------------------------------------------------------------------
// SIGNUP FLOW SERVICES
// -----------------------------------------------------------------------------

// SignupSendCodeService handles Step 1: Account details validation, cooldown/rate limit checks,
// password hashing, OTP generation, session storage, and sending verification code.
func SignupSendCodeService(ctx context.Context, ip string, req *dto.SignupSendCodeRequest) (*dto.SignupSendCodeResponse, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// 1. Check uniqueness in PostgreSQL database
	emailExists, err := repo.CheckEmailExists(ctx, req.Email)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to verify email availability: %w", err)
	}
	if emailExists {
		return nil, http.StatusConflict, errors.New("an account with this email already exists")
	}

	userExists, err := repo.CheckUsernameExists(ctx, req.DisplayName)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to verify display name availability: %w", err)
	}
	if userExists {
		return nil, http.StatusConflict, errors.New("this display name is already taken")
	}

	// 2. Check Cooldown (60 seconds per email)
	if remaining, onCooldown := checkSignupCooldown(ctx, req.Email); onCooldown {
		return nil, http.StatusTooManyRequests, fmt.Errorf("please wait %d seconds before requesting another code", remaining)
	}

	// 3. Check Rate Limit (5 requests per 15-minute window)
	if !checkAndIncrementSignupRateLimit(ctx, req.Email, 5, 15*time.Minute) {
		return nil, http.StatusTooManyRequests, errors.New("too many verification code requests. please try again in 15 minutes")
	}

	// 4. Hash password with bcrypt and configured server secret
	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	combined := req.Password + secret
	hashed, err := bcrypt.GenerateFromPassword([]byte(combined), 12)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to process password: %w", err)
	}

	// 5. Generate secure 6-digit numeric OTP
	otp, err := utils.GenerateOtp(6)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to generate verification code")
	}

	// 6. Store pending signup in Redis/memory with 10-minute expiry
	pendingData := &dto.PendingSignupData{
		Username:     req.DisplayName,
		DisplayName:  req.DisplayName,
		Email:        req.Email,
		PasswordHash: string(hashed),
		OTP:          otp,
		Attempts:     0,
		CreatedAt:    time.Now().UTC(),
	}
	if err := storePendingSignup(ctx, pendingData, 10*time.Minute); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to store registration session: %w", err)
	}

	// 7. Set 60-second cooldown
	setSignupCooldown(ctx, req.Email, 60*time.Second)

	// 8. Prominently log verification code
	log.Printf("================================================================")
	log.Printf("[SIGNUP VERIFICATION CODE] 6-Digit Code: >>> %s <<< for %s (%s)", otp, req.Email, req.DisplayName)
	log.Printf("================================================================")

	return &dto.SignupSendCodeResponse{
		Email:           req.Email,
		CooldownSeconds: 60,
		ExpiresIn:       600,
		Message:         fmt.Sprintf("Verification code sent to %s", req.Email),
	}, http.StatusOK, nil
}

// SignupResendCodeService handles resending code with cooldown and rate limit enforcement.
func SignupResendCodeService(ctx context.Context, ip string, req *dto.SignupResendCodeRequest) (*dto.SignupSendCodeResponse, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// 1. Verify pending registration session exists
	pending, found, err := getPendingSignup(ctx, req.Email)
	if err != nil || !found || pending == nil {
		return nil, http.StatusBadRequest, errors.New("no pending registration found or verification session expired. Please fill out the registration form again")
	}

	// 2. Check Cooldown (60 seconds)
	if remaining, onCooldown := checkSignupCooldown(ctx, req.Email); onCooldown {
		return nil, http.StatusTooManyRequests, fmt.Errorf("please wait %d seconds before requesting another code", remaining)
	}

	// 3. Check Rate Limit (5 requests per 15 minutes)
	if !checkAndIncrementSignupRateLimit(ctx, req.Email, 5, 15*time.Minute) {
		return nil, http.StatusTooManyRequests, errors.New("too many verification code requests. please try again in 15 minutes")
	}

	// 4. Generate fresh 6-digit OTP
	otp, err := utils.GenerateOtp(6)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to generate verification code")
	}

	// 5. Update pending data and reset attempt counter
	pending.OTP = otp
	pending.Attempts = 0
	if err := storePendingSignup(ctx, pending, 10*time.Minute); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to refresh registration session: %w", err)
	}

	// 6. Set 60-second cooldown
	setSignupCooldown(ctx, req.Email, 60*time.Second)

	// 7. Prominently log verification code
	log.Printf("================================================================")
	log.Printf("[SIGNUP RESEND CODE] New 6-Digit Code: >>> %s <<< for %s", otp, req.Email)
	log.Printf("================================================================")

	return &dto.SignupSendCodeResponse{
		Email:           req.Email,
		CooldownSeconds: 60,
		ExpiresIn:       600,
		Message:         fmt.Sprintf("A new verification code was sent to %s", req.Email),
	}, http.StatusOK, nil
}

// SignupVerifyService handles Step 2: Code verification, PostgreSQL account creation,
// and automatic token issuance.
func SignupVerifyService(ctx context.Context, req *dto.SignupVerifyRequest) (string, string, *models.Client, int, error) {
	if err := req.Validate(); err != nil {
		return "", "", nil, http.StatusBadRequest, err
	}

	// 1. Fetch pending signup session
	pending, found, err := getPendingSignup(ctx, req.Email)
	if err != nil || !found || pending == nil {
		return "", "", nil, http.StatusBadRequest, errors.New("verification code expired or registration session not found. Please request a new code")
	}

	// 2. Check if attempts exhausted
	if pending.Attempts >= 5 {
		deletePendingSignup(ctx, req.Email)
		return "", "", nil, http.StatusTooManyRequests, errors.New("maximum verification attempts exceeded. Code has been invalidated. Please request a new code")
	}

	// 3. Constant-time comparison of OTP
	if subtle.ConstantTimeCompare([]byte(pending.OTP), []byte(req.Code)) != 1 {
		attempts := incrementPendingSignupAttempts(ctx, req.Email)
		remaining := 5 - attempts
		if remaining <= 0 {
			return "", "", nil, http.StatusBadRequest, errors.New("invalid verification code. Maximum attempts reached. Please request a new code")
		}
		return "", "", nil, http.StatusBadRequest, fmt.Errorf("invalid verification code. %d attempts remaining", remaining)
	}

	// 4. Double check unique constraints in DB before creating
	emailExists, _ := repo.CheckEmailExists(ctx, pending.Email)
	if emailExists {
		deletePendingSignup(ctx, pending.Email)
		return "", "", nil, http.StatusConflict, errors.New("an account with this email already exists")
	}
	userExists, _ := repo.CheckUsernameExists(ctx, pending.Username)
	if userExists {
		deletePendingSignup(ctx, pending.Email)
		return "", "", nil, http.StatusConflict, errors.New("display name is already taken")
	}

	// 5. Create client record in PostgreSQL
	newClient := models.Client{
		Username: pending.Username,
		Email:    pending.Email,
		Password: pending.PasswordHash,
		Status:   "active",
	}
	if err := repo.CreateClient(ctx, &newClient); err != nil {
		return "", "", nil, http.StatusInternalServerError, fmt.Errorf("failed to create client account: %w", err)
	}

	// 6. Initialize starter gamification profile (XP, coins, level)
	_, _ = GetOrCreateProfile(ctx, newClient.ID)

	// 7. Generate JWT access & refresh tokens
	accessStr, refreshStr, err := GenerateTokensForClient(ctx, &newClient)
	if err != nil {
		return "", "", nil, http.StatusInternalServerError, fmt.Errorf("failed to generate authentication tokens: %w", err)
	}

	// 8. Invalidate pending signup session and cooldown
	deletePendingSignup(ctx, req.Email)
	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, "signup:cooldown:"+req.Email).Err()
	}

	log.Printf("[SIGNUP] Account created and verified successfully: ID=%d, Username=%s, Email=%s", newClient.ID, newClient.Username, newClient.Email)

	return accessStr, refreshStr, &newClient, http.StatusCreated, nil
}
