package services

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/repository"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/utils"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// -----------------------------------------------------------------------------
// LOGIN SERVICES
// -----------------------------------------------------------------------------

func LoginGenOTPService(ctx context.Context, req *dto.LoginGenOTPRequest) (*dto.LoginGenOTPResponse, int, error) {
	if req.Email == "" || req.Password == "" {
		return nil, http.StatusBadRequest, errors.New("email and password required")
	}

	// 1. Fetch client using repository layer
	clientRecord, err := repository.GetClientByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusUnauthorized, errors.New("invalid email or password")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("database query error: %w", err)
	}

	// 2. Check allowed statuses
	if clientRecord.Status != "active" && clientRecord.Status != "pending" {
		return nil, http.StatusUnauthorized, errors.New("account not active or blocked")
	}

	// 3. Verify password
	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	combined := req.Password + secret

	if err := bcrypt.CompareHashAndPassword([]byte(clientRecord.Password), []byte(combined)); err != nil {
		return nil, http.StatusUnauthorized, errors.New("invalid email or password")
	}

	// 4. Check OTP request cooldown in Redis (30 seconds)
	cooldownKey := "otp-cooldown:" + req.Email
	if config.RedisClient != nil {
		cooldownExists, err := config.RedisClient.Exists(ctx, cooldownKey).Result()
		if err == nil && cooldownExists > 0 {
			return nil, http.StatusTooManyRequests, errors.New("please wait 30 seconds before requesting another OTP")
		}
	}

	// 5. Generate secure OTP (6-digit)
	otp, err := utils.GenerateOtp(6)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to generate secure OTP")
	}

	// 6. Store OTP in Redis with a 1-minute expiration
	if config.RedisClient != nil {
		err = config.RedisClient.Set(ctx, "otp:"+req.Email, otp, 1*time.Minute).Err()
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to save OTP: %w", err)
		}
		_ = config.RedisClient.Set(ctx, cooldownKey, "active", 30*time.Second).Err()
	}

	phoneStr := ""
	if clientRecord.Phone != nil {
		phoneStr = *clientRecord.Phone
	}

	// Log OTP prominently
	log.Printf("================================================================")
	log.Printf("[LOGIN OTP] Generated OTP: >>> %s <<< for Email: %s (Phone: %s)", otp, req.Email, phoneStr)
	log.Printf("================================================================")

	return &dto.LoginGenOTPResponse{
		Phone:   phoneStr,
		Message: "OTP sent successfully",
	}, http.StatusOK, nil
}

func LoginResendOTPService(ctx context.Context, req *dto.LoginResendOTPRequest) (*dto.LoginGenOTPResponse, int, error) {
	if req.Email == "" {
		return nil, http.StatusBadRequest, errors.New("email is required")
	}

	// 1. Fetch client from database
	clientRecord, err := repository.GetClientByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusUnauthorized, errors.New("invalid email")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("database query error: %w", err)
	}

	// 2. Check allowed statuses
	if clientRecord.Status != "active" && clientRecord.Status != "pending" {
		return nil, http.StatusUnauthorized, errors.New("account not active or blocked")
	}

	// 3. Check lockout due to 5 prior failed attempts
	lockoutKey := "block:otp-verify:" + req.Email
	if config.RedisClient != nil {
		isLocked, err := config.RedisClient.Exists(ctx, lockoutKey).Result()
		if err == nil && isLocked > 0 {
			return nil, http.StatusTooManyRequests, errors.New("too many failed attempts, please try again in 10 minutes")
		}

		// 4. Check OTP request cooldown in Redis (30 seconds)
		cooldownKey := "otp-cooldown:" + req.Email
		cooldownExists, err := config.RedisClient.Exists(ctx, cooldownKey).Result()
		if err == nil && cooldownExists > 0 {
			return nil, http.StatusTooManyRequests, errors.New("please wait 30 seconds before requesting another OTP")
		}
	}

	// 5. Generate secure OTP
	otp, err := utils.GenerateOtp(6)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to generate secure OTP")
	}

	// 6. Overwrite OTP in Redis with 1-minute expiration
	if config.RedisClient != nil {
		err = config.RedisClient.Set(ctx, "otp:"+req.Email, otp, 1*time.Minute).Err()
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to save OTP: %w", err)
		}
		_ = config.RedisClient.Set(ctx, "otp-cooldown:"+req.Email, "active", 30*time.Second).Err()
	}

	phoneStr := ""
	if clientRecord.Phone != nil {
		phoneStr = *clientRecord.Phone
	}

	// Log Resent OTP prominently
	log.Printf("================================================================")
	log.Printf("[LOGIN OTP RESEND] Resent OTP: >>> %s <<< for Email: %s (Phone: %s)", otp, req.Email, phoneStr)
	log.Printf("================================================================")

	return &dto.LoginGenOTPResponse{
		Phone:   phoneStr,
		Message: "OTP resent successfully",
	}, http.StatusOK, nil
}

