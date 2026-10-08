package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"gorm.io/gorm"
)

const (
	// Default time limit per question in seconds. It is retained for session
	// behavior, but it does not affect MCQ points or rewards.
	DefaultTimeLimitSec = 15

	MCQCorrectAnswerPoints = 10
	MCQCompletionXP        = 10
	MCQCorrectAnswerXP     = 5
	MCQPerfectBonusXP      = 15
	MCQCompletionCoins     = 5
	MCQCorrectAnswerCoins  = 2
	MCQPerfectBonusCoins   = 10

	SuddenDeathStreakSize        = 3
	SuddenDeathStreakPoints      = 5
	SuddenDeathStreakXP          = 5
	SuddenDeathStreakCoins       = 3
	SuddenDeathFullRunBonusXP    = 15
	SuddenDeathFullRunBonusCoins = 10
)

// SessionInactivityTTL is the maximum allowed duration of inactivity (5 minutes) before a quiz session is abandoned.
const SessionInactivityTTL = 5 * time.Minute

// CheckAndAbandonIfExpired inspects if an active session has been inactive for longer than SessionInactivityTTL.
// If expired, it transitions the status to "abandoned", sets EndedAt, persists the change, removes any Redis key,
// and returns true.
func CheckAndAbandonIfExpired(ctx context.Context, session *models.GameSession) bool {
	if session == nil || session.Status != models.SessionStatusInProgress {
		return false
	}

	lastActivity := session.UpdatedAt
	if lastActivity.IsZero() {
		lastActivity = session.StartedAt
	}
	if lastActivity.IsZero() {
		lastActivity = session.CreatedAt
	}

	if time.Since(lastActivity) > SessionInactivityTTL {
		now := time.Now().UTC()
		session.Status = models.SessionStatusAbandoned
		if session.IsSuddenDeath() {
			session.EndReason = "abandoned"
			session.CompletedSuccessfully = false
		}
		session.EndedAt = &now
		if err := repo.UpdateSession(ctx, session); err != nil {
			log.Printf("[SessionTTL] Failed to persist abandonment for session %s: %v\n", session.ID, err)
			return false
		}

		if config.RedisClient != nil {
			_ = config.RedisClient.Del(ctx, fmt.Sprintf("quiz:session:%s", session.ID)).Err()
		}
		return true
	}
	return false
}

// RefreshSessionTTL sets or refreshes the 5-minute inactivity TTL in Redis.
func RefreshSessionTTL(ctx context.Context, sessionID string) {
	if config.RedisClient != nil && sessionID != "" {
		_ = config.RedisClient.Set(ctx, fmt.Sprintf("quiz:session:%s", sessionID), "active", SessionInactivityTTL).Err()
	}
}

// ClearSessionTTL deletes the session key from Redis.
func ClearSessionTTL(ctx context.Context, sessionID string) {
	if config.RedisClient != nil && sessionID != "" {
		_ = config.RedisClient.Del(ctx, fmt.Sprintf("quiz:session:%s", sessionID)).Err()
	}
}

// StartSessionTTLSweeper runs a periodic background sweeper that marks inactive sessions (>5m) as abandoned.
func StartSessionTTLSweeper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count, err := repo.AbandonInactiveSessions(ctx, SessionInactivityTTL)
				if err != nil {
					log.Printf("[SessionTTLSweeper] Error sweeping inactive sessions: %v\n", err)
				} else if count > 0 {
					log.Printf("[SessionTTLSweeper] Abandoned %d inactive quiz sessions (> %v without activity)\n", count, SessionInactivityTTL)
				}
			}
		}
	}()
}

// --- Session Management ---

