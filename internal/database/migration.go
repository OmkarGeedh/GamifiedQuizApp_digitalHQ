package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	authModels "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/auth/client/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	gameModels "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/models"
	profileModels "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/profile/models"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AllModels lists all GORM entity models across the application
func AllModels() []interface{} {
	return []interface{}{
		&authModels.Client{},
		&profileModels.Profile{},
		&profileModels.StreakActivity{},
		&gameModels.Question{},
		&gameModels.GameSession{},
		&gameModels.UserQuestionHistory{},
		&gameModels.WalletLedger{},
	}
}

// AutoMigrate runs schema auto-migrations for all application models
func AutoMigrate(db *gorm.DB) error {
	log.Println("==> Running database auto-migrations...")

	models := AllModels()
	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			return fmt.Errorf("failed to migrate model %T: %w", m, err)
		}
	}

	log.Println("==> Database auto-migrations completed successfully! [clients, client_profiles, client_streak_activities, questions, game_sessions, user_question_history, wallet_ledger]")
	return nil
}

// DropAll drops all application tables (use with caution)
func DropAll(db *gorm.DB) error {
	log.Println("==> Dropping all application tables...")

	models := AllModels()
	for i := len(models) - 1; i >= 0; i-- {
		if err := db.Migrator().DropTable(models[i]); err != nil {
			return fmt.Errorf("failed to drop table for %T: %w", models[i], err)
		}
	}

	log.Println("==> All application tables dropped successfully.")
	return nil
}

// Reset drops all tables and re-applies migrations
func Reset(db *gorm.DB) error {
	if err := DropAll(db); err != nil {
		return err
	}
	return AutoMigrate(db)
}

// Seed populates the database with initial demo data
func Seed(db *gorm.DB) error {
	log.Println("==> Seeding database with initial data...")
	ctx := context.Background()

	// 1. Seed demo player
	var existing authModels.Client
	err := db.WithContext(ctx).Where("email = ?", "player@example.com").First(&existing).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		passwordSecret := "super_secret_pepper_salt"
		if config.AppConfig != nil && config.AppConfig.Server.PasswordSecret != "" {
			passwordSecret = config.AppConfig.Server.PasswordSecret
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte("secretpassword123"+passwordSecret), 12)
		if err != nil {
			return fmt.Errorf("failed to hash seed password: %w", err)
		}

		phone := "+1-555-0199"
		client := authModels.Client{
			Username: "quizmaster",
			Email:    "player@example.com",
			Phone:    &phone,
			Password: string(hashed),
			Status:   "active",
		}
		if err := db.WithContext(ctx).Create(&client).Error; err != nil {
			return fmt.Errorf("failed to seed demo client: %w", err)
		}

		now := time.Now().UTC()
		todayStr := now.Format("2006-01-02")
		todayDate, _ := time.Parse("2006-01-02", todayStr)

		profile := profileModels.Profile{
			ClientID:       client.ID,
			UUID:           uuid.New().String(),
			FullName:       "Pro Quiz Champion",
			Coins:          250,
			Gems:           50,
			Experience:     120,
			Level:          2,
			CurrentStreak:  3,
			HighestStreak:  7,
			LastActiveDate: todayDate,
			WeeklyScore:    450,
		}
		if err := db.WithContext(ctx).Create(&profile).Error; err != nil {
			return fmt.Errorf("failed to seed demo profile: %w", err)
		}

		// Seed streak activities for the last 3 days
		for i := 2; i >= 0; i-- {
			d := now.AddDate(0, 0, -i).Format("2006-01-02")
			db.WithContext(ctx).Create(&profileModels.StreakActivity{
				ClientID:     client.ID,
				ActivityDate: d,
				Completed:    true,
			})
		}

		log.Println("==> Seeded demo user: player@example.com / secretpassword123")
	} else {
		log.Println("==> Demo user already exists, skipping seed.")
	}

	// Seed MCQ questions from JSON file
	if err := seedQuestions(ctx, db); err != nil {
		log.Printf("Warning: Question seeding failed: %v\n", err)
	}

	return nil
}

