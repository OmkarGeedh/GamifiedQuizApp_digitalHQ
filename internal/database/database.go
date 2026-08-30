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
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("gorm.Open(): %w", err)
	}

	// Configure underlying connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("db.DB(): %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(15 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	// Verify connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("sqlDB.Ping(): %w", err)
	}

	log.Println("GORM database connection established successfully")
	return db, nil
}