// CreateSession initializes a new quiz session, picks random questions from the DB,
// and returns the session ID + sanitized questions (no correct answers exposed).
func CreateSession(ctx context.Context, clientID int, req *dto.CreateSessionRequestDTO) (*dto.SessionCreatedResponseDTO, int, error) {
	// Check for existing active session
	existing, err := repo.GetActiveSessionByClientID(ctx, clientID)
	if err == nil && existing != nil {
		if CheckAndAbandonIfExpired(ctx, existing) {
			// Previous session was inactive for > 5 min and has been auto-abandoned.
			// Client can now start a fresh session smoothly.
		} else if req.AbandonStale {
			now := time.Now().UTC()
			existing.Status = models.SessionStatusAbandoned
			if existing.IsSuddenDeath() {
				existing.EndReason = "abandoned"
				existing.CompletedSuccessfully = false
			}
			existing.EndedAt = &now
			if err := repo.UpdateSession(ctx, existing); err != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to abandon active session: %w", err)
			}
			ClearSessionTTL(ctx, existing.ID)
		} else {
			return nil, http.StatusConflict, errors.New("an active session already exists; complete or abandon it first")
		}
	}

	// Determine target question type for this mode
	targetType := models.GameModeMCQ
	if req.GameMode == models.GameModeSuddenDeath {
		targetType = models.GameModeSuddenDeath
	}

	// Validate topic has enough questions
	count, err := repo.CountQuestionsByTopicAndMode(ctx, req.TopicID, targetType)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to check question count: %w", err)
	}
	if count == 0 {
		return nil, http.StatusNotFound, errors.New("no questions available for the selected topic")
	}
	if int(count) < req.QuestionCount {
		req.QuestionCount = int(count)
	}

	// Fetch questions: use specific requested codes if provided, otherwise random selection
	var questions []models.Question
	if len(req.QuestionCodes) > 0 {
		var err error
		questions, err = repo.GetQuestionsByCodes(ctx, req.QuestionCodes)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch requested questions: %w", err)
		}
		if req.GameMode == models.GameModeSuddenDeath {
			if len(questions) != len(req.QuestionCodes) {
				return nil, http.StatusBadRequest, errors.New("all requested question_codes must exist and belong to sudden_death")
			}
			for _, q := range questions {
				if q.QuestionType != models.GameModeSuddenDeath {
					return nil, http.StatusBadRequest, fmt.Errorf("question %s is not a sudden_death question", q.QuestionCode)
				}
			}
		}
	}
	if len(questions) == 0 {
		var err error
		questions, err = repo.GetQuestionsByTopicAndMode(ctx, req.TopicID, targetType, req.QuestionCount)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch questions: %w", err)
		}
		// Shuffle the order of randomly selected questions
		rand.Shuffle(len(questions), func(i, j int) {
			questions[i], questions[j] = questions[j], questions[i]
		})
	}

	if req.GameMode == models.GameModeSuddenDeath {
		if err := ValidateSuddenDeathQuestions(questions); err != nil {
			return nil, http.StatusInternalServerError, err
		}
	}

	// Shuffle options for each question and create SessionQuestion objects
	sessionQuestions := make([]models.SessionQuestion, 0, len(questions))
	questionDTOs := make([]dto.QuestionDTO, 0, len(questions))
	for _, q := range questions {
		sq := ShuffleQuestion(q)
		sessionQuestions = append(sessionQuestions, sq)
		// Existing MCQ clients retain the historical response. Sudden Death never
		// exposes its authoritative answer before the WebSocket grades it.
		questionDTOs = append(questionDTOs, SessionQuestionToDTO(sq, req.GameMode != models.GameModeSuddenDeath))
	}

	// Create session record with serialized QuestionsState
	session := &models.GameSession{
		ClientID:       clientID,
		TopicID:        req.TopicID,
		GameMode:       req.GameMode,
		TotalQuestions: len(sessionQuestions),
		Status:         models.SessionStatusInProgress,
	}
	if err := session.SetQuestions(sessionQuestions); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to serialize session questions: %w", err)
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to create session: %w", err)
	}

	RefreshSessionTTL(ctx, session.ID)

	return &dto.SessionCreatedResponseDTO{
		Session:                 session.ID,
		Topic:                   session.TopicID,
		GameMode:                session.GameMode,
		TotalQuestions:          session.TotalQuestions,
		TimeLimitSec:            DefaultTimeLimitSec,
		TimeLimitPerQuestionSec: DefaultTimeLimitSec,
		Questions:               questionDTOs,
	}, http.StatusCreated, nil
}

// AbandonSession marks an active session as abandoned.
func AbandonSession(ctx context.Context, clientID int, sessionID string) (int, error) {
	session, err := repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, errors.New("session not found")
		}
		return http.StatusInternalServerError, fmt.Errorf("failed to fetch session: %w", err)
	}
	if session.ClientID != clientID {
		return http.StatusForbidden, errors.New("session does not belong to this user")
	}
	if session.Status != models.SessionStatusInProgress {
		return http.StatusConflict, errors.New("session is already completed or abandoned")
	}
	now := time.Now().UTC()
	session.Status = models.SessionStatusAbandoned
	if session.IsSuddenDeath() {
		session.EndReason = "abandoned"
		session.CompletedSuccessfully = false
	}
	session.EndedAt = &now
	if err := repo.UpdateSession(ctx, session); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to abandon session: %w", err)
	}
	ClearSessionTTL(ctx, sessionID)
	return http.StatusOK, nil
}

