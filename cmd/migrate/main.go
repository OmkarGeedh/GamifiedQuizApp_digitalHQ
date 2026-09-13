package main

import (
	"flag"
	"log"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	db "github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/database"
)

func main() {
	action := flag.String("action", "up", "Migration action: up | down | reset | seed | status")
	flag.Parse()

	// Load configuration
	if err := config.LoadEnv(); err != nil {
		log.Printf("Warning loading .env: %v\n", err)
	}

	databaseURL := config.AppConfig.Database.ConnectionString()

	pgDB, err := db.Connect(databaseURL)
	if err != nil {
		log.Fatalf("Fatal: Database connection failed: %v\n", err)
	}

	sqlDB, err := pgDB.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	switch *action {
	case "up", "migrate":
		if err := db.AutoMigrate(pgDB); err != nil {
			log.Fatalf("Migration failed: %v\n", err)
		}
		_ = db.Status(pgDB)

	case "down", "drop":
		if err := db.DropAll(pgDB); err != nil {
			log.Fatalf("Drop failed: %v\n", err)
		}
		_ = db.Status(pgDB)

	case "reset":
		if err := db.Reset(pgDB); err != nil {
			log.Fatalf("Reset failed: %v\n", err)
		}
		_ = db.Status(pgDB)

	case "seed":
		if err := db.Seed(pgDB); err != nil {
			log.Fatalf("Seeding failed: %v\n", err)
		}
		_ = db.Status(pgDB)

	case "status":
		if err := db.Status(pgDB); err != nil {
			log.Fatalf("Status check failed: %v\n", err)
		}

	default:
		log.Fatalf("Unknown action '%s'. Supported actions: up, down, reset, seed, status\n", *action)
	}
}