func LoginOTPVerifyService(ctx context.Context, req *dto.LoginOTPVerifyRequest) (string, string, int, error) {
	if req.Email == "" || req.OTP == "" {
		return "", "", http.StatusBadRequest, errors.New("email and OTP required")
	}

	// 1. Check lockout
	lockoutKey := "block:otp-verify:" + req.Email
	if config.RedisClient != nil {
		isLocked, err := config.RedisClient.Exists(ctx, lockoutKey).Result()
		if err == nil && isLocked > 0 {
			return "", "", http.StatusTooManyRequests, errors.New("too many failed attempts, please try again in 10 minutes")
		}
	}

	// 2. Get OTP from Redis
	storedOTP := ""
	if config.RedisClient != nil {
		var err error
		storedOTP, err = config.RedisClient.Get(ctx, "otp:"+req.Email).Result()
		if err != nil {
			return "", "", http.StatusUnauthorized, errors.New("no OTP generated or expired")
		}
	}

	// 3. Constant-time comparison
	if subtle.ConstantTimeCompare([]byte(storedOTP), []byte(req.OTP)) != 1 {
		if config.RedisClient != nil {
			attemptsKey := "failed-otp-attempts:login:" + req.Email
			attempts, _ := config.RedisClient.Incr(ctx, attemptsKey).Result()
			if attempts == 1 {
				config.RedisClient.Expire(ctx, attemptsKey, 10*time.Minute)
			}

			if attempts >= 5 {
				config.RedisClient.Set(ctx, lockoutKey, "locked", 10*time.Minute)
				config.RedisClient.Del(ctx, "otp:"+req.Email)
				config.RedisClient.Del(ctx, attemptsKey)
				return "", "", http.StatusTooManyRequests, errors.New("too many incorrect attempts. Your OTP has been invalidated, please try again in 10 minutes")
			}

			remaining := 5 - attempts
			return "", "", http.StatusUnauthorized, fmt.Errorf("the OTP you entered is incorrect. %d attempts remaining", remaining)
		}
		return "", "", http.StatusUnauthorized, errors.New("invalid OTP")
	}

	// 4. Clean up OTP and failed attempts
	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, "otp:"+req.Email).Err()
		_ = config.RedisClient.Del(ctx, "failed-otp-attempts:login:"+req.Email).Err()
	}

	// 5. Fetch Client from Database
	clientRecord, err := repository.GetClientByEmail(ctx, req.Email)
	if err != nil {
		return "", "", http.StatusUnauthorized, errors.New("client not found")
	}

	// 6. Generate JWT Tokens
	jwtSecret := ""
	if config.AppConfig != nil {
		jwtSecret = config.AppConfig.Server.JWTSecret
	}
	if jwtSecret == "" {
		return "", "", http.StatusInternalServerError, errors.New("JWT secret is not configured")
	}

	jti, err := utils.GenerateToken64()
	if err != nil {
		return "", "", http.StatusInternalServerError, fmt.Errorf("failed to generate token ID: %w", err)
	}

	accessExpiry := 30 * 24 * time.Hour
	if config.AppConfig != nil && config.AppConfig.Server.JWTAccessTokenExpiry > 0 {
		accessExpiry = config.AppConfig.Server.JWTAccessTokenExpiry
	}
	refreshExpiry := 180 * 24 * time.Hour
	if config.AppConfig != nil && config.AppConfig.Server.JWTRefreshTokenExpiry > 0 {
		refreshExpiry = config.AppConfig.Server.JWTRefreshTokenExpiry
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": clientRecord.ID,
		"email":     clientRecord.Email,
		"exp":       time.Now().Add(accessExpiry).Unix(),
		"jti":       jti,
	})
	accessStr, err := accessToken.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", http.StatusInternalServerError, fmt.Errorf("failed to sign access token: %w", err)
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": clientRecord.ID,
		"email":     clientRecord.Email,
		"exp":       time.Now().Add(refreshExpiry).Unix(),
	})
	refreshStr, err := refreshToken.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", http.StatusInternalServerError, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	// 7. Persist active refresh token
	err = repository.UpdateClientRefreshToken(ctx, clientRecord.ID, &refreshStr)
	if err != nil {
		return "", "", http.StatusInternalServerError, fmt.Errorf("failed to save refresh token to database: %w", err)
	}

	return accessStr, refreshStr, http.StatusOK, nil
}

