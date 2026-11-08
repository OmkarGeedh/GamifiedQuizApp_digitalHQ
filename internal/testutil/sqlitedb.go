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

	// Client and Profile share the clients table. Migrate Client first (it owns the
	// account columns), then Profile (it adds the game columns); one combined call
	// can create the table from Profile alone and lose the username column.
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatalf("migrate client: %v", err)
	}
	if err := db.AutoMigrate(&models.Profile{}); err != nil {
		t.Fatalf("migrate profile: %v", err)
	}
	// Streak days live in client_activity_days, which has no model any more (the
	// repo reads and writes it with plain SQL), so create the table directly.
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS client_activity_days (
		client_id INTEGER NOT NULL,
		day DATE NOT NULL,
		PRIMARY KEY (client_id, day)
	)`).Error; err != nil {
		t.Fatalf("create client_activity_days: %v", err)
	}
	// clients.streak_freezes comes from migration 000007 in the migration repo; it is
	// not a model field, so add it here.
	if err := db.Exec(`ALTER TABLE clients ADD COLUMN streak_freezes INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
		t.Fatalf("add streak_freezes: %v", err)
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
	// Like a fresh signup: the game columns take their defaults and full_name is
	// empty until the profile is set up. Plain SQL because Client has no full_name.
	res := db.Exec(
		`INSERT INTO clients (username, email, password, status, full_name, created_at, updated_at)
		 VALUES (?, ?, 'x', 'active', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		username, username+"@example.com",
	)
	if res.Error != nil {
		t.Fatalf("seed client %q: %v", username, res.Error)
	}
	var id int
	if err := db.Raw(`SELECT id FROM clients WHERE username = ?`, username).Scan(&id).Error; err != nil {
		t.Fatalf("read seeded client id %q: %v", username, err)
	}
	return id
}
