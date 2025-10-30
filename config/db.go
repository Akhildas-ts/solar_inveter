package config

import (
	"fmt"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

// InfluxDatabase implements the Database interface for InfluxDB
type InfluxDatabase struct {
	client    *influxdb3.Client
	database  string
	connected bool
}

// NewInfluxDatabase creates a new InfluxDB database instance
func NewInfluxDatabase() *InfluxDatabase {
	return &InfluxDatabase{}
}

// Connect establishes connection to InfluxDB
func (i *InfluxDatabase) Connect() error {
	url := getEnv("INFLUXDB_URL", "https://us-east-1-1.aws.cloud2.influxdata.com")
	token := getEnv("INFLUXDB_TOKEN", "")
	i.database = getEnv("INFLUXDB_DATABASE", "solar_monitoring")

	if token == "" {
		return fmt.Errorf("INFLUXDB_TOKEN environment variable is required")
	}

	// ✅ ADD DEBUG: Show what we're connecting to
	fmt.Println("\n🔧 InfluxDB Connection Details:")
	fmt.Printf("   URL: %s\n", url)
	fmt.Printf("   Database: %s\n", i.database)
	fmt.Printf("   Token: %s...%s\n", token[:10], token[len(token)-10:])

	var err error
	i.client, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     url,
		Token:    token,
		Database: i.database,
	})

	if err != nil {
		return fmt.Errorf("failed to create InfluxDB client: %w", err)
	}

	i.connected = true

	fmt.Println("✓ InfluxDB client created successfully!")
	fmt.Printf("  URL: %s\n", url)
	fmt.Printf("  Database: %s\n", i.database)

	return nil
}

// Close closes the InfluxDB connection
func (i *InfluxDatabase) Close() {
	if i.client != nil {
		i.client.Close()
		i.connected = false
		fmt.Println("✓ InfluxDB connection closed")
	}
}

// GetType returns the database type
func (i *InfluxDatabase) GetType() DatabaseType {
	return InfluxDB
}

// IsConnected returns connection status
func (i *InfluxDatabase) IsConnected() bool {
	return i.connected
}

// GetClient returns the InfluxDB client
func (i *InfluxDatabase) GetClient() *influxdb3.Client {
	return i.client
}

// GetDatabase returns the InfluxDB database name
func (i *InfluxDatabase) GetDatabase() string {
	return i.database
}

// Helper function to get InfluxDB client from activeDB
func GetInfluxClient() *influxdb3.Client {
	if activeDB == nil {
		return nil
	}
	if influxDB, ok := activeDB.(*InfluxDatabase); ok {
		return influxDB.GetClient()
	}
	return nil
}

// Helper function to get InfluxDB database name from activeDB
func GetInfluxDatabase() string {
	if activeDB == nil {
		return ""
	}
	if influxDB, ok := activeDB.(*InfluxDatabase); ok {
		return influxDB.GetDatabase()
	}
	return ""
}
