package config

import (
	"fmt"
	"os"
	"strings"
)

// DatabaseType represents the type of database being used
type DatabaseType string

const (
	MongoDB  DatabaseType = "mongo"
	InfluxDB DatabaseType = "influx"
)

// Database interface that both implementations must follow
type Database interface {
	Connect() error
	Close()
	GetType() DatabaseType
	IsConnected() bool
}

var activeDB Database

// InitDB initializes the database based on DB_TYPE environment variable
func InitDB() error {
	dbType := strings.ToLower(getEnv("DB_TYPE", "mongo"))

	switch DatabaseType(dbType) {
	case MongoDB:
		activeDB = NewMongoDatabase()
	case InfluxDB:
		activeDB = NewInfluxDatabase()
	default:
		return fmt.Errorf("unsupported database type: %s (use 'mongo' or 'influx')", dbType)
	}

	if err := activeDB.Connect(); err != nil {
		return fmt.Errorf("failed to connect to %s: %w", dbType, err)
	}

	fmt.Printf("✓ Connected to %s successfully!\n", dbType)
	return nil
}

// GetActiveDB returns the currently active database
func GetActiveDB() Database {
	return activeDB
}

// GetDBType returns the current database type
func GetDBType() DatabaseType {
	if activeDB == nil {
		return ""
	}
	return activeDB.GetType()
}

// CloseDB closes the active database connection
func CloseDB() {
	if activeDB != nil {
		activeDB.Close()
	}
}

// Helper function to get environment variables with default values
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}