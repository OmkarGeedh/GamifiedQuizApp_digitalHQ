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

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"golang.org/x/sync/errgroup"
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
		_ = repo.UpdateSession(ctx, session)
		_ = repo.ClearClientActiveSession(ctx, session.ClientID)
		_ = repo.InvalidateCachedSession(ctx, session.ID)

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

	// Special flow for Sort It Out rapid-fire swipe mini-game
	if req.GameMode == models.GameModeSortItOut {
		pairType := req.PairType
		if pairType == "" {
			pairTypes, err := repo.GetDistinctPairTypes(ctx, req.TopicID, models.GameModeSortItOut)
			if err != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch pair types: %w", err)
			}
			if len(pairTypes) == 0 {
				return nil, http.StatusNotFound, errors.New("no sort_it_out questions available for the selected topic")
			}
			pairType = pairTypes[rand.IntN(len(pairTypes))]
		}

		var questions []models.SortItOutQuestion
		if len(req.QuestionCodes) > 0 {
			var err error
			questions, err = repo.GetSortItOutQuestionsByCodes(ctx, req.QuestionCodes)
			if err != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch requested questions: %w", err)
			}
			if len(questions) != len(req.QuestionCodes) {
				return nil, http.StatusBadRequest, errors.New("all requested question_codes must exist")
			}
			if pairType == "" && len(questions) > 0 {
				pairType = questions[0].PairType
			}
		} else {
			if req.QuestionCount <= 0 {
				req.QuestionCount = 10
			}
			var err error
			questions, err = repo.GetSortItOutQuestions(ctx, req.TopicID, pairType, req.QuestionCount)
			if err != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch questions: %w", err)
			}
			if len(questions) == 0 {
				return nil, http.StatusNotFound, fmt.Errorf("no sort_it_out questions found for pair type '%s'", pairType)
			}
			rand.Shuffle(len(questions), func(i, j int) {
				questions[i], questions[j] = questions[j], questions[i]
			})
		}

		timeLimitPerQuestion := req.TimeLimitPerQuestionSec
		if timeLimitPerQuestion < 5 || timeLimitPerQuestion > 10 {
			timeLimitPerQuestion = 7
		}

		sessionQuestions := make([]models.SessionQuestion, 0, len(questions))
		questionDTOs := make([]dto.QuestionDTO, 0, len(questions))
		for _, q := range questions {
			correctOpt := strings.ToLower(strings.TrimSpace(q.CorrectOption))
			var correctText string
			if correctOpt == "a" {
				correctText = q.OptionA
			} else if correctOpt == "b" {
				correctText = q.OptionB
			}
			sq := models.SessionQuestion{
				QuestionCode:    q.QuestionCode,
				Prompt:          q.Prompt,
				OptionA:         q.OptionA,
				OptionB:         q.OptionB,
				PairType:        q.PairType,
				CorrectOption:   correctOpt,
				OriginalCorrect: correctOpt,
				CorrectText:     correctText,
				Difficulty:      q.Difficulty,
				Points:          MCQCorrectAnswerPoints,
				Hint:            q.Hint,
				Explanation:     q.Explanation,
			}
			sessionQuestions = append(sessionQuestions, sq)

			opts := []dto.OptionDTO{
				{Option: "a", Text: sq.OptionA},
				{Option: "b", Text: sq.OptionB},
			}
			questionDTOs = append(questionDTOs, dto.QuestionDTO{
				Question:      sq.QuestionCode,
				Prompt:        sq.Prompt,
				Points:        MCQCorrectAnswerPoints,
				Hint:          sq.Hint,
				TimeLimitSec:  timeLimitPerQuestion,
				Options:       opts,
				CorrectOption: sq.CorrectOption,
				PairType:      sq.PairType,
				Explanation:   sq.Explanation,
			})
		}

		session := &models.GameSession{
			ClientID:       clientID,
			TopicID:        req.TopicID,
			GameMode:       req.GameMode,
			PairType:       pairType,
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
			PairType:                session.PairType,
			TotalQuestions:          session.TotalQuestions,
			TimeLimitSec:            timeLimitPerQuestion * len(sessionQuestions),
			TimeLimitPerQuestionSec: timeLimitPerQuestion,
			Questions:               questionDTOs,
		}, http.StatusCreated, nil
	}

	// Special flow for Match the Following multi-board matching puzzle
	if req.GameMode == models.GameModeMatchTheFollowing {
		var questions []models.MatchTheFollowingQuestion
		if len(req.QuestionCodes) > 0 {
			var err error
			questions, err = repo.GetMatchTheFollowingQuestionsByCodes(ctx, req.QuestionCodes)
			if err != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch requested questions: %w", err)
			}
			if len(questions) != len(req.QuestionCodes) {
				return nil, http.StatusBadRequest, errors.New("all requested question_codes must exist")
			}
		} else {
			if req.QuestionCount <= 0 {
				req.QuestionCount = 3
			}
			var err error
			questions, err = repo.GetMatchTheFollowingQuestions(ctx, req.TopicID, req.QuestionCount)
			if err != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch questions: %w", err)
			}
			if len(questions) == 0 {
				return nil, http.StatusNotFound, errors.New("no match_the_following questions found for the selected topic")
			}
			rand.Shuffle(len(questions), func(i, j int) {
				questions[i], questions[j] = questions[j], questions[i]
			})
			if len(questions) > req.QuestionCount {
				questions = questions[:req.QuestionCount]
			}
		}

		timeLimitPerQuestion := req.TimeLimitPerQuestionSec
		if timeLimitPerQuestion < 15 || timeLimitPerQuestion > 45 {
			timeLimitPerQuestion = 25
		}

		questionIDs := make([]string, len(questions))
		for i, q := range questions {
			questionIDs[i] = q.ID
		}
		pairsMap, err := repo.GetMatchPairsByQuestionIDs(ctx, questionIDs)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch match pairs: %w", err)
		}

		sessionQuestions := make([]models.SessionQuestion, 0, len(questions))
		boardDTOs := make([]dto.MatchBoardDTO, 0, len(questions))

		for _, q := range questions {
			pairs := pairsMap[q.ID]
			leftItems := make([]dto.MatchPairItemDTO, 0, len(pairs))
			rightItems := make([]dto.MatchPairItemDTO, 0, len(pairs))
			solMap := make(map[string]string, len(pairs))

			for _, p := range pairs {
				leftID := fmt.Sprintf("%s_l", p.ID)
				rightID := fmt.Sprintf("%s_r", p.ID)
				leftItems = append(leftItems, dto.MatchPairItemDTO{
					ID:   leftID,
					Text: p.LeftText,
				})
				rightItems = append(rightItems, dto.MatchPairItemDTO{
					ID:   rightID,
					Text: p.RightText,
				})
				solMap[leftID] = rightID
			}

			// Shuffle right items to prevent across-the-row matching
			rand.Shuffle(len(rightItems), func(i, j int) {
				rightItems[i], rightItems[j] = rightItems[j], rightItems[i]
			})

			sq := models.SessionQuestion{
				QuestionCode: q.QuestionCode,
				Prompt:       q.Prompt,
				Difficulty:   q.Difficulty,
				Points:       40,
				Hint:         q.Hint,
				Explanation:  q.Explanation,
				MatchPairs:   pairs,
			}
			sessionQuestions = append(sessionQuestions, sq)

			boardDTOs = append(boardDTOs, dto.MatchBoardDTO{
				QuestionCode: q.QuestionCode,
				Prompt:       q.Prompt,
				TimeLimitSec: timeLimitPerQuestion,
				LeftItems:    leftItems,
				RightItems:   rightItems,
				SolutionMap:  solMap,
				Hint:         q.Hint,
				Explanation:  q.Explanation,
			})
		}

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
			TimeLimitSec:            timeLimitPerQuestion * len(sessionQuestions),
			TimeLimitPerQuestionSec: timeLimitPerQuestion,
			Boards:                  boardDTOs,
		}, http.StatusCreated, nil
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
	_ = repo.UpdateSession(ctx, session)
	_ = repo.ClearClientActiveSession(ctx, clientID)
	ClearSessionTTL(ctx, sessionID)
	return http.StatusOK, nil
}

