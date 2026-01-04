package db

import (
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// Init initializes the SQLite database connection and performs auto-migration
func Init(dbPath string) {
	if dbPath == "" {
		dbPath = "qiservice.db"
	}

	// Ensure directory exists if path contains dir
	dir := filepath.Dir(dbPath)
	if dir != "." {
		os.MkdirAll(dir, 0755)
	}

	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}

	log.Println("✅ Database connection established.")

	// Auto Migrate Schema
	err = DB.AutoMigrate(
		&User{},
		&APIKey{},
		&Service{},
		&RequestLog{},
	)
	if err != nil {
		log.Fatalf("❌ Database migration failed: %v", err)
	}
	log.Println("✅ Database schema migrated.")
}
