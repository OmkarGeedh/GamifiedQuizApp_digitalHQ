package repo

import (
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"gorm.io/gorm"
)

// GetDB returns the active database connection pool.
func GetDB() *gorm.DB {
	return config.DB
}
