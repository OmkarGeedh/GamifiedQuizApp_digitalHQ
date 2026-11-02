// Package testutil provides an in-process SQLite database for tests that need real
// SQL semantics (unique indexes, transactions) without a Postgres or Redis fixture.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewDB creates a fresh SQLite database with the client/profile tables, installs it as
// config.DB for the duration of the test, and mirrors migration 000010's unique index.
func NewDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		TranslateError: true, // same setting as database.Connect
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // SQLite allows one writer; the unique index still decides races.

	if err := db.AutoMigrate(&models.Client{}, &models.Profile{}, &models.StreakActivity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uq_clients_display_name_ci ON clients (lower(trim(username)))`).Error; err != nil {
		t.Fatalf("create display-name index: %v", err)
	}

	prev := config.DB
	config.DB = db
	t.Cleanup(func() {
		config.DB = prev
		_ = sqlDB.Close()
	})
	return db
}

// SeedClient inserts an active client and returns its ID.
func SeedClient(t *testing.T, db *gorm.DB, username string) int {
	t.Helper()
	c := models.Client{Username: username, Email: username + "@example.com", Password: "x", Status: "active"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("seed client %q: %v", username, err)
	}
	return c.ID
}