// seedQuestions loads 100 MCQ questions from the embedded JSON seed file.
func seedQuestions(ctx context.Context, db *gorm.DB) error {
	var existingCount int64
	db.WithContext(ctx).Model(&gameModels.Question{}).Count(&existingCount)
	if existingCount > 0 {
		log.Printf("==> Refreshing questions table (%d existing rows with latest seed data)...\n", existingCount)
		if err := db.WithContext(ctx).Exec("TRUNCATE TABLE questions RESTART IDENTITY CASCADE").Error; err != nil {
			return fmt.Errorf("failed to clear existing questions: %w", err)
		}
	}

	// Try multiple paths to find the seed file
	paths := []string{
		"internal/game/data/seed_questions.json",
		"./internal/game/data/seed_questions.json",
		"/app/internal/game/data/seed_questions.json",
	}

	var fileData []byte
	var readErr error
	for _, p := range paths {
		fileData, readErr = os.ReadFile(p)
		if readErr == nil {
			break
		}
	}
	if readErr != nil {
		return fmt.Errorf("failed to read seed_questions.json: %w", readErr)
	}

	type seedQuestion struct {
		QuestionCode  string  `json:"question_code"`
		TopicID       string  `json:"topic_id"`
		Prompt        string  `json:"prompt"`
		OptionA       string  `json:"option_a"`
		OptionB       string  `json:"option_b"`
		OptionC       string  `json:"option_c"`
		OptionD       string  `json:"option_d"`
		CorrectOption string  `json:"correct_option"`
		Difficulty    int     `json:"difficulty"`
		Points        int     `json:"points"`
		Hint          *string `json:"hint"`
		Explanation   *string `json:"explanation"`
	}

	var seeds []seedQuestion
	if err := json.Unmarshal(fileData, &seeds); err != nil {
		return fmt.Errorf("failed to parse seed_questions.json: %w", err)
	}

	questions := make([]gameModels.Question, 0, len(seeds))
	for _, s := range seeds {
		var hint *string
		if s.Hint != nil && *s.Hint != "" {
			h := *s.Hint
			hint = &h
		}
		var explanation *string
		if s.Explanation != nil && *s.Explanation != "" {
			e := *s.Explanation
			explanation = &e
		}
		q := gameModels.Question{
			QuestionCode:  s.QuestionCode,
			TopicID:       s.TopicID,
			Prompt:        s.Prompt,
			OptionA:       s.OptionA,
			OptionB:       s.OptionB,
			OptionC:       s.OptionC,
			OptionD:       s.OptionD,
			CorrectOption: s.CorrectOption,
			Difficulty:    s.Difficulty,
			Points:        s.Points,
			Hint:          hint,
			Explanation:   explanation,
		}
		questions = append(questions, q)
	}

	if err := db.WithContext(ctx).CreateInBatches(questions, 50).Error; err != nil {
		return fmt.Errorf("failed to seed questions: %w", err)
	}

	log.Printf("==> Seeded %d MCQ questions successfully.\n", len(questions))
	return nil
}

// Status inspects the current database status and row counts
func Status(db *gorm.DB) error {
	log.Println("================================================================")
	log.Println("                    DATABASE SCHEMA STATUS                      ")
	log.Println("================================================================")

	tables := []string{"clients", "client_profiles", "client_streak_activities", "questions", "game_sessions", "user_question_history", "wallet_ledger"}
	for _, t := range tables {
		hasTable := db.Migrator().HasTable(t)
		if hasTable {
			var count int64
			db.Table(t).Count(&count)
			log.Printf("  [OK] Table '%s' exists | Total Rows: %d\n", t, count)
		} else {
			log.Printf("  [MISSING] Table '%s' does not exist\n", t)
		}
	}
	log.Println("================================================================")
	return nil
}
