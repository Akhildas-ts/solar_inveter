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
var mongoDB *MongoDatabase // ✅ Always keep MongoDB for mappings

// InitDB initializes the database based on DB_TYPE environment variable
func InitDB() error {
	dbType := strings.ToLower(getEnv("DB_TYPE", "mongo"))

	// ✅ ALWAYS initialize MongoDB (for mappings)
	mongoDB = NewMongoDatabase()
	if err := mongoDB.Connect(); err != nil {
		return fmt.Errorf("failed to connect to MongoDB (required for mappings): %w", err)
	}
	fmt.Println("✓ MongoDB connected (for mappings)")

	// ✅ Initialize data storage database
	switch DatabaseType(dbType) {
	case MongoDB:
		activeDB = mongoDB
		fmt.Println("✓ Using MongoDB for data storage")
		
	case InfluxDB:
		activeDB = NewInfluxDatabase()
		if err := activeDB.Connect(); err != nil {
			return fmt.Errorf("failed to connect to InfluxDB: %w", err)
		}
		fmt.Println("✓ Using InfluxDB for data storage")
		
	default:
		return fmt.Errorf("unsupported database type: %s (use 'mongo' or 'influx')", dbType)
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
	// Close data storage DB
	if activeDB != nil {
		activeDB.Close()
	}
	
	// Close MongoDB if it's not the active DB
	if mongoDB != nil && GetDBType() != MongoDB {
		mongoDB.Close()
	}
}

// ✅ Always return MongoDB client (for mappings)
func GetMongoDBForMappings() *MongoDatabase {
	return mongoDB
}

// Helper function to get environment variables with default values
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}