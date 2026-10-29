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
	"gorm.io/gorm/clause"
)

// --- Question Sampling & Retrieval (content tables are read-only for the backend) ---

// questionTable maps an MCQ-family game mode to its content table.
func questionTable(mode string) string {
	if mode == models.GameModeSuddenDeath {
		return "sudden_death_questions"
	}
	return "mcq_questions"
}

// scope narrows a content query to a subject (API topic_id) and, when > 0, a chapter.
func scope(q *gorm.DB, topicID string, chapterNo int) *gorm.DB {
	if topicID != "" {
		q = q.Where("subject = ?", topicID)
	}
	if chapterNo > 0 {
		q = q.Where("chapter_no = ?", chapterNo)
	}
	return q
}

// cacheScope is the Redis code-pool key segment for a subject/chapter.
func cacheScope(topicID string, chapterNo int) string {
	if chapterNo > 0 {
		return fmt.Sprintf("%s:ch%d", topicID, chapterNo)
	}
	return topicID
}

// sampleRandomCodes returns up to limit randomly chosen codes.
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

// GetQuestionsByTopic fetches random MCQ questions for a subject (and optional chapter).
func GetQuestionsByTopic(ctx context.Context, topicID string, chapterNo, limit int) ([]models.Question, error) {
	return GetQuestionsByTopicAndMode(ctx, topicID, chapterNo, models.GameModeMCQ, limit)
}

// GetQuestionsByTopicAndMode samples question codes from the mode's table (Redis code pool
// first, then an index-only scan) and loads the sampled rows.
func GetQuestionsByTopicAndMode(ctx context.Context, topicID string, chapterNo int, mode string, limit int) ([]models.Question, error) {
	if mode != models.GameModeSuddenDeath {
		mode = models.GameModeMCQ
	}
	key := cacheScope(topicID, chapterNo)
	if cachedCodes, ok := cache.GetCachedQuestionCodes(ctx, key, mode); ok && len(cachedCodes) >= limit {
		return GetQuestionsByCodes(ctx, sampleRandomCodes(cachedCodes, limit))
	}

	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var codes []string
	if err := scope(db.WithContext(ctx).Table(questionTable(mode)), topicID, chapterNo).
		Pluck("question_code", &codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, nil
	}
	_ = cache.SetCachedQuestionCodes(ctx, key, mode, codes)
	return GetQuestionsByCodes(ctx, sampleRandomCodes(codes, limit))
}

// GetQuestionsByCodes loads MCQ and Sudden Death questions by code, preserving the
// requested order. Codes are unique across both tables (mode tag inside the code).
func GetQuestionsByCodes(ctx context.Context, codes []string) ([]models.Question, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	byCode := make(map[string]models.Question, len(codes))
	for _, mode := range []string{models.GameModeMCQ, models.GameModeSuddenDeath} {
		var rows []models.Question
		if err := db.WithContext(ctx).Table(questionTable(mode)).
			Where("question_code IN ?", codes).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, q := range rows {
			q.QuestionType = mode
			byCode[q.QuestionCode] = q
		}
	}
	ordered := make([]models.Question, 0, len(codes))
	for _, code := range codes {
		if q, ok := byCode[code]; ok {
			ordered = append(ordered, q)
		}
	}
	return ordered, nil
}

// GetQuestionByCode fetches a single MCQ or Sudden Death question by code.
func GetQuestionByCode(ctx context.Context, code string) (*models.Question, error) {
	qs, err := GetQuestionsByCodes(ctx, []string{code})
	if err != nil {
		return nil, err
	}
	if len(qs) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &qs[0], nil
}

// GetContentCounts returns per subject/chapter/mode question counts, cached for an hour.
func GetContentCounts(ctx context.Context) ([]models.ContentCount, error) {
	val, err, _ := cache.RequestGroup.Do("content_counts", func() (interface{}, error) {
		db := GetDB()
		if db == nil {
			return []models.ContentCount{}, nil
		}
		var rows []models.ContentCount
		err := db.WithContext(ctx).
			Order("sort_order, topic_id, chapter_no, game_mode").
			Find(&rows).Error
		return rows, err
	})
	if err != nil {
		return nil, err
	}
	return val.([]models.ContentCount), nil
}