// -----------------------------------------------------------------------------
// SESSION & TOKEN SERVICES
// -----------------------------------------------------------------------------

func LogoutClientService(ctx context.Context, clientID int, jti string, exp int64) (int, error) {
	err := repository.UpdateClientRefreshToken(ctx, clientID, nil)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to clear refresh token in database: %w", err)
	}

	// Blacklist JTI in Redis until expiration
	if jti != "" && exp > 0 && config.RedisClient != nil {
		ttl := time.Until(time.Unix(exp, 0))
		if ttl > 0 {
			err = config.RedisClient.Set(ctx, "blacklist:"+jti, "revoked", ttl).Err()
			if err != nil {
				return http.StatusInternalServerError, fmt.Errorf("failed to blacklist token JTI: %w", err)
			}
		}
	}

	return http.StatusOK, nil
}

func RefreshTokenService(ctx context.Context, tokenStr string) (string, int, error) {
	if tokenStr == "" {
		return "", http.StatusBadRequest, errors.New("refresh token required")
	}

	jwtSecret := ""
	if config.AppConfig != nil {
		jwtSecret = config.AppConfig.Server.JWTSecret
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return "", http.StatusUnauthorized, errors.New("invalid or expired refresh token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", http.StatusUnauthorized, errors.New("invalid token payload")
	}

	clientIDVal, ok := claims["client_id"]
	if !ok {
		return "", http.StatusUnauthorized, errors.New("invalid token payload (client_id missing)")
	}
	clientID := int(clientIDVal.(float64))

	clientRecord, err := repository.GetClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", http.StatusUnauthorized, errors.New("client not found")
		}
		return "", http.StatusInternalServerError, fmt.Errorf("failed to fetch client: %w", err)
	}

	if clientRecord.RefreshToken == nil || *clientRecord.RefreshToken != tokenStr {
		return "", http.StatusUnauthorized, errors.New("invalid session or refresh token. please log in again")
	}

	jti, err := utils.GenerateToken64()
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("failed to generate token ID: %w", err)
	}

	accessExpiry := 30 * 24 * time.Hour
	if config.AppConfig != nil && config.AppConfig.Server.JWTAccessTokenExpiry > 0 {
		accessExpiry = config.AppConfig.Server.JWTAccessTokenExpiry
	}

	newAccessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": clientRecord.ID,
		"email":     clientRecord.Email,
		"exp":       time.Now().Add(accessExpiry).Unix(),
		"jti":       jti,
	})
	newAccessStr, err := newAccessToken.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("failed to sign access token: %w", err)
	}

	return newAccessStr, http.StatusOK, nil
}

func ClientVerifySessionService(ctx context.Context, clientID int) (*dto.ClientVerifySessionResponse, int, error) {
	clientRecord, err := repository.GetClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &dto.ClientVerifySessionResponse{Active: false}, http.StatusOK, nil
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("database query error: %w", err)
	}

	return &dto.ClientVerifySessionResponse{
		Active: true,
		Status: clientRecord.Status,
	}, http.StatusOK, nil
}