// --- Answer Evaluation ---

// EvaluateAnswer grades a player's answer, records it in history, updates session state,
// and returns the result with gamification data.
func EvaluateAnswer(ctx context.Context, clientID int, req *dto.SubmitAnswerRequestDTO) (*dto.AnswerResultDTO, int, error) {
	// 1. Fetch and validate session ownership
	session, err := repo.GetSessionByID(ctx, req.Session)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusNotFound, errors.New("session not found")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch session: %w", err)
	}
	if session.ClientID != clientID {
		return nil, http.StatusForbidden, errors.New("session does not belong to this user")
	}
	if session.Status != models.SessionStatusInProgress {
		return nil, http.StatusConflict, errors.New("session is not active")
	}
	// Check if session has expired due to 5 minutes of inactivity
	if CheckAndAbandonIfExpired(ctx, session) {
		return nil, http.StatusConflict, errors.New("quiz session expired due to 5 minutes of inactivity and has been abandoned")
	}

	// 2. Prevent duplicate answers for the same question in this session
	already, err := repo.HasAnsweredQuestion(ctx, req.Session, req.Question)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to check answer history: %w", err)
	}
	if already {
		return nil, http.StatusConflict, errors.New("question has already been answered in this session")
	}

	// 3. Find question in session state (fallback to DB question if session state is empty)
	var questionExplanation *string

	sessionQuestions, _ := session.GetQuestions()
	var matchedSQ *models.SessionQuestion
	for _, sq := range sessionQuestions {
		if sq.QuestionCode == req.Question {
			matchedSQ = &sq
			break
		}
	}

	var fallbackQ *models.Question
	if matchedSQ != nil {
		questionExplanation = matchedSQ.Explanation
	} else {
		// Fallback to database
		var err error
		fallbackQ, err = repo.GetQuestionByCode(ctx, req.Question)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, http.StatusNotFound, errors.New("question not found")
			}
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch question: %w", err)
		}
		questionExplanation = fallbackQ.Explanation
	}

	// 4. Grade the answer with robust multi-layered verification
	isCorrect, isSkipped, resolvedCorrectOption := GradeAnswer(matchedSQ, fallbackQ, req.Option, req.SelectedText)

	// 5. Award fixed MCQ points after the backend has evaluated correctness.
	pointsEarned := 0
	coinsEarned := 0

	if isCorrect {
		session.ComboStreak++
		if session.ComboStreak > session.BestStreak {
			session.BestStreak = session.ComboStreak
		}
		session.CorrectCount++
		if session.IsSuddenDeath() {
			pointsEarned = CalculateSuddenDeathAnswerPoints(true, session.ComboStreak)
			if session.ComboStreak%SuddenDeathStreakSize == 0 {
				session.StreakMilestones++
			}
		} else {
			pointsEarned = CalculateMCQAnswerPoints(true)
		}
	} else {
		session.ComboStreak = 0
		if isSkipped && session.IsSuddenDeath() {
			session.SkippedCount++
		}
	}

	session.Score += pointsEarned
	session.CurrentIdx++

	// 6. Record the answer in the immutable history table
	historyRecord := &models.UserQuestionHistory{
		ClientID:       clientID,
		SessionID:      req.Session,
		QuestionCode:   req.Question,
		SelectedOption: req.Option,
		IsCorrect:      isCorrect,
		TimeTakenMs:    req.TimeTakenMs,
		PointsAwarded:  pointsEarned,
		CoinsAwarded:   coinsEarned,
	}
	if err := repo.RecordAnswer(ctx, historyRecord); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to record answer: %w", err)
	}

	// 7. Persist updated session state
	if err := repo.UpdateSession(ctx, session); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to update session: %w", err)
	}

	RefreshSessionTTL(ctx, session.ID)

	return &dto.AnswerResultDTO{
		Question:      req.Question,
		Option:        req.Option,
		CorrectOption: strings.ToLower(resolvedCorrectOption),
		IsCorrect:     isCorrect,
		IsSkipped:     isSkipped,
		Explanation:   questionExplanation,
		PointsEarned:  pointsEarned,
		CoinsEarned:   coinsEarned,
		ComboStreak:   session.ComboStreak,
		TotalScore:    session.Score,
	}, http.StatusOK, nil
}