// --- Answer Evaluation ---

// EvaluateAnswer grades a player's answer, records it in history, updates session state,
// and returns the result with gamification data.
func EvaluateAnswer(ctx context.Context, clientID int, req *dto.SubmitAnswerRequestDTO) (*dto.AnswerResultDTO, int, error) {
	// 1. Fetch active session from Redis RAM (< 0.2ms)
	session, err := repo.GetSessionByID(ctx, req.Session)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusNotFound, errors.New("session not found")
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to validate session: %w", err)
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

	// 2. Atomic duplicate check via Redis/RAM mutex in < 0.1ms (eliminates synchronous DB query)
	if !cache.AcquireAnswerLock(ctx, req.Session, req.Question) {
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
		GameMode:       session.GameMode,
		QuestionCode:   req.Question,
		SelectedOption: req.Option,
		IsCorrect:      isCorrect,
		TimeTakenMs:    req.TimeTakenMs,
		PointsAwarded:  pointsEarned,
		CoinsAwarded:   coinsEarned,
	}

	// 7. Persist updated session state in Redis RAM (< 0.2ms)
	if err := repo.UpdateSession(ctx, session); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to update session state: %w", err)
	}

	// 8. Decouple PostgreSQL history insert to detached background worker
	// Eliminates 15ms-30ms disk WAL latency from client hot path
	go func(rec *models.UserQuestionHistory) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.RecordAnswer(bgCtx, rec); err != nil {
			log.Printf("[rest] async persist answer failed for %s: %v", rec.QuestionCode, err)
		}
	}(historyRecord)

	go RefreshSessionTTL(context.Background(), session.ID)

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

