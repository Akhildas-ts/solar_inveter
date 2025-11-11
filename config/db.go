package config

import (
	"fmt"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

// InfluxDatabase implements the Database interface for InfluxDB v3
type InfluxDatabase struct {
	client    *influxdb3.Client
	database  string
	connected bool
}

// NewInfluxDatabase creates a new InfluxDB v3 database instance
func NewInfluxDatabase() *InfluxDatabase {
	return &InfluxDatabase{}
}

// Connect establishes connection to LOCAL InfluxDB v3 Core
func (i *InfluxDatabase) Connect() error {
	// ✅ Get settings from environment
	url := getEnv("INFLUXDB_URL", "http://127.0.0.1:8086")
	token := getEnv("INFLUXDB_TOKEN", "") // Empty for local InfluxDB 3 Core
	i.database = getEnv("INFLUXDB_DATABASE", "solar_monitoring")

	// ✅ DEBUG: Show connection details
	fmt.Println("\n🔧 InfluxDB v3 Core Local Connection:")
	fmt.Printf("   URL: %s\n", url)
	fmt.Printf("   Database: %s\n", i.database)
	if token == "" {
		fmt.Println("   Token: (empty - no auth)")
	} else {
		fmt.Printf("   Token: %s...%s\n", token[:min(5, len(token))], token[max(0, len(token)-5):])
	}

	var err error
	
	// ✅ Create InfluxDB v3 client
	// For InfluxDB 3 Core with INFLUXDB_IOX_NO_AUTH=true, token can be empty
	i.client, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     url,
		Token:    token,
		Database: i.database,
	})

	if err != nil {
		return fmt.Errorf("failed to create InfluxDB v3 client: %w", err)
	}

	i.connected = true

	fmt.Println("✅ InfluxDB v3 Core client created successfully!")
	fmt.Printf("   URL: %s\n", url)
	fmt.Printf("   Database: %s\n", i.database)
	fmt.Println("   Mode: LOCAL DOCKER (No Auth)")

	return nil
}

// Close closes the InfluxDB v3 connection
func (i *InfluxDatabase) Close() {
	if i.client != nil {
		i.client.Close()
		i.connected = false
		fmt.Println("✅ InfluxDB v3 connection closed")
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

// GetClient returns the InfluxDB v3 client
func (i *InfluxDatabase) GetClient() *influxdb3.Client {
	return i.client
}

// GetDatabase returns the InfluxDB v3 database name
func (i *InfluxDatabase) GetDatabase() string {
	return i.database
}

// Helper function to get InfluxDB v3 client from activeDB
func GetInfluxClient() *influxdb3.Client {
	if activeDB == nil {
		return nil
	}
	if influxDB, ok := activeDB.(*InfluxDatabase); ok {
		return influxDB.GetClient()
	}
	return nil
}

// Helper function to get InfluxDB v3 database name from activeDB
func GetInfluxDatabase() string {
	if activeDB == nil {
		return ""
	}
	if influxDB, ok := activeDB.(*InfluxDatabase); ok {
		return influxDB.GetDatabase()
	}
	return ""
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}