// --- 50:50 Power-Up ---

// ApplyFiftyFifty returns two incorrect options to hide, ensuring the correct answer
// and one randomly chosen wrong answer remain visible.
func ApplyFiftyFifty(ctx context.Context, clientID int, req *dto.FiftyFiftyRequestDTO) (*dto.FiftyFiftyResponseDTO, int, error) {
	// Validate session ownership
	session, err := repo.GetSessionByID(ctx, req.Session)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusNotFound, errors.New("session not found")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch session: %w", err)
	}
	if session.ClientID != clientID {
		return nil, http.StatusForbidden, errors.New("session does not belong to this user")
	}
	if session.Status != models.SessionStatusInProgress {
		return nil, http.StatusConflict, errors.New("session is not active")
	}
	if session.IsSuddenDeath() {
		return nil, http.StatusBadRequest, errors.New("fifty_fifty is not supported for sudden_death")
	}

	// Check if session has expired due to 5 minutes of inactivity
	if CheckAndAbandonIfExpired(ctx, session) {
		return nil, http.StatusConflict, errors.New("quiz session expired due to 5 minutes of inactivity and has been abandoned")
	}

	RefreshSessionTTL(ctx, session.ID)

	// Fetch question from session state (or DB fallback) to identify correct option
	var correct string
	sessionQuestions, _ := session.GetQuestions()
	var matchedSQ *models.SessionQuestion
	for _, sq := range sessionQuestions {
		if sq.QuestionCode == req.Question {
			matchedSQ = &sq
			break
		}
	}
	if matchedSQ != nil {
		correct = strings.ToLower(strings.TrimSpace(matchedSQ.CorrectOption))
	} else {
		question, err := repo.GetQuestionByCode(ctx, req.Question)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, http.StatusNotFound, errors.New("question not found")
			}
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch question: %w", err)
		}
		correct = strings.ToLower(strings.TrimSpace(question.CorrectOption))
	}

	// Pick 2 wrong options to hide (guaranteeing correct option is never hidden)
	wrongOptions := make([]string, 0, 3)
	for _, opt := range []string{"a", "b", "c", "d"} {
		if !strings.EqualFold(opt, correct) {
			wrongOptions = append(wrongOptions, opt)
		}
	}
	// Shuffle and pick 2 to hide
	rand.Shuffle(len(wrongOptions), func(i, j int) {
		wrongOptions[i], wrongOptions[j] = wrongOptions[j], wrongOptions[i]
	})
	hidden := wrongOptions[:2]

	return &dto.FiftyFiftyResponseDTO{
		Question:      req.Question,
		HiddenOptions: hidden,
	}, http.StatusOK, nil
}

// --- Session Completion ---

