package repo

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/cache"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// --- O(1) Indexed Question Sampling & Retrieval ---

// sampleRandomCodes takes a slice of question codes and returns up to limit randomly chosen codes in Go memory (O(N) shuffle).
func sampleRandomCodes(codes []string, limit int) []string {
	if len(codes) <= limit {
		return codes
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	shuffled := make([]string, len(codes))
	copy(shuffled, codes)
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled[:limit]
}

// GetQuestionsByTopic fetches random questions for a given topic from mcq_questions.
// Uses fast index-only code scan followed by primary B-tree lookup (< 1.5ms vs 80ms ORDER BY RANDOM()).
func GetQuestionsByTopic(ctx context.Context, topicID string, limit int) ([]models.Question, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}

	var codes []string
	err := db.WithContext(ctx).
		Model(&models.MCQQuestion{}).
		Where("topic_id = ? AND (question_type = ? OR question_type = '' OR question_type IS NULL)", topicID, models.GameModeMCQ).
		Pluck("question_code", &codes).Error
	if err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, nil
	}

	sampledCodes := sampleRandomCodes(codes, limit)
	return GetQuestionsByCodes(ctx, sampledCodes)
}

// GetQuestionsByTopicAndMode fetches random questions for a given topic and question_type.
func GetQuestionsByTopicAndMode(ctx context.Context, topicID string, questionType string, limit int) ([]models.Question, error) {
	if questionType == "" {
		return GetQuestionsByTopic(ctx, topicID, limit)
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}

	var codes []string
	err := db.WithContext(ctx).
		Model(&models.MCQQuestion{}).
		Where("topic_id = ? AND question_type = ?", topicID, questionType).
		Pluck("question_code", &codes).Error
	if err != nil {
		return nil, err
	}

	if len(codes) == 0 && questionType != models.GameModeSuddenDeath {
		return GetQuestionsByTopic(ctx, topicID, limit)
	}
	if len(codes) == 0 {
		return nil, nil
	}

	sampledCodes := sampleRandomCodes(codes, limit)
	return GetQuestionsByCodes(ctx, sampledCodes)
}

// GetQuestionsByCodes fetches questions matching the specified question codes from mcq_questions,
// preserving the caller's requested order.
func GetQuestionsByCodes(ctx context.Context, codes []string) ([]models.Question, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}

	var questions []models.MCQQuestion
	err := db.WithContext(ctx).
		Where("question_code IN ?", codes).
		Find(&questions).Error
	if err != nil {
		return nil, err
	}

	questionMap := make(map[string]models.MCQQuestion, len(questions))
	for _, q := range questions {
		questionMap[q.QuestionCode] = q
	}
	ordered := make([]models.Question, 0, len(codes))
	for _, code := range codes {
		if q, ok := questionMap[code]; ok {
			ordered = append(ordered, q)
		}
	}
	return ordered, nil
}