func ClientSessionService(ctx context.Context, clientID int) (*dto.ClientSessionResponse, int, error) {
	clientRecord, err := repository.GetClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusNotFound, errors.New("client not found")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("database query error: %w", err)
	}

	return &dto.ClientSessionResponse{
		ID:        clientRecord.ID,
		Username:  clientRecord.Username,
		Email:     clientRecord.Email,
		Phone:     clientRecord.Phone,
		Status:    clientRecord.Status,
		CreatedAt: clientRecord.CreatedAt,
		UpdatedAt: clientRecord.UpdatedAt,
	}, http.StatusOK, nil
}

// -----------------------------------------------------------------------------
// PASSWORD CHANGE SERVICES
// -----------------------------------------------------------------------------

func ChangePasswordRequestService(ctx context.Context, clientID int, req *dto.ChangePasswordRequest) (string, int, error) {
	if req.OldPassword == "" || req.NewPassword == "" {
		return "", http.StatusBadRequest, errors.New("both oldPassword and newPassword are required")
	}

	clientRecord, err := repository.GetClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", http.StatusNotFound, errors.New("client not found")
		}
		return "", http.StatusInternalServerError, fmt.Errorf("database error: %w", err)
	}

	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	combined := req.OldPassword + secret
	if err := bcrypt.CompareHashAndPassword([]byte(clientRecord.Password), []byte(combined)); err != nil {
		return "", http.StatusUnauthorized, errors.New("incorrect old password")
	}

	otpCode, err := utils.GenerateOtp(6)
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("failed to generate verification OTP")
	}

	stash := dto.ChangePasswordStash{
		OTP:         otpCode,
		NewPassword: req.NewPassword,
	}
	stashBytes, err := json.Marshal(stash)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("failed to serialize password change details: %w", err)
	}

	if config.RedisClient != nil {
		key := "change-password:" + strconv.Itoa(clientID)
		err = config.RedisClient.Set(ctx, key, string(stashBytes), 2*time.Minute).Err()
		if err != nil {
			return "", http.StatusInternalServerError, fmt.Errorf("failed to save verification details to Redis: %w", err)
		}
	}

	// Log Change Password OTP prominently
	log.Printf("================================================================")
	log.Printf("[CHANGE PASSWORD OTP] Generated OTP: >>> %s <<< for Client ID: %d (Email: %s)", otpCode, clientID, clientRecord.Email)
	log.Printf("================================================================")

	return otpCode, http.StatusOK, nil
}

func ChangePasswordVerifyService(ctx context.Context, clientID int, req *dto.ChangePasswordVerifyRequest) (int, error) {
	if req.OTP == "" {
		return http.StatusBadRequest, errors.New("OTP is required")
	}

	key := "change-password:" + strconv.Itoa(clientID)
	stashJSON := ""
	if config.RedisClient != nil {
		var err error
		stashJSON, err = config.RedisClient.Get(ctx, key).Result()
		if err != nil {
			return http.StatusUnauthorized, errors.New("no OTP found or expired. Request password change first")
		}
	}

	var stash dto.ChangePasswordStash
	if err := json.Unmarshal([]byte(stashJSON), &stash); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to parse verification details: %w", err)
	}

	if req.OTP != stash.OTP {
		return http.StatusUnauthorized, errors.New("invalid OTP")
	}

	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, key).Err()
	}

	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	combined := stash.NewPassword + secret
	hashed, err := bcrypt.GenerateFromPassword([]byte(combined), 12)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to hash new password: %w", err)
	}

	err = repository.UpdateClientPassword(ctx, clientID, string(hashed))
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to save new password to database: %w", err)
	}

	log.Printf("[CHANGE PASSWORD] Successfully updated password for Client ID: %d\n", clientID)

	return http.StatusOK, nil
}

// -----------------------------------------------------------------------------
// FORGOT / RESET PASSWORD SERVICES
// -----------------------------------------------------------------------------