// CompleteSession finalizes the session, calculates total rewards,
// and atomically updates the wallet ledger and player profile.
func CompleteSession(ctx context.Context, clientID int, req *dto.CompleteSessionRequestDTO) (*dto.SessionCompleteResponseDTO, int, error) {
	session, err := repo.GetSessionByID(ctx, req.Session)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusNotFound, errors.New("session not found")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch session: %w", err)
	}
	if session.ClientID != clientID {
		return nil, http.StatusForbidden, errors.New("session does not belong to this user")
	}
	if session.Status != models.SessionStatusInProgress {
		return nil, http.StatusConflict, errors.New("session is already completed or abandoned")
	}

	// Check if session has expired due to 5 minutes of inactivity
	if CheckAndAbandonIfExpired(ctx, session) {
		return nil, http.StatusConflict, errors.New("quiz session expired due to 5 minutes of inactivity and has been abandoned")
	}

	// Recompute the final score from backend-owned correctness so sessions
	// always finish under the deterministic MCQ rule, including sessions that
	// may have started before a scoring deployment.
	finalScore := session.CorrectCount * MCQCorrectAnswerPoints
	completedSuccessfully := session.CurrentIdx >= session.TotalQuestions
	if session.IsSuddenDeath() {
		finalScore += session.StreakMilestones * SuddenDeathStreakPoints
		session.CompletedSuccessfully = completedSuccessfully
		if completedSuccessfully {
			session.EndReason = "completed"
		}
	}
	session.Score = finalScore
	reward := CalculateGameRewards(finalScore, session.CorrectCount, session.TotalQuestions)
	if session.IsSuddenDeath() {
		reward = CalculateSuddenDeathRewards(session.CorrectCount, session.TotalQuestions, session.StreakMilestones, completedSuccessfully)
	}

	// Fetch current profile for level calculation
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch profile: %w", err)
	}
	oldLevel := profile.Level
	newExperience := profile.Experience + reward.XP
	newLevel := CalculateNewLevel(newExperience)
	didLevelUp := newLevel > oldLevel

	var levelUpReward *dto.LevelUpRewardDTO
	levelBonusCoins := 0
	levelBonusGems := 0
	if didLevelUp {
		levelsGained := newLevel - oldLevel
		levelBonusCoins = levelsGained * 50
		levelBonusGems = levelsGained
		levelUpReward = &dto.LevelUpRewardDTO{
			Coins: levelBonusCoins,
			Gems:  levelBonusGems,
		}
	}

	// Atomic finalization: session + ledger + profile update
	if err := repo.FinalizeSession(
		ctx,
		session,
		reward.Coins+levelBonusCoins,
		reward.XP,
		reward.Gems+levelBonusGems,
	); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to finalize session: %w", err)
	}
	ClearSessionTTL(ctx, session.ID)

	// If level changed, update level column separately
	if didLevelUp {
		profile.Level = newLevel
		_ = updateProfileLevel(ctx, clientID, newLevel)
	}

	accuracy := 0.0
	if session.TotalQuestions > 0 {
		accuracy = math.Round(float64(session.CorrectCount)/float64(session.TotalQuestions)*10000) / 100
	}

	nextLevelExp := CalculateExperienceForLevel(newLevel)

	return &dto.SessionCompleteResponseDTO{
		Session:            session.ID,
		TotalQuestions:     session.TotalQuestions,
		CorrectCount:       session.CorrectCount,
		AccuracyPercentage: accuracy,
		FinalScore:         finalScore,
		MaxScore:           CalculateMaxScoreForMode(session.GameMode, session.TotalQuestions),
		CoinsAwarded:       reward.Coins,
		XPAwarded:          reward.XP,
		GemsAwarded:        reward.Gems,
		ScoreBreakdown: map[string]int{
			"correct_answer_points": finalScore,
		},
		CoinBreakdown: reward.CoinBreakdown,
		XPBreakdown:   reward.XPBreakdown,
		LevelUpReward: levelUpReward,
		Level: dto.LevelInfoDTO{
			Current:      newLevel,
			DidLevelUp:   didLevelUp,
			Experience:   newExperience,
			NextLevelExp: nextLevelExp,
		},
		Streak: dto.StreakInfoDTO{
			CurrentStreak:  profile.CurrentStreak,
			TodayCompleted: true,
		},
	}, http.StatusOK, nil
}

// --- Scoring Formulas ---

// CalculatePoints computes deterministic MVP points for a correct answer.
// Difficulty, speed, combo streaks, and power-ups do not modify score yet.
func CalculatePoints(_, _, _, _ int) int {
	return MCQCorrectAnswerPoints
}

// CalculateMCQAnswerPoints applies the complete per-answer MCQ score rule.
// Wrong and skipped answers are both represented by isCorrect=false.
func CalculateMCQAnswerPoints(isCorrect bool) int {
	if !isCorrect {
		return 0
	}
	return MCQCorrectAnswerPoints
}

// CalculateSuddenDeathAnswerPoints adds a milestone bonus on every third
// consecutive correct answer without changing MCQ scoring.
func CalculateSuddenDeathAnswerPoints(isCorrect bool, comboStreak int) int {
	if !isCorrect {
		return 0
	}
	points := MCQCorrectAnswerPoints
	if comboStreak > 0 && comboStreak%SuddenDeathStreakSize == 0 {
		points += SuddenDeathStreakPoints
	}
	return points
}

// CalculateCoins is retained for API compatibility. MCQ coins are awarded only
// on session completion, not per question.
func CalculateCoins(_ int) int {
	return 0
}

type GameRewardSummary struct {
	Coins         int
	XP            int
	Gems          int
	XPBreakdown   map[string]int
	CoinBreakdown map[string]int
}