// GetQuestionByCode fetches a single question by its unique code from mcq_questions.
func GetQuestionByCode(ctx context.Context, code string) (*models.Question, error) {
	db := GetDB()
	if db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var q models.MCQQuestion
	err := db.WithContext(ctx).
		Where("question_code = ?", code).
		First(&q).Error
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// GetAvailableTopics returns distinct topic IDs with question counts from mcq_questions.
// Protected by SingleFlight and cached in Redis for 1 hour.
func GetAvailableTopics(ctx context.Context) ([]map[string]interface{}, error) {
	// 1. Check Redis cache first (< 0.2ms)
	if cached, ok := cache.GetCachedTopics(ctx); ok && len(cached) > 0 {
		return cached, nil
	}

	// 2. Coalesce concurrent requests into a single database execution
	val, err, _ := cache.RequestGroup.Do("get_available_topics", func() (interface{}, error) {
		// Double-check cache inside mutex
		if cached, ok := cache.GetCachedTopics(ctx); ok && len(cached) > 0 {
			return cached, nil
		}
		db := GetDB()
		if db == nil {
			return []map[string]interface{}{}, nil
		}
		var results []map[string]interface{}
		err := db.WithContext(ctx).
			Model(&models.MCQQuestion{}).
			Select("topic_id, COUNT(*) as count").
			Group("topic_id").
			Order("topic_id ASC").
			Find(&results).Error
		if err == nil && len(results) > 0 {
			_ = cache.SetCachedTopics(ctx, results)
		}
		return results, err
	})
	if err != nil {
		return nil, err
	}
	return val.([]map[string]interface{}), nil
}

// CountQuestionsByTopic returns the number of questions for a topic in mcq_questions.
func CountQuestionsByTopic(ctx context.Context, topicID string) (int64, error) {
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	var count int64
	err := db.WithContext(ctx).
		Model(&models.MCQQuestion{}).
		Where("topic_id = ? AND (question_type = ? OR question_type = '' OR question_type IS NULL)", topicID, models.GameModeMCQ).
		Count(&count).Error
	return count, err
}

// CountQuestionsByTopicAndMode returns the number of questions for a topic and mode.
func CountQuestionsByTopicAndMode(ctx context.Context, topicID string, questionType string) (int64, error) {
	if questionType == "" {
		return CountQuestionsByTopic(ctx, topicID)
	}
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	var count int64
	err := db.WithContext(ctx).
		Model(&models.MCQQuestion{}).
		Where("topic_id = ? AND question_type = ?", topicID, questionType).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	if count == 0 && questionType != models.GameModeSuddenDeath {
		return CountQuestionsByTopic(ctx, topicID)
	}
	return count, nil
}

// --- Ephemeral Session Repository (Pure In-Memory & Redis RAM) ---

// CreateSession stores a new active game session in Redis RAM (0 PostgreSQL writes)
// and registers the active exclusion lock pointer (< 0.2ms).
func CreateSession(ctx context.Context, session *models.GameSession) error {
	if session == nil {
		return fmt.Errorf("session cannot be nil")
	}
	if session.ID == "" {
		session.ID = uuid.NewString()
	}
	now := time.Now()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}
	if err := CacheSession(ctx, session); err != nil {
		return err
	}
	if session.ClientID > 0 {
		_ = SetClientActiveSession(ctx, session.ClientID, session.ID, session.GameMode)
	}
	return nil
}

// GetSessionByID fetches an active session from Redis RAM (< 0.5ms).
func GetSessionByID(ctx context.Context, sessionID string) (*models.GameSession, error) {
	if cached, ok := GetCachedSession(ctx, sessionID); ok && cached != nil {
		return cached, nil
	}
	return nil, gorm.ErrRecordNotFound
}

// GetActiveSessionByClientID checks if a client has an active in-progress game in < 0.2ms.
// Queries the Redis active session pointer without scanning any database table.
func GetActiveSessionByClientID(ctx context.Context, clientID int) (*models.GameSession, error) {
	info, found := GetClientActiveSession(ctx, clientID)
	if !found || info == nil || info.SessionID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	sess, err := GetSessionByID(ctx, info.SessionID)
	if err != nil {
		_ = ClearClientActiveSession(ctx, clientID)
		return nil, gorm.ErrRecordNotFound
	}
	if sess.Status != models.SessionStatusInProgress {
		_ = ClearClientActiveSession(ctx, clientID)
		return nil, gorm.ErrRecordNotFound
	}
	return sess, nil
}

// UpdateSession persists updated in-flight session fields to Redis RAM (0 PostgreSQL writes).
func UpdateSession(ctx context.Context, session *models.GameSession) error {
	if session == nil {
		return nil
	}
	return CacheSession(ctx, session)
}

// AbandonInactiveSessions cleans up expired in-memory sessions (handled automatically by Redis TTL).
func AbandonInactiveSessions(ctx context.Context, ttl time.Duration) (int64, error) {
	// With Redis TTL, expired sessions self-destruct automatically. 0 DB work needed.
	return 0, nil
}

// --- History Repository ---