// GetAvailableTopics returns each subject with its MCQ question count.
func GetAvailableTopics(ctx context.Context) ([]map[string]interface{}, error) {
	if cached, ok := cache.GetCachedTopics(ctx); ok && len(cached) > 0 {
		return cached, nil
	}
	rows, err := GetContentCounts(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	var order []string
	for _, r := range rows {
		if r.GameMode != models.GameModeMCQ {
			continue
		}
		if _, ok := counts[r.TopicID]; !ok {
			order = append(order, r.TopicID)
		}
		counts[r.TopicID] += r.QuestionCount
	}
	results := make([]map[string]interface{}, 0, len(order))
	for _, t := range order {
		results = append(results, map[string]interface{}{"topic_id": t, "count": counts[t]})
	}
	if len(results) > 0 {
		_ = cache.SetCachedTopics(ctx, results)
	}
	return results, nil
}

// CountQuestionsByTopicAndMode counts MCQ or Sudden Death questions in a subject/chapter.
func CountQuestionsByTopicAndMode(ctx context.Context, topicID string, chapterNo int, mode string) (int64, error) {
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	var count int64
	err := scope(db.WithContext(ctx).Table(questionTable(mode)), topicID, chapterNo).Count(&count).Error
	return count, err
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
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(history).Error
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

// finalizeSQL commits a finished game in one statement (one round trip, atomic):
// reward ledger rows, balances + level, today's activity day and the leaderboard row.
// The leaderboard insert is the idempotency guard: if the session was already
// finalized nothing else is written.
const finalizeSQL = `
WITH result AS (
    INSERT INTO leaderboard_game_results (session_id, client_id, topic_id, game_mode, final_score, completed_at)
    VALUES (@session, @client, @topic, @mode, @score, @now)
    ON CONFLICT (session_id) DO NOTHING
    RETURNING client_id
), ledger AS (
    INSERT INTO wallet_ledger (client_id, transaction_type, amount, currency, reference_id)
    SELECT r.client_id, @tx_type, v.amount, v.currency, @session
    FROM result r, (VALUES (CAST(@coins AS int), 'coins'), (CAST(@xp AS int), 'experience'), (CAST(@gems AS int), 'gems')) AS v (amount, currency)
    WHERE v.amount > 0
), activity AS (
    INSERT INTO client_activity_days (client_id, day)
    SELECT client_id, CAST(@now AS date) FROM result
    ON CONFLICT DO NOTHING
)
UPDATE clients c
SET coins = coins + @coins, gems = gems + @gems, experience = experience + @xp,
    weekly_score = weekly_score + @score, level = GREATEST(level, @level), updated_at = now()
FROM result r
WHERE c.id = r.client_id`

// FinalizeSession commits rewards, level, streak day and the leaderboard result in one
// round trip, then updates the Redis leaderboard and clears ephemeral session keys.
func FinalizeSession(ctx context.Context, session *models.GameSession, coins, xp, gems, level int) error {
	now := time.Now().UTC()
	session.Status = models.SessionStatusFinished
	session.EndedAt = &now

	db := GetDB()
	if db != nil {
		err := db.WithContext(ctx).Exec(finalizeSQL, map[string]any{
			"session": session.ID,
			"client":  session.ClientID,
			"topic":   session.TopicID,
			"mode":    session.GameMode,
			"score":   session.Score,
			"now":     now,
			"tx_type": models.TxTypeQuizReward,
			"coins":   coins,
			"xp":      xp,
			"gems":    gems,
			"level":   level,
		}).Error
		if err != nil {
			return fmt.Errorf("failed to finalize session: %w", err)
		}
	} else {
		// Record in-memory for testing when db is nil
		_ = RecordInMemoryLeaderboardResult(&models.LeaderboardGameResult{
			SessionID:   session.ID,
			ClientID:    session.ClientID,
			TopicID:     session.TopicID,
			GameMode:    session.GameMode,
			FinalScore:  session.Score,
			CompletedAt: now,
		})
	}

	// Redis real-time leaderboard update
	if session.Score > 0 {
		_ = cache.UpdateWeeklyLeaderboard(ctx, session.ClientID, session.Score)
	}

	// Invalidate profile cache and clear client active exclusion lock
	_ = cache.InvalidateCachedProfile(ctx, session.ClientID)
	_ = ClearClientActiveSession(ctx, session.ClientID)
	_ = CacheSession(ctx, session)

	return nil
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
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(history, 50).Error
}

// --- Sort It Out ---

// GetDistinctPairTypes returns the sort set codes available in a subject/chapter.
func GetDistinctPairTypes(ctx context.Context, topicID string, chapterNo int) ([]string, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var pairTypes []string
	err := scope(db.WithContext(ctx).Model(&models.SortItOutQuestion{}), topicID, chapterNo).
		Distinct("set_code").Pluck("set_code", &pairTypes).Error
	return pairTypes, err
}

// GetSortItOutQuestions samples up to limit items from a subject/chapter, optionally one set.
func GetSortItOutQuestions(ctx context.Context, topicID string, chapterNo int, pairType string, limit int) ([]models.SortItOutQuestion, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	query := scope(db.WithContext(ctx).Model(&models.SortItOutQuestion{}), topicID, chapterNo)
	if pairType != "" {
		query = query.Where("set_code = ?", pairType)
	}
	var codes []string
	if err := query.Pluck("question_code", &codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, nil
	}
	var questions []models.SortItOutQuestion
	err := db.WithContext(ctx).
		Where("question_code IN ?", sampleRandomCodes(codes, limit)).
		Find(&questions).Error
	return questions, err
}

// GetSortItOutQuestionsByCodes loads sort items by code, preserving the requested order.
func GetSortItOutQuestionsByCodes(ctx context.Context, codes []string) ([]models.SortItOutQuestion, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var questions []models.SortItOutQuestion
	if err := db.WithContext(ctx).Where("question_code IN ?", codes).Find(&questions).Error; err != nil {
		return nil, err
	}
	byCode := make(map[string]models.SortItOutQuestion, len(questions))
	for _, q := range questions {
		byCode[q.QuestionCode] = q
	}
	ordered := make([]models.SortItOutQuestion, 0, len(codes))
	for _, code := range codes {
		if q, ok := byCode[code]; ok {
			ordered = append(ordered, q)
		}
	}
	return ordered, nil
}

// --- Match the Following ---

// GetMatchPairs loads every pair of a subject (and optional chapter). Pair counts per
// chapter are small, and the service needs all of them to deal fresh boards each game.
func GetMatchPairs(ctx context.Context, topicID string, chapterNo int) ([]models.MatchPair, error) {
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var pairs []models.MatchPair
	err := scope(db.WithContext(ctx).Model(&models.MatchPair{}), topicID, chapterNo).Find(&pairs).Error
	return pairs, err
}

// GetMatchPairsByCodes loads pairs by code, preserving the requested order.
func GetMatchPairsByCodes(ctx context.Context, codes []string) ([]models.MatchPair, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	db := GetDB()
	if db == nil {
		return nil, nil
	}
	var pairs []models.MatchPair
	if err := db.WithContext(ctx).Where("question_code IN ?", codes).Find(&pairs).Error; err != nil {
		return nil, err
	}
	byCode := make(map[string]models.MatchPair, len(pairs))
	for _, p := range pairs {
		byCode[p.ID] = p
	}
	ordered := make([]models.MatchPair, 0, len(codes))
	for _, code := range codes {
		if p, ok := byCode[code]; ok {
			ordered = append(ordered, p)
		}
	}
	return ordered, nil
}
