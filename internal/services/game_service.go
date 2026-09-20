package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"gorm.io/gorm"
)

// Default time limit per question in seconds.
const DefaultTimeLimitSec = 15

// --- Session Management ---

// CreateSession initializes a new quiz session, picks random questions from the DB,
// and returns the session ID + sanitized questions (no correct answers exposed).
func CreateSession(ctx context.Context, clientID int, req *dto.CreateSessionRequestDTO) (*dto.SessionCreatedResponseDTO, int, error) {
	// Check for existing active session
	existing, err := repo.GetActiveSessionByClientID(ctx, clientID)
	if err == nil && existing != nil {
		if req.AbandonStale {
			existing.Status = models.SessionStatusAbandoned
			_ = repo.UpdateSession(ctx, existing)
		} else {
			return nil, http.StatusConflict, errors.New("an active session already exists; complete or abandon it first")
		}
	}

	// Validate topic has enough questions
	count, err := repo.CountQuestionsByTopic(ctx, req.TopicID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to check question count: %w", err)
	}
	if count == 0 {
		return nil, http.StatusNotFound, errors.New("no questions available for the selected topic")
	}
	if int(count) < req.QuestionCount {
		req.QuestionCount = int(count)
	}

	// Fetch random questions from DB
	questions, err := repo.GetQuestionsByTopic(ctx, req.TopicID, req.QuestionCount)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch questions: %w", err)
	}

	// Shuffle the order of questions
	rand.Shuffle(len(questions), func(i, j int) {
		questions[i], questions[j] = questions[j], questions[i]
	})

	// Shuffle options for each question and create SessionQuestion objects
	sessionQuestions := make([]models.SessionQuestion, 0, len(questions))
	questionDTOs := make([]dto.QuestionDTO, 0, len(questions))
	for _, q := range questions {
		sq := ShuffleQuestion(q)
		sessionQuestions = append(sessionQuestions, sq)
		questionDTOs = append(questionDTOs, SessionQuestionToDTO(sq, true))
	}

	// Create session record with serialized QuestionsState
	session := &models.GameSession{
		ClientID:       clientID,
		TopicID:        req.TopicID,
		TotalQuestions: len(sessionQuestions),
		Status:         models.SessionStatusInProgress,
	}
	if err := session.SetQuestions(sessionQuestions); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to serialize session questions: %w", err)
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to create session: %w", err)
	}

	return &dto.SessionCreatedResponseDTO{
		Session:        session.ID,
		Topic:          session.TopicID,
		TotalQuestions: session.TotalQuestions,
		TimeLimitSec:   DefaultTimeLimitSec,
		Questions:      questionDTOs,
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
	session.Status = models.SessionStatusAbandoned
	if err := repo.UpdateSession(ctx, session); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to abandon session: %w", err)
	}
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

	// 2. Prevent duplicate answers for the same question in this session
	already, err := repo.HasAnsweredQuestion(ctx, req.Session, req.Question)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to check answer history: %w", err)
	}
	if already {
		return nil, http.StatusConflict, errors.New("question has already been answered in this session")
	}

	// 3. Find question in session state (fallback to DB question if session state is empty)
	var correctOption string
	var questionDifficulty int
	var questionPoints int
	var questionExplanation *string

	sessionQuestions, _ := session.GetQuestions()
	var matchedSQ *models.SessionQuestion
	for _, sq := range sessionQuestions {
		if sq.QuestionCode == req.Question {
			matchedSQ = &sq
			break
		}
	}

	if matchedSQ != nil {
		correctOption = matchedSQ.CorrectOption
		questionDifficulty = matchedSQ.Difficulty
		questionPoints = matchedSQ.Points
		questionExplanation = matchedSQ.Explanation
	} else {
		// Fallback to database
		question, err := repo.GetQuestionByCode(ctx, req.Question)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, http.StatusNotFound, errors.New("question not found")
			}
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch question: %w", err)
		}
		correctOption = question.CorrectOption
		questionDifficulty = question.Difficulty
		questionPoints = question.Points
		questionExplanation = question.Explanation
	}

	// 4. Grade the answer
	isSkipped := req.Option == "skip"
	isCorrect := false
	if !isSkipped {
		isCorrect = strings.EqualFold(req.Option, correctOption)
	}

	// 5. Calculate scoring
	pointsEarned := 0
	coinsEarned := 0

	if isCorrect {
		session.ComboStreak++
		if session.ComboStreak > session.BestStreak {
			session.BestStreak = session.ComboStreak
		}
		pointsEarned = CalculatePoints(questionPoints, questionDifficulty, req.TimeTakenMs, session.ComboStreak)
		coinsEarned = CalculateCoins(pointsEarned)
		session.CorrectCount++
	} else {
		session.ComboStreak = 0
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

	return &dto.AnswerResultDTO{
		Question:      req.Question,
		Option:        req.Option,
		CorrectOption: strings.ToLower(correctOption),
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
		correct = strings.ToLower(matchedSQ.CorrectOption)
	} else {
		question, err := repo.GetQuestionByCode(ctx, req.Question)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, http.StatusNotFound, errors.New("question not found")
			}
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch question: %w", err)
		}
		correct = strings.ToLower(question.CorrectOption)
	}

	// Pick 2 wrong options to hide
	wrongOptions := make([]string, 0, 3)
	for _, opt := range []string{"a", "b", "c", "d"} {
		if opt != correct {
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

	// Calculate rewards
	coins, xp, gems := CalculateGameRewards(session.Score, session.CorrectCount, session.TotalQuestions)

	// Fetch current profile for level calculation
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch profile: %w", err)
	}
	oldLevel := profile.Level
	newExperience := profile.Experience + xp
	newLevel := CalculateNewLevel(newExperience)
	didLevelUp := newLevel > oldLevel

	// Level-up bonus
	if didLevelUp {
		coins += (newLevel - oldLevel) * 50
		gems += (newLevel - oldLevel)
	}

	// Atomic finalization: session + ledger + profile update
	if err := repo.FinalizeSession(ctx, session, coins, xp, gems); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to finalize session: %w", err)
	}

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
		FinalScore:         session.Score,
		CoinsAwarded:       coins,
		XPAwarded:          xp,
		GemsAwarded:        gems,
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