// CompleteSortItOutSession processes batch answer submissions for a Sort It Out swipe session,
// applies authoritative Speed + Combo scoring, awards economy rewards, logs history, and finalizes the session.
func CompleteSortItOutSession(ctx context.Context, clientID int, req *dto.CompleteSortItOutRequestDTO) (*dto.CompleteSortItOutResponseDTO, int, error) {
	if req == nil {
		return nil, http.StatusBadRequest, errors.New("request body is required")
	}
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

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
	if session.GameMode != models.GameModeSortItOut {
		return nil, http.StatusBadRequest, errors.New("session is not a sort_it_out game mode")
	}
	if session.Status != models.SessionStatusInProgress {
		return nil, http.StatusConflict, errors.New("session is already completed or abandoned")
	}
	if CheckAndAbandonIfExpired(ctx, session) {
		return nil, http.StatusConflict, errors.New("quiz session expired due to 5 minutes of inactivity and has been abandoned")
	}

	sessionQuestions, err := session.GetQuestions()
	if err != nil || len(sessionQuestions) == 0 {
		return nil, http.StatusInternalServerError, errors.New("failed to load session questions")
	}

	sqMap := make(map[string]models.SessionQuestion, len(sessionQuestions))
	for _, sq := range sessionQuestions {
		sqMap[sq.QuestionCode] = sq
	}

	comboStreak := 0
	bestStreak := 0
	correctCount := 0
	timeoutCount := 0
	totalBasePts := 0
	totalSpeedBonus := 0
	totalComboBonus := 0
	cardsReview := make([]dto.SortItOutCardReviewDTO, 0, len(req.Answers))
	historyRecords := make([]*models.UserQuestionHistory, 0, len(req.Answers))
	seenQuestions := make(map[string]bool, len(req.Answers))

	for _, ans := range req.Answers {
		sq, ok := sqMap[ans.Question]
		if !ok {
			return nil, http.StatusBadRequest, fmt.Errorf("question %s does not belong to session", ans.Question)
		}
		if seenQuestions[ans.Question] {
			return nil, http.StatusConflict, fmt.Errorf("duplicate answer for question %s", ans.Question)
		}
		seenQuestions[ans.Question] = true

		isTimeout := ans.IsTimeout || strings.EqualFold(ans.Option, "timeout") || strings.EqualFold(ans.Option, "skip")
		selectedOption := strings.ToLower(strings.TrimSpace(ans.Option))
		correctOption := strings.ToLower(strings.TrimSpace(sq.CorrectOption))
		isCorrect := false

		if !isTimeout {
			if selectedOption == correctOption {
				isCorrect = true
			} else if sq.CorrectText != "" && strings.EqualFold(selectedOption, strings.TrimSpace(sq.CorrectText)) {
				isCorrect = true
			}
		} else {
			timeoutCount++
		}

		if isCorrect {
			comboStreak++
			if comboStreak > bestStreak {
				bestStreak = comboStreak
			}
			correctCount++
		} else {
			comboStreak = 0
		}

		basePts, speedBonus, comboBonus, ptsEarned := CalculateSortItOutPoints(isCorrect, ans.TimeTakenMs, comboStreak)
		totalBasePts += basePts
		totalSpeedBonus += speedBonus
		totalComboBonus += comboBonus

		cardsReview = append(cardsReview, dto.SortItOutCardReviewDTO{
			Question:       sq.QuestionCode,
			Prompt:         sq.Prompt,
			SelectedOption: selectedOption,
			CorrectOption:  correctOption,
			IsCorrect:      isCorrect,
			IsTimeout:      isTimeout,
			TimeTakenMs:    ans.TimeTakenMs,
			BasePoints:     basePts,
			SpeedBonus:     speedBonus,
			ComboBonus:     comboBonus,
			PointsEarned:   ptsEarned,
			Explanation:    sq.Explanation,
		})

		historyRecords = append(historyRecords, &models.UserQuestionHistory{
			ClientID:       clientID,
			SessionID:      session.ID,
			GameMode:       models.GameModeSortItOut,
			QuestionCode:   sq.QuestionCode,
			SelectedOption: selectedOption,
			IsCorrect:      isCorrect,
			TimeTakenMs:    ans.TimeTakenMs,
			PointsAwarded:  ptsEarned,
			CoinsAwarded:   0,
		})
	}

	finalScore := totalBasePts + totalSpeedBonus + totalComboBonus
	session.Score = finalScore
	session.CorrectCount = correctCount
	session.ComboStreak = comboStreak
	session.BestStreak = bestStreak
	session.CurrentIdx = len(req.Answers)
	session.CompletedSuccessfully = len(req.Answers) >= session.TotalQuestions
	session.EndReason = "completed"

	reward := CalculateSortItOutRewards(correctCount, session.TotalQuestions, finalScore)

	var profile *models.Profile
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return repo.RecordBatchAnswers(gCtx, historyRecords)
	})

	g.Go(func() error {
		var pErr error
		profile, pErr = GetOrCreateProfile(gCtx, clientID)
		if pErr != nil {
			log.Printf("[SortItOut] Notice fetching profile for client %d: %v\n", clientID, pErr)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to record answer history: %w", err)
	}
	oldLevel := 1
	oldExperience := 0
	currentStreak := 0
	if profile != nil {
		oldLevel = profile.Level
		oldExperience = profile.Experience
		currentStreak = profile.CurrentStreak
	}

	newExperience := oldExperience + reward.XP
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

	if didLevelUp && profile != nil {
		profile.Level = newLevel
		_ = updateProfileLevel(ctx, clientID, newLevel)
	}

	accuracy := 0.0
	if session.TotalQuestions > 0 {
		accuracy = math.Round(float64(correctCount)/float64(session.TotalQuestions)*10000) / 100
	}
	nextLevelExp := CalculateExperienceForLevel(newLevel)

	return &dto.CompleteSortItOutResponseDTO{
		Session:            session.ID,
		PairType:           session.PairType,
		TotalQuestions:     session.TotalQuestions,
		CorrectCount:       correctCount,
		TimeoutCount:       timeoutCount,
		BestStreak:         bestStreak,
		AccuracyPercentage: accuracy,
		FinalScore:         finalScore,
		CoinsAwarded:       reward.Coins,
		XPAwarded:          reward.XP,
		GemsAwarded:        reward.Gems,
		ScoreBreakdown: map[string]int{
			"base_points": totalBasePts,
			"speed_bonus": totalSpeedBonus,
			"combo_bonus": totalComboBonus,
			"final_score": finalScore,
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
			CurrentStreak:  currentStreak,
			TodayCompleted: true,
		},
		CardsReview: cardsReview,
	}, http.StatusOK, nil
}

// CompleteMatchTheFollowingSession processes batch board submissions for a Match the Following session,
// applies authoritative pair verification and scoring, awards economy rewards, logs history, and finalizes the session.
func CompleteMatchTheFollowingSession(ctx context.Context, clientID int, req *dto.CompleteMatchTheFollowingRequestDTO) (*dto.CompleteMatchTheFollowingResponseDTO, int, error) {
	if req == nil {
		return nil, http.StatusBadRequest, errors.New("request body is required")
	}
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

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
	if session.GameMode != models.GameModeMatchTheFollowing {
		return nil, http.StatusBadRequest, errors.New("session is not a match_the_following game mode")
	}
	if session.Status != models.SessionStatusInProgress {
		return nil, http.StatusConflict, errors.New("session is already completed or abandoned")
	}
	if CheckAndAbandonIfExpired(ctx, session) {
		return nil, http.StatusConflict, errors.New("quiz session expired due to 5 minutes of inactivity and has been abandoned")
	}

	sessionQuestions, err := session.GetQuestions()
	if err != nil || len(sessionQuestions) == 0 {
		return nil, http.StatusInternalServerError, errors.New("failed to load session questions")
	}

	sqMap := make(map[string]models.SessionQuestion, len(sessionQuestions))
	for _, sq := range sessionQuestions {
		sqMap[sq.QuestionCode] = sq
	}

	clearedBoards := 0
	totalPairsMatched := 0
	totalPairsCount := 0
	totalMistakes := 0
	totalBasePts := 0
	totalFlawlessBonus := 0
	totalSpeedBonus := 0
	totalMistakePenalty := 0
	finalScore := 0

	boardsReview := make([]dto.MatchBoardReviewDTO, 0, len(req.Boards))
	historyRecords := make([]*models.UserQuestionHistory, 0, len(req.Boards))
	seenBoards := make(map[string]bool, len(req.Boards))

	timeLimitSec := 25

	for _, boardSub := range req.Boards {
		sq, ok := sqMap[boardSub.QuestionCode]
		if !ok {
			return nil, http.StatusBadRequest, fmt.Errorf("question %s does not belong to session", boardSub.QuestionCode)
		}
		if seenBoards[boardSub.QuestionCode] {
			return nil, http.StatusConflict, fmt.Errorf("duplicate submission for board %s", boardSub.QuestionCode)
		}
		seenBoards[boardSub.QuestionCode] = true

		solMap := make(map[string]string)
		for _, p := range sq.MatchPairs {
			leftID := fmt.Sprintf("%s_l", p.ID)
			rightID := fmt.Sprintf("%s_r", p.ID)
			solMap[leftID] = rightID
			solMap[p.ID] = p.ID
		}

		totalPairs := len(sq.MatchPairs)
		matchedCount := 0
		usedRightIDs := make(map[string]bool)

		for leftID, rightID := range boardSub.Matches {
			if usedRightIDs[rightID] {
				continue
			}
			expectedRight, ok := solMap[leftID]
			if !ok {
				expectedRight, ok = solMap[strings.TrimSuffix(leftID, "_l")]
			}
			if ok && (expectedRight == rightID || strings.TrimSuffix(expectedRight, "_r") == strings.TrimSuffix(rightID, "_r")) {
				usedRightIDs[rightID] = true
				matchedCount++
			}
		}

		isCleared := totalPairs > 0 && matchedCount == totalPairs
		if isCleared {
			clearedBoards++
		}
		isFlawless := isCleared && boardSub.Mistakes == 0
		isTimeout := boardSub.IsTimeout

		basePts, flawlessBonus, speedBonus, mistakePenalty, totalBoardScore := CalculateMatchBoardPoints(
			matchedCount,
			totalPairs,
			boardSub.Mistakes,
			timeLimitSec,
			boardSub.TimeTakenMs,
		)

		totalBasePts += basePts
		totalFlawlessBonus += flawlessBonus
		totalSpeedBonus += speedBonus
		totalMistakePenalty += mistakePenalty
		finalScore += totalBoardScore

		totalPairsMatched += matchedCount
		totalPairsCount += totalPairs
		totalMistakes += boardSub.Mistakes

		boardsReview = append(boardsReview, dto.MatchBoardReviewDTO{
			QuestionCode:    sq.QuestionCode,
			Prompt:          sq.Prompt,
			PairsMatched:    matchedCount,
			TotalPairs:      totalPairs,
			Mistakes:        boardSub.Mistakes,
			TimeTakenMs:     boardSub.TimeTakenMs,
			IsFlawless:      isFlawless,
			IsTimeout:       isTimeout,
			BasePoints:      basePts,
			FlawlessBonus:   flawlessBonus,
			SpeedBonus:      speedBonus,
			MistakePenalty:  mistakePenalty,
			TotalBoardScore: totalBoardScore,
			Explanation:     sq.Explanation,
		})

		historyRecords = append(historyRecords, &models.UserQuestionHistory{
			ClientID:       clientID,
			SessionID:      session.ID,
			GameMode:       models.GameModeMatchTheFollowing,
			QuestionCode:   sq.QuestionCode,
			SelectedOption: fmt.Sprintf("%d/%d", matchedCount, totalPairs),
			IsCorrect:      isCleared,
			TimeTakenMs:    boardSub.TimeTakenMs,
			PointsAwarded:  totalBoardScore,
			CoinsAwarded:   0,
		})
	}

	session.Score = finalScore
	session.CorrectCount = clearedBoards
	session.CurrentIdx = len(req.Boards)
	session.CompletedSuccessfully = len(req.Boards) >= session.TotalQuestions
	session.EndReason = "completed"

	reward := CalculateMatchTheFollowingRewards(totalPairsMatched, totalPairsCount, clearedBoards, session.TotalQuestions, finalScore)

	var profile *models.Profile
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return repo.RecordBatchAnswers(gCtx, historyRecords)
	})

	g.Go(func() error {
		var pErr error
		profile, pErr = GetOrCreateProfile(gCtx, clientID)
		if pErr != nil {
			log.Printf("[MatchTheFollowing] Notice fetching profile for client %d: %v\n", clientID, pErr)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to record answer history: %w", err)
	}
	oldLevel := 1
	oldExperience := 0
	currentStreak := 0
	if profile != nil {
		oldLevel = profile.Level
		oldExperience = profile.Experience
		currentStreak = profile.CurrentStreak
	}

	newExperience := oldExperience + reward.XP
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

	if didLevelUp && profile != nil {
		profile.Level = newLevel
		_ = updateProfileLevel(ctx, clientID, newLevel)
	}

	accuracy := 0.0
	if totalPairsCount > 0 {
		accuracy = math.Round(float64(totalPairsMatched)/float64(totalPairsCount)*10000) / 100
	}
	nextLevelExp := CalculateExperienceForLevel(newLevel)

	return &dto.CompleteMatchTheFollowingResponseDTO{
		Session:            session.ID,
		TotalBoards:        session.TotalQuestions,
		ClearedBoards:      clearedBoards,
		TotalPairsMatched:  totalPairsMatched,
		TotalPairsCount:    totalPairsCount,
		TotalMistakes:      totalMistakes,
		AccuracyPercentage: accuracy,
		FinalScore:         finalScore,
		CoinsAwarded:       reward.Coins,
		XPAwarded:          reward.XP,
		GemsAwarded:        reward.Gems,
		ScoreBreakdown: map[string]int{
			"base_points":     totalBasePts,
			"flawless_bonus":  totalFlawlessBonus,
			"speed_bonus":     totalSpeedBonus,
			"mistake_penalty": totalMistakePenalty,
			"final_score":     finalScore,
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
			CurrentStreak:  currentStreak,
			TodayCompleted: true,
		},
		BoardsReview: boardsReview,
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

// CalculateSortItOutPoints computes authoritative Speed + Combo points for a Sort It Out swipe.
// Base Points: 10 pts per correct answer
// Speed Bonus: +5 pts if < 2.0s (2000ms), +2 pts if < 4.0s (4000ms)
// Combo Streak Bonus: +2 pts per consecutive streak step (capped at +10 pts)
func CalculateSortItOutPoints(isCorrect bool, timeTakenMs int, comboStreak int) (basePts, speedBonus, comboBonus, totalPts int) {
	if !isCorrect {
		return 0, 0, 0, 0
	}
	basePts = 10
	if timeTakenMs > 0 && timeTakenMs < 2000 {
		speedBonus = 5
	} else if timeTakenMs >= 2000 && timeTakenMs < 4000 {
		speedBonus = 2
	}
	if comboStreak > 0 {
		comboBonus = comboStreak * 2
		if comboBonus > 10 {
			comboBonus = 10
		}
	}
	totalPts = basePts + speedBonus + comboBonus
	return basePts, speedBonus, comboBonus, totalPts
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

// CalculateSortItOutRewards computes economy rewards for completed Sort It Out swipe rounds.
func CalculateSortItOutRewards(correctCount, totalQuestions, _ int) GameRewardSummary {
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
	for _, v := range xpBreakdown {
		xp += v
	}
	coins := 0
	for _, v := range coinBreakdown {
		coins += v
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

// CalculateMatchBoardPoints computes authoritative score components for a Match the Following board.
// Base Points: 10 pts per correctly matched pair (40 pts max base per board with 4 pairs).
// Flawless Bonus: +10 pts if cleared with 0 mistakes.
// Speed Bonus: +1 pt per second remaining on the board timer upon board clearance.
// Mistake Penalty: -2 pts per wrong attempt.
// Clamped so board score cannot drop below 0.
func CalculateMatchBoardPoints(matchedCount int, totalPairs int, mistakes int, timeLimitSec int, timeTakenMs int) (basePts, flawlessBonus, speedBonus, mistakePenalty, totalPts int) {
	if matchedCount < 0 {
		matchedCount = 0
	}
	basePts = matchedCount * 10

	isCleared := totalPairs > 0 && matchedCount == totalPairs
	if isCleared && mistakes == 0 {
		flawlessBonus = 10
	}

	if isCleared && timeLimitSec > 0 {
		remSec := timeLimitSec - (timeTakenMs / 1000)
		if remSec > 0 {
			speedBonus = remSec
		}
	}

	if mistakes > 0 {
		mistakePenalty = mistakes * 2
	}

	rawScore := basePts + flawlessBonus + speedBonus - mistakePenalty
	if rawScore < 0 {
		totalPts = 0
	} else {
		totalPts = rawScore
	}
	return basePts, flawlessBonus, speedBonus, mistakePenalty, totalPts
}

// CalculateMatchTheFollowingRewards computes economy rewards for completed Match the Following puzzle sessions.
func CalculateMatchTheFollowingRewards(pairsMatched, totalPairs, clearedBoards, totalBoards, finalScore int) GameRewardSummary {
	xpBreakdown := map[string]int{
		"completion_xp":     MCQCompletionXP,
		"correct_answer_xp": pairsMatched * MCQCorrectAnswerXP,
		"perfect_bonus_xp":  0,
	}
	coinBreakdown := map[string]int{
		"completion_coins":     MCQCompletionCoins,
		"correct_answer_coins": pairsMatched * MCQCorrectAnswerCoins,
		"perfect_bonus_coins":  0,
	}

	if totalPairs > 0 && pairsMatched == totalPairs {
		xpBreakdown["perfect_bonus_xp"] = MCQPerfectBonusXP
		coinBreakdown["perfect_bonus_coins"] = MCQPerfectBonusCoins
	}

	xp := 0
	for _, v := range xpBreakdown {
		xp += v
	}
	coins := 0
	for _, v := range coinBreakdown {
		coins += v
	}

	gems := 0
	if totalPairs > 0 {
		accuracy := float64(pairsMatched) / float64(totalPairs)
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
// streak milestones, Sort It Out speed/combo potential, and Match the Following bonuses.
func CalculateMaxScoreForMode(gameMode string, totalQuestions int) int {
	maxScore := CalculateMaxScore(totalQuestions)
	if gameMode == models.GameModeSuddenDeath && totalQuestions > 0 {
		maxScore += (totalQuestions / SuddenDeathStreakSize) * SuddenDeathStreakPoints
	} else if gameMode == models.GameModeSortItOut && totalQuestions > 0 {
		comboTotal := 0
		for i := 1; i <= totalQuestions; i++ {
			cb := i * 2
			if cb > 10 {
				cb = 10
			}
			comboTotal += cb
		}
		maxScore = (totalQuestions * 10) + (totalQuestions * 5) + comboTotal
	} else if gameMode == models.GameModeMatchTheFollowing && totalQuestions > 0 {
		// 40 base + 10 flawless + 25 speed bonus (at 25s limit) = 75 per board
		maxScore = totalQuestions * 75
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
		PairType:     sq.PairType,
	}
	if includeCorrect {
		d.CorrectOption = strings.ToLower(sq.CorrectOption)
		d.Explanation = sq.Explanation
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