// CalculateGameRewards computes deterministic completion rewards.
func CalculateGameRewards(_ int, correctCount, totalQuestions int) GameRewardSummary {
	xpBreakdown := map[string]int{
		"completion_xp":     MCQCompletionXP,
		"correct_answer_xp": correctCount * MCQCorrectAnswerXP,
		"perfect_bonus_xp":  0,
	}
	coinBreakdown := map[string]int{
		"completion_coins":     MCQCompletionCoins,
		"correct_answer_coins": correctCount * MCQCorrectAnswerCoins,
		"perfect_bonus_coins":  0,
	}

	if totalQuestions > 0 && correctCount == totalQuestions {
		xpBreakdown["perfect_bonus_xp"] = MCQPerfectBonusXP
		coinBreakdown["perfect_bonus_coins"] = MCQPerfectBonusCoins
	}

	xp := 0
	for _, value := range xpBreakdown {
		xp += value
	}
	coins := 0
	for _, value := range coinBreakdown {
		coins += value
	}

	gems := 0
	if totalQuestions > 0 {
		accuracy := float64(correctCount) / float64(totalQuestions)
		if accuracy >= 1.0 {
			gems = 3
		} else if accuracy >= 0.8 {
			gems = 1
		}
	}

	return GameRewardSummary{
		Coins:         coins,
		XP:            xp,
		Gems:          gems,
		XPBreakdown:   xpBreakdown,
		CoinBreakdown: coinBreakdown,
	}
}

// CalculateSuddenDeathRewards applies only the agreed Sudden Death economy.
// A full-run bonus is awarded for reaching the end, including runs containing
// skips, while gems retain the existing accuracy-based behavior.
func CalculateSuddenDeathRewards(correctCount, totalQuestions, streakMilestones int, completedSuccessfully bool) GameRewardSummary {
	xpBreakdown := map[string]int{
		"participation_xp":    MCQCompletionXP,
		"correct_answer_xp":   correctCount * MCQCorrectAnswerXP,
		"streak_milestone_xp": streakMilestones * SuddenDeathStreakXP,
		"full_run_bonus_xp":   0,
	}
	coinBreakdown := map[string]int{
		"participation_coins":    MCQCompletionCoins,
		"correct_answer_coins":   correctCount * MCQCorrectAnswerCoins,
		"streak_milestone_coins": streakMilestones * SuddenDeathStreakCoins,
		"full_run_bonus_coins":   0,
	}
	if completedSuccessfully {
		xpBreakdown["full_run_bonus_xp"] = SuddenDeathFullRunBonusXP
		coinBreakdown["full_run_bonus_coins"] = SuddenDeathFullRunBonusCoins
	}

	xp := 0
	for _, value := range xpBreakdown {
		xp += value
	}
	coins := 0
	for _, value := range coinBreakdown {
		coins += value
	}

	gems := 0
	if totalQuestions > 0 {
		accuracy := float64(correctCount) / float64(totalQuestions)
		if accuracy >= 1.0 {
			gems = 3
		} else if accuracy >= 0.8 {
			gems = 1
		}
	}

	return GameRewardSummary{
		Coins:         coins,
		XP:            xp,
		Gems:          gems,
		XPBreakdown:   xpBreakdown,
		CoinBreakdown: coinBreakdown,
	}
}

func CalculateMaxScore(totalQuestions int) int {
	if totalQuestions <= 0 {
		return 0
	}
	return totalQuestions * MCQCorrectAnswerPoints
}

// CalculateMaxScoreForMode preserves MCQ scoring while including Sudden Death
// streak milestones at questions 3, 6, 9, and so on.
func CalculateMaxScoreForMode(gameMode string, totalQuestions int) int {
	maxScore := CalculateMaxScore(totalQuestions)
	if gameMode == models.GameModeSuddenDeath && totalQuestions > 0 {
		maxScore += (totalQuestions / SuddenDeathStreakSize) * SuddenDeathStreakPoints
	}
	return maxScore
}

// CalculateNewLevel determines the player's level based on total experience.
func CalculateNewLevel(totalExperience int) int {
	level := 1
	for {
		required := CalculateExperienceForLevel(level)
		if totalExperience < required {
			break
		}
		totalExperience -= required
		level++
	}
	return level
}

// updateProfileLevel updates only the level column on the profile.
func updateProfileLevel(ctx context.Context, clientID, newLevel int) error {
	return repo.UpdateProfileLevel(ctx, clientID, newLevel)
}

// --- Question Shuffling & Sanitization ---

type rawOption struct {
	origLetter string
	text       string
}

