package config

import (
	"fmt"
	"os"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

var (
	client *influxdb3.Client
)

// InitDB initializes the InfluxDB connection
func InitDB() error {
	// Get configuration from environment variables
	url := getEnv("INFLUXDB_URL", "https://us-east-1-1.aws.cloud2.influxdata.com")
	token := getEnv("INFLUXDB_TOKEN", "")

	if token == "" {
		return fmt.Errorf("INFLUXDB_TOKEN environment variable is required")
	}

	// Initialize InfluxDB 3.0 client
	var err error
	client, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     url,
		Token:    token,
		Database: getEnv("INFLUXDB_DATABASE", "solar_monitoring"),
	})

	if err != nil {
		return fmt.Errorf("failed to create InfluxDB client: %w", err)
	}

	fmt.Println("✓ Connected to InfluxDB 3.0 successfully!")

	return nil
}

// GetClient returns the InfluxDB client
func GetClient() *influxdb3.Client {
	return client
}

// CloseDB closes the InfluxDB connection
func CloseDB() {
	if client != nil {
		client.Close()
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