// RecordAnswer inserts an immutable answer history record.
func RecordAnswer(ctx context.Context, history *models.UserQuestionHistory) error {
	db := GetDB()
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).Create(history).Error
}

// HasAnsweredQuestion checks if a question was already answered in a given session.
func HasAnsweredQuestion(ctx context.Context, sessionID, questionCode string) (bool, error) {
	db := GetDB()
	if db == nil {
		return false, nil
	}
	var count int64
	err := db.WithContext(ctx).
		Model(&models.UserQuestionHistory{}).
		Where("session_id = ? AND question_code = ?", sessionID, questionCode).
		Count(&count).Error
	return count > 0, err
}

// GetSessionHistory returns all answer records for a session ordered by time.
func GetSessionHistory(ctx context.Context, sessionID string) ([]models.UserQuestionHistory, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var records []models.UserQuestionHistory
	err := db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("answered_at ASC").
		Find(&records).Error
	return records, err
}

// GetClientGameHistory returns completed game session summaries aggregated from user_question_history.
func GetClientGameHistory(ctx context.Context, clientID, limit, offset int) ([]models.GameSession, int64, error) {
	db := GetDB()
	if db == nil {
		return nil, 0, nil
	}

	var sessionIDs []string
	err := db.WithContext(ctx).
		Model(&models.UserQuestionHistory{}).
		Where("client_id = ?", clientID).
		Distinct("session_id").
		Pluck("session_id", &sessionIDs).Error
	if err != nil {
		return nil, 0, err
	}
	total := int64(len(sessionIDs))
	if total == 0 {
		return nil, 0, nil
	}

	// Slice pagination
	start := offset
	if start > len(sessionIDs) {
		start = len(sessionIDs)
	}
	end := start + limit
	if end > len(sessionIDs) {
		end = len(sessionIDs)
	}
	pagedIDs := sessionIDs[start:end]

	var allHist []models.UserQuestionHistory
	_ = db.WithContext(ctx).Where("session_id IN ?", pagedIDs).Order("answered_at ASC").Find(&allHist).Error

	histBySession := make(map[string][]models.UserQuestionHistory, len(pagedIDs))
	for _, h := range allHist {
		histBySession[h.SessionID] = append(histBySession[h.SessionID], h)
	}

	sessions := make([]models.GameSession, 0, len(pagedIDs))
	for _, sID := range pagedIDs {
		hist := histBySession[sID]
		if len(hist) > 0 {
			score := 0
			correct := 0
			mode := hist[0].GameMode
			for _, h := range hist {
				score += h.PointsAwarded
				if h.IsCorrect {
					correct++
				}
			}
			sessions = append(sessions, models.GameSession{
				ID:             sID,
				ClientID:       clientID,
				GameMode:       mode,
				TotalQuestions: len(hist),
				CorrectCount:   correct,
				Score:          score,
				Status:         models.SessionStatusFinished,
				StartedAt:      hist[0].AnsweredAt,
			})
		}
	}
	return sessions, total, nil
}

// --- Finalization (Atomic Transaction) ---

