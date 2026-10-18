package db

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(databaseURL string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	const maxRetries = 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		db, err = gorm.Open(postgres.Open(databaseURL), &gorm.Config{
			PrepareStmt:            true, // Cache pre-compiled SQL query plans on PostgreSQL
			SkipDefaultTransaction: true, // Disables implicit BEGIN/COMMIT on single queries (saves 1 RTT per operation)
			Logger:                 logger.Default.LogMode(logger.Warn),
		})
		if err == nil {
			if sqlDB, dbErr := db.DB(); dbErr == nil {
				if pingErr := sqlDB.Ping(); pingErr == nil {
					sqlDB.SetMaxOpenConns(50)                  // Max pool connections
					sqlDB.SetMaxIdleConns(25)                  // Warm idle pool to prevent connection churn
					sqlDB.SetConnMaxLifetime(30 * time.Minute) // Connection recycling
					sqlDB.SetConnMaxIdleTime(5 * time.Minute)  // Evict idle connections
					log.Println("GORM database connection established successfully (PrepareStmt=true, SkipDefaultTx=true)")
					return db, nil
				} else {
					err = pingErr
				}
			} else {
				err = dbErr
			}
		}

		log.Printf("Database connection attempt %d/%d failed: %v. Retrying in 2s...\n", attempt, maxRetries, err)
		time.Sleep(2 * time.Second)
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, err)
}