func ForgotPasswordRequestService(ctx context.Context, req *dto.ForgotPasswordRequest) (string, int, error) {
	if req.Email == "" {
		return "", http.StatusBadRequest, errors.New("email is required")
	}

	clientRecord, err := repository.GetClientByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Don't leak user existence; return standard success message
			return "", http.StatusOK, nil
		}
		return "", http.StatusInternalServerError, fmt.Errorf("database error: %w", err)
	}

	// Generate secure 64-char reset token
	resetToken, err := utils.GenerateToken64()
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("failed to generate reset token")
	}

	// Store in Redis with 15-minute expiration
	if config.RedisClient != nil {
		err = config.RedisClient.Set(ctx, "forgot-password:"+resetToken, strconv.Itoa(clientRecord.ID), 15*time.Minute).Err()
		if err != nil {
			return "", http.StatusInternalServerError, fmt.Errorf("failed to save reset token in Redis: %w", err)
		}
	}

	// Log Forgot Password Token prominently
	log.Printf("================================================================")
	log.Printf("[FORGOT PASSWORD TOKEN] Generated Reset Token: >>> %s <<< for Email: %s", resetToken, req.Email)
	log.Printf("================================================================")

	return resetToken, http.StatusOK, nil
}

func ForgotPasswordVerifyTokenService(ctx context.Context, req *dto.ForgotPasswordVerifyTokenRequest) (int, error) {
	if req.Token == "" {
		return http.StatusBadRequest, errors.New("token is required")
	}

	if config.RedisClient != nil {
		exists, err := config.RedisClient.Exists(ctx, "forgot-password:"+req.Token).Result()
		if err != nil || exists == 0 {
			return http.StatusUnauthorized, errors.New("invalid or expired reset token")
		}
	}

	return http.StatusOK, nil
}

func ResetPasswordVerifyService(ctx context.Context, req *dto.ResetPasswordVerifyRequest) (int, error) {
	if req.Token == "" || req.NewPassword == "" {
		return http.StatusBadRequest, errors.New("token and newPassword are required")
	}

	clientIDStr := ""
	if config.RedisClient != nil {
		var err error
		clientIDStr, err = config.RedisClient.Get(ctx, "forgot-password:"+req.Token).Result()
		if err != nil || clientIDStr == "" {
			return http.StatusUnauthorized, errors.New("invalid or expired reset token")
		}
	}

	clientID, err := strconv.Atoi(clientIDStr)
	if err != nil {
		return http.StatusInternalServerError, errors.New("invalid client ID stored in reset token")
	}

	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	combined := req.NewPassword + secret
	hashed, err := bcrypt.GenerateFromPassword([]byte(combined), 12)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to hash password: %w", err)
	}

	if err := repository.UpdateClientPassword(ctx, clientID, string(hashed)); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to update password: %w", err)
	}

	// Invalidate reset token and refresh token
	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, "forgot-password:"+req.Token).Err()
	}
	_ = repository.UpdateClientRefreshToken(ctx, clientID, nil)

	log.Printf("[RESET PASSWORD] Successfully reset password for Client ID: %d\n", clientID)

	return http.StatusOK, nil
}

// -----------------------------------------------------------------------------
// REGISTRATION OTP SERVICES
// -----------------------------------------------------------------------------

func RegisterEmailRequestService(ctx context.Context, req *dto.RegisterEmailRequest) (string, int, error) {
	if req.Email == "" {
		return "", http.StatusBadRequest, errors.New("email is required")
	}

	exists, err := repository.CheckEmailExists(ctx, req.Email)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("database query error: %w", err)
	}
	if exists {
		return "", http.StatusConflict, errors.New("email already in use")
	}

	otp, err := utils.GenerateOtp(6)
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("failed to generate OTP")
	}

	if config.RedisClient != nil {
		err = config.RedisClient.Set(ctx, "register-email-otp:"+req.Email, otp, 5*time.Minute).Err()
		if err != nil {
			return "", http.StatusInternalServerError, fmt.Errorf("failed to save OTP in Redis: %w", err)
		}
	}

	// Log Registration Email OTP prominently
	log.Printf("================================================================")
	log.Printf("[REGISTER EMAIL OTP] Generated OTP: >>> %s <<< for Email: %s", otp, req.Email)
	log.Printf("================================================================")

	return otp, http.StatusOK, nil
}

