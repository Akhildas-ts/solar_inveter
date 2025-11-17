package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds application configuration
type Config struct {
	// Server
	ServerPort int

	// Database
	DBType     string // "mongo" or "influx"
	
	// MongoDB
	MongoURI        string
	MongoDB         string
	MongoCollection string

	// InfluxDB
	InfluxURL      string
	InfluxToken    string
	InfluxDatabase string

	// Processing
	BatchSize       int
	FlushInterval   int // milliseconds
	StrictMode      bool
	
	// Logging
	LogLevel string
	LogDir   string
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		ServerPort:      getEnvInt("SERVER_PORT", 8080),
		DBType:          getEnv("DB_TYPE", "mongo"),
		
		// MongoDB
		MongoURI:        getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:         getEnv("MONGO_DATABASE", "solar_monitoring"),
		MongoCollection: getEnv("MONGO_COLLECTION", "inverter_data"),
		
		// InfluxDB
		InfluxURL:      getEnv("INFLUXDB_URL", "http://localhost:8086"),
		InfluxToken:    getEnv("INFLUXDB_TOKEN", ""),
		InfluxDatabase: getEnv("INFLUXDB_DATABASE", "solar_monitoring"),
		
		// Processing
		BatchSize:     getEnvInt("BATCH_SIZE", 100),
		FlushInterval: getEnvInt("FLUSH_INTERVAL", 200),
		StrictMode:    getEnvBool("STRICT_MAPPING_MODE", false),
		
		// Logging
		LogLevel: getEnv("LOG_LEVEL", "INFO"),
		LogDir:   getEnv("LOG_DIRECTORY", "./logs"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks if configuration is valid
func (c *Config) Validate() error {
	if c.DBType != "mongo" && c.DBType != "influx" {
		return fmt.Errorf("invalid DB_TYPE: %s (use 'mongo' or 'influx')", c.DBType)
	}

	if c.BatchSize < 1 || c.BatchSize > 10000 {
		return fmt.Errorf("invalid BATCH_SIZE: %d (must be 1-10000)", c.BatchSize)
	}

	if c.FlushInterval < 50 || c.FlushInterval > 5000 {
		return fmt.Errorf("invalid FLUSH_INTERVAL: %d (must be 50-5000ms)", c.FlushInterval)
	}

	return nil
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}