// CalculatePoints computes points for a correct answer.
// Base points are multiplied by difficulty factor, speed bonus, and combo multiplier.
func CalculatePoints(basePoints, difficulty, timeTakenMs, comboStreak int) int {
	difficultyMultiplier := 1.0
	switch difficulty {
	case 2:
		difficultyMultiplier = 1.5
	case 3:
		difficultyMultiplier = 2.0
	}

	// Speed bonus: answering in under 50% of time limit gets up to 50% bonus
	timeLimitMs := DefaultTimeLimitSec * 1000
	speedFactor := 1.0
	if timeTakenMs > 0 && timeTakenMs < timeLimitMs {
		ratio := float64(timeTakenMs) / float64(timeLimitMs)
		if ratio < 0.5 {
			speedFactor = 1.5
		} else if ratio < 0.75 {
			speedFactor = 1.25
		}
	}

	// Combo streak multiplier
	comboMultiplier := 1.0
	switch {
	case comboStreak >= 7:
		comboMultiplier = 2.0
	case comboStreak >= 5:
		comboMultiplier = 1.5
	case comboStreak >= 3:
		comboMultiplier = 1.2
	}

	raw := float64(basePoints) * difficultyMultiplier * speedFactor * comboMultiplier
	return int(math.Round(raw))
}

// CalculateCoins derives coins from points earned per question.
func CalculateCoins(pointsEarned int) int {
	return int(math.Ceil(float64(pointsEarned) / 5.0))
}

// CalculateGameRewards computes total coins, XP, and gems for a completed session.
func CalculateGameRewards(totalScore, correctCount, totalQuestions int) (coins, xp, gems int) {
	coins = int(math.Ceil(float64(totalScore) / 5.0))
	xp = int(math.Ceil(float64(totalScore) / 2.0))
	gems = 0

	if totalQuestions > 0 {
		accuracy := float64(correctCount) / float64(totalQuestions)
		if accuracy >= 1.0 {
			gems = 3 // Perfect game
		} else if accuracy >= 0.8 {
			gems = 1
		}
	}

	// Completion bonus
	coins += 5
	xp += 10

	return coins, xp, gems
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

// ShuffleQuestion randomly permutes options A, B, C, D and recalculates the correct_option letter.
func ShuffleQuestion(q models.Question) models.SessionQuestion {
	options := []rawOption{
		{origLetter: "a", text: q.OptionA},
		{origLetter: "b", text: q.OptionB},
		{origLetter: "c", text: q.OptionC},
		{origLetter: "d", text: q.OptionD},
	}

	rand.Shuffle(len(options), func(i, j int) {
		options[i], options[j] = options[j], options[i]
	})

	letters := []string{"a", "b", "c", "d"}
	var newCorrect string
	for i, opt := range options {
		if strings.EqualFold(opt.origLetter, q.CorrectOption) {
			newCorrect = letters[i]
			break
		}
	}

	return models.SessionQuestion{
		QuestionCode:  q.QuestionCode,
		Prompt:        q.Prompt,
		OptionA:       options[0].text,
		OptionB:       options[1].text,
		OptionC:       options[2].text,
		OptionD:       options[3].text,
		CorrectOption: newCorrect,
		Difficulty:    q.Difficulty,
		Points:        q.Points,
		Hint:          q.Hint,
		Explanation:   q.Explanation,
	}
}

// SessionQuestionToDTO converts a SessionQuestion into a client-safe DTO.
func SessionQuestionToDTO(sq models.SessionQuestion, includeCorrect bool) dto.QuestionDTO {
	d := dto.QuestionDTO{
		Question: sq.QuestionCode,
		Prompt:   sq.Prompt,
		Points:   sq.Points,
		Hint:     sq.Hint,
		Options: []dto.OptionDTO{
			{Option: "a", Text: sq.OptionA},
			{Option: "b", Text: sq.OptionB},
			{Option: "c", Text: sq.OptionC},
			{Option: "d", Text: sq.OptionD},
		},
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
		Topic:        topicID,
		Total:        len(questionDTOs),
		TimeLimitSec: DefaultTimeLimitSec,
		Questions:    questionDTOs,
	}, http.StatusOK, nil
}