func RegisterEmailVerifyService(ctx context.Context, req *dto.RegisterEmailVerifyRequest) (int, error) {
	if req.Email == "" || req.OTP == "" {
		return http.StatusBadRequest, errors.New("email and OTP required")
	}

	storedOTP := ""
	if config.RedisClient != nil {
		var err error
		storedOTP, err = config.RedisClient.Get(ctx, "register-email-otp:"+req.Email).Result()
		if err != nil {
			return http.StatusUnauthorized, errors.New("no OTP requested or expired")
		}
	}

	if subtle.ConstantTimeCompare([]byte(storedOTP), []byte(req.OTP)) != 1 {
		return http.StatusUnauthorized, errors.New("invalid OTP")
	}

	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, "register-email-otp:"+req.Email).Err()
	}

	return http.StatusOK, nil
}

func RegisterPhoneOtpRequestService(ctx context.Context, req *dto.RegisterPhoneRequest) (string, int, error) {
	if req.Phone == "" {
		return "", http.StatusBadRequest, errors.New("phone is required")
	}

	otp, err := utils.GenerateOtp(6)
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("failed to generate OTP")
	}

	if config.RedisClient != nil {
		err = config.RedisClient.Set(ctx, "register-phone-otp:"+req.Phone, otp, 5*time.Minute).Err()
		if err != nil {
			return "", http.StatusInternalServerError, fmt.Errorf("failed to save OTP in Redis: %w", err)
		}
	}

	// Log Registration Phone OTP prominently
	log.Printf("================================================================")
	log.Printf("[REGISTER PHONE OTP] Generated OTP: >>> %s <<< for Phone: %s", otp, req.Phone)
	log.Printf("================================================================")

	return otp, http.StatusOK, nil
}

func RegisterPhoneOtpVerifyService(ctx context.Context, req *dto.RegisterPhoneVerifyRequest) (int, error) {
	if req.Phone == "" || req.OTP == "" {
		return http.StatusBadRequest, errors.New("phone and OTP required")
	}

	storedOTP := ""
	if config.RedisClient != nil {
		var err error
		storedOTP, err = config.RedisClient.Get(ctx, "register-phone-otp:"+req.Phone).Result()
		if err != nil {
			return http.StatusUnauthorized, errors.New("no OTP requested or expired")
		}
	}

	if subtle.ConstantTimeCompare([]byte(storedOTP), []byte(req.OTP)) != 1 {
		return http.StatusUnauthorized, errors.New("invalid OTP")
	}

	if config.RedisClient != nil {
		_ = config.RedisClient.Del(ctx, "register-phone-otp:"+req.Phone).Err()
	}

	return http.StatusOK, nil
}

func RegisterValidateBasicService(ctx context.Context, req *dto.RegisterValidateBasicRequest) (int, error) {
	exists, err := repository.CheckEmailExists(ctx, req.Email)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to check email: %w", err)
	}
	if exists {
		return http.StatusConflict, errors.New("email already in use")
	}

	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Server.PasswordSecret
	}
	combined := req.Password + secret
	hashed, err := bcrypt.GenerateFromPassword([]byte(combined), 12)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to hash password: %w", err)
	}

	newClient := models.Client{
		Username: req.Username,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: string(hashed),
		Status:   "active",
	}

	if err := repository.CreateClient(ctx, &newClient); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to create client: %w", err)
	}

	return http.StatusCreated, nil
}

func CheckEmailVerifyService(ctx context.Context, email string) (bool, int, error) {
	exists, err := repository.CheckEmailExists(ctx, email)
	if err != nil {
		return false, http.StatusInternalServerError, fmt.Errorf("failed to check email: %w", err)
	}
	return exists, http.StatusOK, nil
}