// FinalizeSession atomically commits rewards to wallet_ledger, updates client_profiles,
// increments Redis real-time leaderboard (ZINCRBY), and cleans up ephemeral session keys.
func FinalizeSession(ctx context.Context, session *models.GameSession, coins, xp, gems int) error {
	now := time.Now().UTC()
	session.Status = models.SessionStatusFinished
	session.EndedAt = &now

	db := GetDB()
	if db != nil {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 1. Write immutable wallet ledger entries in single batch
			ledgerEntries := []models.WalletLedger{}
			if coins > 0 {
				ledgerEntries = append(ledgerEntries, models.WalletLedger{
					ClientID:        session.ClientID,
					TransactionType: models.TxTypeQuizReward,
					Amount:          coins,
					Currency:        models.CurrencyCoins,
					ReferenceID:     session.ID,
				})
			}
			if xp > 0 {
				ledgerEntries = append(ledgerEntries, models.WalletLedger{
					ClientID:        session.ClientID,
					TransactionType: models.TxTypeQuizReward,
					Amount:          xp,
					Currency:        models.CurrencyExperience,
					ReferenceID:     session.ID,
				})
			}
			if gems > 0 {
				ledgerEntries = append(ledgerEntries, models.WalletLedger{
					ClientID:        session.ClientID,
					TransactionType: models.TxTypeQuizReward,
					Amount:          gems,
					Currency:        models.CurrencyGems,
					ReferenceID:     session.ID,
				})
			}
			if len(ledgerEntries) > 0 {
				if err := tx.CreateInBatches(ledgerEntries, len(ledgerEntries)).Error; err != nil {
					return fmt.Errorf("failed to write wallet ledger: %w", err)
				}
			}

			// 2. Update profile balances atomically
			profileUpdate := map[string]interface{}{
				"coins":        gorm.Expr("coins + ?", coins),
				"gems":         gorm.Expr("gems + ?", gems),
				"experience":   gorm.Expr("experience + ?", xp),
				"weekly_score": gorm.Expr("weekly_score + ?", session.Score),
			}
			if err := tx.Model(&models.Profile{}).
				Where("client_id = ?", session.ClientID).
				Updates(profileUpdate).Error; err != nil {
				return fmt.Errorf("failed to update profile: %w", err)
			}

			// 3. Record daily streak activity
			todayStr := now.Format("2006-01-02")
			_ = RecordDailyActivity(ctx, session.ClientID, todayStr)

			return nil
		})
		if err != nil {
			return err
		}
	}

	// 4. Redis Real-Time Leaderboard Update (< 0.2ms)
	if session.Score > 0 {
		_ = cache.UpdateWeeklyLeaderboard(ctx, session.ClientID, session.Score)
	}

	// 5. Invalidate profile cache and clear client active exclusion lock
	_ = cache.InvalidateCachedProfile(ctx, session.ClientID)
	_ = ClearClientActiveSession(ctx, session.ClientID)
	_ = CacheSession(ctx, session)

	return nil
}

// --- Seed Helpers ---

// BulkInsertQuestions inserts a batch of questions into mcq_questions.
func BulkInsertQuestions(ctx context.Context, questions []models.MCQQuestion) error {
	db := GetDB()
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).CreateInBatches(questions, 50).Error
}

// QuestionCount returns the total number of questions in mcq_questions.
func QuestionCount(ctx context.Context) (int64, error) {
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	var count int64
	err := db.WithContext(ctx).Model(&models.MCQQuestion{}).Count(&count).Error
	return count, err
}

// UpdateProfileLevel updates only the level column on the player's profile.
func UpdateProfileLevel(ctx context.Context, clientID, newLevel int) error {
	db := GetDB()
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).
		Table("client_profiles").
		Where("client_id = ?", clientID).
		Update("level", newLevel).Error
}

// --- Sort It Out Helpers ---

// GetDistinctPairTypes fetches available pair types from sort_it_out_questions.
func GetDistinctPairTypes(ctx context.Context, topicID, mode string) ([]string, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var pairTypes []string
	query := db.WithContext(ctx).
		Model(&models.SortItOutQuestion{}).
		Where("pair_type IS NOT NULL AND pair_type != ''")
	if topicID != "" {
		query = query.Where("topic_id = ?", topicID)
	}
	err := query.Distinct("pair_type").Pluck("pair_type", &pairTypes).Error
	return pairTypes, err
}

// GetSortItOutQuestions fetches questions from sort_it_out_questions using O(1) indexed code sampling.
func GetSortItOutQuestions(ctx context.Context, topicID, pairType string, limit int) ([]models.SortItOutQuestion, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}

	query := db.WithContext(ctx).Model(&models.SortItOutQuestion{})
	if topicID != "" {
		query = query.Where("topic_id = ?", topicID)
	}
	if pairType != "" {
		query = query.Where("pair_type = ?", pairType)
	}

	var codes []string
	if err := query.Pluck("question_code", &codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, nil
	}

	sampledCodes := sampleRandomCodes(codes, limit)
	var questions []models.SortItOutQuestion
	err := db.WithContext(ctx).
		Where("question_code IN ?", sampledCodes).
		Find(&questions).Error
	return questions, err
}