// ShuffleQuestion randomly permutes options A, B, C, D, recalculates the correct_option letter,
// and preserves both the original correct letter and the correct answer text.
func ShuffleQuestion(q models.Question) models.SessionQuestion {
	origCorrectLetter := strings.ToLower(strings.TrimSpace(q.CorrectOption))

	// Resolve the true correct text from the unshuffled options
	var correctText string
	switch origCorrectLetter {
	case "a":
		correctText = q.OptionA
	case "b":
		correctText = q.OptionB
	case "c":
		correctText = q.OptionC
	case "d":
		correctText = q.OptionD
	default:
		// If CorrectOption is not 'a'/'b'/'c'/'d', check if it directly contains the answer text
		for _, candidate := range []string{q.OptionA, q.OptionB, q.OptionC, q.OptionD} {
			if strings.EqualFold(strings.TrimSpace(q.CorrectOption), strings.TrimSpace(candidate)) {
				correctText = candidate
				break
			}
		}
		if correctText == "" {
			correctText = q.OptionA
			origCorrectLetter = "a"
		}
	}

	var rawOptions []rawOption
	if strings.TrimSpace(q.OptionA) != "" {
		rawOptions = append(rawOptions, rawOption{origLetter: "a", text: q.OptionA})
	}
	if strings.TrimSpace(q.OptionB) != "" {
		rawOptions = append(rawOptions, rawOption{origLetter: "b", text: q.OptionB})
	}
	if strings.TrimSpace(q.OptionC) != "" {
		rawOptions = append(rawOptions, rawOption{origLetter: "c", text: q.OptionC})
	}
	if strings.TrimSpace(q.OptionD) != "" {
		rawOptions = append(rawOptions, rawOption{origLetter: "d", text: q.OptionD})
	}

	rand.Shuffle(len(rawOptions), func(i, j int) {
		rawOptions[i], rawOptions[j] = rawOptions[j], rawOptions[i]
	})

	letters := []string{"a", "b", "c", "d"}
	var newCorrect string
	for i, opt := range rawOptions {
		if strings.EqualFold(opt.origLetter, origCorrectLetter) || (correctText != "" && strings.EqualFold(strings.TrimSpace(opt.text), strings.TrimSpace(correctText))) {
			newCorrect = letters[i]
			break
		}
	}
	if newCorrect == "" {
		newCorrect = "a"
	}

	sq := models.SessionQuestion{
		QuestionCode:    q.QuestionCode,
		Prompt:          q.Prompt,
		CorrectOption:   newCorrect,
		OriginalCorrect: origCorrectLetter,
		CorrectText:     correctText,
		Difficulty:      q.Difficulty,
		Points:          q.Points,
		Hint:            q.Hint,
		Explanation:     q.Explanation,
	}
	if len(rawOptions) > 0 {
		sq.OptionA = rawOptions[0].text
	}
	if len(rawOptions) > 1 {
		sq.OptionB = rawOptions[1].text
	}
	if len(rawOptions) > 2 {
		sq.OptionC = rawOptions[2].text
	}
	if len(rawOptions) > 3 {
		sq.OptionD = rawOptions[3].text
	}

	return sq
}

// GradeAnswer evaluates against the option mapping actually served to the
// player. selectedText and OriginalCorrect never override a session option.
// The database fallback remains for legacy sessions without persisted state.
func GradeAnswer(sq *models.SessionQuestion, q *models.Question, option string, _ string) (isCorrect bool, isSkipped bool, resolvedCorrectOption string) {
	cleanOption := strings.ToLower(strings.TrimSpace(option))
	if cleanOption == "skip" {
		if sq != nil {
			return false, true, strings.ToLower(sq.CorrectOption)
		}
		if q != nil {
			return false, true, strings.ToLower(strings.TrimSpace(q.CorrectOption))
		}
		return false, true, ""
	}

	if sq != nil {
		resolvedCorrectOption = strings.ToLower(strings.TrimSpace(sq.CorrectOption))

		// The served/shuffled slot is authoritative. OriginalCorrect is metadata
		// only, and selectedText cannot override a contradictory option letter.
		if cleanOption == "a" || cleanOption == "b" || cleanOption == "c" || cleanOption == "d" {
			return strings.EqualFold(cleanOption, resolvedCorrectOption), false, resolvedCorrectOption
		}

		// Preserve the documented full-text option form safely by resolving the
		// text back to its served slot before comparing that slot.
		servedOptions := []string{sq.OptionA, sq.OptionB, sq.OptionC, sq.OptionD}
		letters := []string{"a", "b", "c", "d"}
		for i, text := range servedOptions {
			if text != "" && strings.EqualFold(strings.TrimSpace(option), strings.TrimSpace(text)) {
				return letters[i] == resolvedCorrectOption, false, resolvedCorrectOption
			}
		}
		return false, false, resolvedCorrectOption
	}

	if q != nil {
		resolvedCorrectOption = strings.ToLower(strings.TrimSpace(q.CorrectOption))
		var correctText string
		switch resolvedCorrectOption {
		case "a":
			correctText = q.OptionA
		case "b":
			correctText = q.OptionB
		case "c":
			correctText = q.OptionC
		case "d":
			correctText = q.OptionD
		}

		if cleanOption == "a" || cleanOption == "b" || cleanOption == "c" || cleanOption == "d" {
			return strings.EqualFold(cleanOption, resolvedCorrectOption), false, resolvedCorrectOption
		}
		if correctText != "" {
			if strings.EqualFold(strings.TrimSpace(option), strings.TrimSpace(correctText)) {
				return true, false, resolvedCorrectOption
			}
		}

		return false, false, resolvedCorrectOption
	}

	return false, false, ""
}

