package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"gorm.io/gorm"
)

// GetQuestionsByTopic fetches random questions for a given topic.
// Uses PostgreSQL ORDER BY RANDOM() with a row limit for efficient selection.
func GetQuestionsByTopic(ctx context.Context, topicID string, limit int) ([]models.Question, error) {
	var questions []models.Question
	err := GetDB().WithContext(ctx).
		Where("topic_id = ?", topicID).
		Order("RANDOM()").
		Limit(limit).
		Find(&questions).Error
	return questions, err
}

// GetQuestionByCode fetches a single question by its unique code.
func GetQuestionByCode(ctx context.Context, code string) (*models.Question, error) {
	var q models.Question
	err := GetDB().WithContext(ctx).
		Where("question_code = ?", code).
		First(&q).Error
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// GetAvailableTopics returns distinct topic IDs with question counts.
func GetAvailableTopics(ctx context.Context) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := GetDB().WithContext(ctx).
		Model(&models.Question{}).
		Select("topic_id, COUNT(*) as count").
		Group("topic_id").
		Order("topic_id ASC").
		Find(&results).Error
	return results, err
}

// CountQuestionsByTopic returns the number of questions for a topic.
func CountQuestionsByTopic(ctx context.Context, topicID string) (int64, error) {
	var count int64
	err := GetDB().WithContext(ctx).
		Model(&models.Question{}).
		Where("topic_id = ?", topicID).
		Count(&count).Error
	return count, err
}

// --- Session Repository ---

// CreateSession inserts a new game session.
func CreateSession(ctx context.Context, session *models.GameSession) error {
	return GetDB().WithContext(ctx).Create(session).Error
}

// GetSessionByID fetches a session by its UUID.
func GetSessionByID(ctx context.Context, sessionID string) (*models.GameSession, error) {
	var s models.GameSession
	err := GetDB().WithContext(ctx).
		Where("id = ?", sessionID).
		First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetActiveSessionByClientID fetches an in-progress session for a client (if any).
func GetActiveSessionByClientID(ctx context.Context, clientID int) (*models.GameSession, error) {
	var s models.GameSession
	err := GetDB().WithContext(ctx).
		Where("client_id = ? AND status = ?", clientID, models.SessionStatusInProgress).
		Order("created_at DESC").
		First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSession persists updated session fields.
func UpdateSession(ctx context.Context, session *models.GameSession) error {
	return GetDB().WithContext(ctx).Save(session).Error
}

// --- History Repository ---

// RecordAnswer inserts an immutable answer history record.
func RecordAnswer(ctx context.Context, history *models.UserQuestionHistory) error {
	return GetDB().WithContext(ctx).Create(history).Error
}

// HasAnsweredQuestion checks if a question was already answered in a given session.
func HasAnsweredQuestion(ctx context.Context, sessionID, questionCode string) (bool, error) {
	var count int64
	err := GetDB().WithContext(ctx).
		Model(&models.UserQuestionHistory{}).
		Where("session_id = ? AND question_code = ?", sessionID, questionCode).
		Count(&count).Error
	return count > 0, err
}

// GetSessionHistory returns all answer records for a session ordered by time.
func GetSessionHistory(ctx context.Context, sessionID string) ([]models.UserQuestionHistory, error) {
	var records []models.UserQuestionHistory
	err := GetDB().WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("answered_at ASC").
		Find(&records).Error
	return records, err
}

// GetClientGameHistory returns completed session summaries for a client (paginated).
func GetClientGameHistory(ctx context.Context, clientID, limit, offset int) ([]models.GameSession, int64, error) {
	var sessions []models.GameSession
	var total int64

	db := GetDB().WithContext(ctx).Model(&models.GameSession{}).Where("client_id = ?", clientID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&sessions).Error
	return sessions, total, err
}

// --- Finalization (Atomic Transaction) ---

// FinalizeSession atomically marks a session as finished, writes wallet ledger entries,
// and updates the player's profile balances. This is the only path that awards rewards.
func FinalizeSession(ctx context.Context, session *models.GameSession, coins, xp, gems int) error {
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Mark session as finished
		now := time.Now().UTC()
		session.Status = models.SessionStatusFinished
		session.EndedAt = &now
		if err := tx.Save(session).Error; err != nil {
			return fmt.Errorf("failed to finalize session: %w", err)
		}

		// 2. Write immutable wallet ledger entries
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
			if err := tx.Create(&ledgerEntries).Error; err != nil {
				return fmt.Errorf("failed to write wallet ledger: %w", err)
			}
		}

		// 3. Update profile balances (cached aggregates for fast reads)
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

		// 4. Record daily streak activity
		todayStr := now.Format("2006-01-02")
		_ = RecordDailyActivity(ctx, session.ClientID, todayStr)

		return nil
	})
}

// --- Seed Helpers ---

// BulkInsertQuestions inserts a batch of questions (used by the seed command).
func BulkInsertQuestions(ctx context.Context, questions []models.Question) error {
	return GetDB().WithContext(ctx).CreateInBatches(questions, 50).Error
}

// QuestionCount returns the total number of questions in the database.
func QuestionCount(ctx context.Context) (int64, error) {
	var count int64
	err := GetDB().WithContext(ctx).Model(&models.Question{}).Count(&count).Error
	return count, err
}

// UpdateProfileLevel updates only the level column on the player's profile.
func UpdateProfileLevel(ctx context.Context, clientID, newLevel int) error {
	return GetDB().WithContext(ctx).
		Table("client_profiles").
		Where("client_id = ?", clientID).
		Update("level", newLevel).Error
}