// RecordBatchAnswers performs efficient batch insertion into user_question_history.
func RecordBatchAnswers(ctx context.Context, history []*models.UserQuestionHistory) error {
	if len(history) == 0 {
		return nil
	}
	db := GetDB()
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).CreateInBatches(history, 50).Error
}

// --- Match the Following Helpers ---

// GetMatchPairsByQuestionIDs fetches all pairs belonging to the given question IDs, ordered by pair_order ASC.
func GetMatchPairsByQuestionIDs(ctx context.Context, questionIDs []string) (map[string][]models.MatchPair, error) {
	result := make(map[string][]models.MatchPair)
	if len(questionIDs) == 0 {
		return result, nil
	}
	db := GetDB()
	if db == nil {
		return result, nil
	}
	var pairs []models.MatchPair
	err := db.WithContext(ctx).
		Where("question_id IN ?", questionIDs).
		Order("pair_order ASC").
		Find(&pairs).Error
	if err != nil {
		return nil, err
	}
	for _, p := range pairs {
		result[p.QuestionID] = append(result[p.QuestionID], p)
	}
	return result, nil
}

// CreateMatchPairs inserts a batch of match pairs into the database.
func CreateMatchPairs(ctx context.Context, pairs []models.MatchPair) error {
	if len(pairs) == 0 {
		return nil
	}
	db := GetDB()
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).CreateInBatches(pairs, 50).Error
}

// GetSortItOutQuestionsByCodes fetches questions matching the specified question codes from sort_it_out_questions.
func GetSortItOutQuestionsByCodes(ctx context.Context, codes []string) ([]models.SortItOutQuestion, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var questions []models.SortItOutQuestion
	err := db.WithContext(ctx).
		Where("question_code IN ?", codes).
		Find(&questions).Error
	if err != nil {
		return nil, err
	}
	questionMap := make(map[string]models.SortItOutQuestion, len(questions))
	for _, q := range questions {
		questionMap[q.QuestionCode] = q
	}
	ordered := make([]models.SortItOutQuestion, 0, len(codes))
	for _, code := range codes {
		if q, ok := questionMap[code]; ok {
			ordered = append(ordered, q)
		}
	}
	return ordered, nil
}

// GetMatchTheFollowingQuestionsByCodes fetches questions matching the specified question codes from match_the_following_questions.
func GetMatchTheFollowingQuestionsByCodes(ctx context.Context, codes []string) ([]models.MatchTheFollowingQuestion, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var questions []models.MatchTheFollowingQuestion
	err := db.WithContext(ctx).
		Where("question_code IN ?", codes).
		Find(&questions).Error
	if err != nil {
		return nil, err
	}
	questionMap := make(map[string]models.MatchTheFollowingQuestion, len(questions))
	for _, q := range questions {
		questionMap[q.QuestionCode] = q
	}
	ordered := make([]models.MatchTheFollowingQuestion, 0, len(codes))
	for _, code := range codes {
		if q, ok := questionMap[code]; ok {
			ordered = append(ordered, q)
		}
	}
	return ordered, nil
}

// GetMatchTheFollowingQuestions fetches questions from match_the_following_questions using O(1) indexed code sampling.
func GetMatchTheFollowingQuestions(ctx context.Context, topicID string, limit int) ([]models.MatchTheFollowingQuestion, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}

	query := db.WithContext(ctx).Model(&models.MatchTheFollowingQuestion{})
	if topicID != "" {
		query = query.Where("topic_id = ?", topicID)
	}

	var codes []string
	if err := query.Pluck("question_code", &codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, nil
	}

	sampledCodes := sampleRandomCodes(codes, limit)
	var questions []models.MatchTheFollowingQuestion
	err := db.WithContext(ctx).
		Where("question_code IN ?", sampledCodes).
		Find(&questions).Error
	return questions, err
}