// ValidateSuddenDeathQuestions enforces the two-option contract without
// constraining the shared four-option MCQ model.
func ValidateSuddenDeathQuestions(questions []models.Question) error {
	for _, q := range questions {
		if q.QuestionType != models.GameModeSuddenDeath {
			return fmt.Errorf("question %s is not a sudden_death question", q.QuestionCode)
		}
		options := map[string]string{
			"a": q.OptionA,
			"b": q.OptionB,
			"c": q.OptionC,
			"d": q.OptionD,
		}
		nonEmpty := 0
		for _, text := range options {
			if strings.TrimSpace(text) != "" {
				nonEmpty++
			}
		}
		correct := strings.ToLower(strings.TrimSpace(q.CorrectOption))
		if nonEmpty != 2 || strings.TrimSpace(options[correct]) == "" {
			return fmt.Errorf("sudden_death question %s must have exactly two non-empty options and one valid correct option", q.QuestionCode)
		}
	}
	return nil
}

// SessionQuestionToDTO converts a SessionQuestion into a client-safe DTO.
func SessionQuestionToDTO(sq models.SessionQuestion, includeCorrect bool) dto.QuestionDTO {
	opts := make([]dto.OptionDTO, 0, 4)
	if strings.TrimSpace(sq.OptionA) != "" {
		opts = append(opts, dto.OptionDTO{Option: "a", Text: sq.OptionA})
	}
	if strings.TrimSpace(sq.OptionB) != "" {
		opts = append(opts, dto.OptionDTO{Option: "b", Text: sq.OptionB})
	}
	if strings.TrimSpace(sq.OptionC) != "" {
		opts = append(opts, dto.OptionDTO{Option: "c", Text: sq.OptionC})
	}
	if strings.TrimSpace(sq.OptionD) != "" {
		opts = append(opts, dto.OptionDTO{Option: "d", Text: sq.OptionD})
	}

	d := dto.QuestionDTO{
		Question:     sq.QuestionCode,
		Prompt:       sq.Prompt,
		Points:       MCQCorrectAnswerPoints,
		Hint:         sq.Hint,
		TimeLimitSec: DefaultTimeLimitSec,
		Options:      opts,
	}
	if includeCorrect {
		d.CorrectOption = strings.ToLower(sq.CorrectOption)
	}
	return d
}

// GetTopicQuestions fetches questions for a topic, shuffles their order and options, and returns them as sanitized DTOs.
func GetTopicQuestions(ctx context.Context, topicID string, limit int) (*dto.TopicQuestionsResponseDTO, int, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	questions, err := repo.GetQuestionsByTopic(ctx, topicID, limit)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch questions: %w", err)
	}
	if len(questions) == 0 {
		return nil, http.StatusNotFound, errors.New("no questions found for the selected topic")
	}

	// Shuffle questions order
	rand.Shuffle(len(questions), func(i, j int) {
		questions[i], questions[j] = questions[j], questions[i]
	})

	questionDTOs := make([]dto.QuestionDTO, 0, len(questions))
	for _, q := range questions {
		sq := ShuffleQuestion(q)
		questionDTOs = append(questionDTOs, SessionQuestionToDTO(sq, true))
	}

	return &dto.TopicQuestionsResponseDTO{
		Topic:                   topicID,
		Total:                   len(questionDTOs),
		TimeLimitSec:            DefaultTimeLimitSec,
		TimeLimitPerQuestionSec: DefaultTimeLimitSec,
		Questions:               questionDTOs,
	}, http.StatusOK, nil
}
