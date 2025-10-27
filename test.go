// package main

// import (
// 	"context"
// 	"fmt"
// 	"os"
// 	"time"

// 	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
// 	"github.com/joho/godotenv"
// )

// func main() {
// 	// Load .env file
// 	if err := godotenv.Load(); err != nil {
// 		fmt.Println("Warning: .env file not found")
// 	}

// 	fmt.Println("🔍 InfluxDB Connection Test")
// 	fmt.Println("=" + repeatString("=", 50))
// 	fmt.Println()

// 	// Get config
// 	url := getEnv("INFLUXDB_URL", "https://us-east-1-1.aws.cloud2.influxdata.com")
// 	token := getEnv("INFLUXDB_TOKEN", "")
// 	bucket := getEnv("INFLUXDB_BUCKET", "solar_monitoring")

// 	if token == "" {
// 		fmt.Println("❌ INFLUXDB_TOKEN not found in environment!")
// 		fmt.Println("   Make sure your .env file has INFLUXDB_TOKEN set")
// 		os.Exit(1)
// 	}

// 	fmt.Printf("📋 Configuration:\n")
// 	fmt.Printf("   URL:    %s\n", url)
// 	fmt.Printf("   Bucket: %s\n", bucket)
// 	fmt.Printf("   Token:  %s...%s\n\n", token[:10], token[len(token)-10:])

// 	// Create client
// 	fmt.Println("🔌 Step 1: Creating InfluxDB client...")
// 	client, err := influxdb3.New(influxdb3.ClientConfig{
// 		Host:     url,
// 		Token:    token,
// 		Database: bucket,
// 	})

// 	if err != nil {
// 		fmt.Printf("❌ Failed to create client: %v\n", err)
// 		os.Exit(1)
// 	}
// 	defer client.Close()
// 	fmt.Println("✅ Client created successfully!")
// 	fmt.Println()

// 	// Test 1: Write a simple test point
// 	fmt.Println("📝 Step 2: Writing test data...")
// 	now := time.Now()

// 	testPoint := influxdb3.NewPointWithMeasurement("test_connection").
// 		SetTag("test", "simple").
// 		SetTag("source", "test_script").
// 		SetField("value", int64(999)).
// 		SetField("message", "Hello from test!").
// 		SetTimestamp(now)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	fmt.Printf("   Writing to bucket: %s\n", bucket)
// 	fmt.Printf("   Measurement: test_connection\n")
// 	fmt.Printf("   Timestamp: %s\n", now.Format(time.RFC3339))

// 	err = client.WritePoints(ctx, []*influxdb3.Point{testPoint}, influxdb3.WithDatabase(bucket))

// 	if err != nil {
// 		fmt.Printf("❌ WRITE FAILED!\n")
// 		fmt.Printf("   Error: %v\n\n", err)

// 		fmt.Println("🔍 Troubleshooting:")
// 		fmt.Println("   1. Check if bucket 'solar_monitoring' exists in InfluxDB UI")
// 		fmt.Println("   2. Go to: Load Data → Buckets")
// 		fmt.Println("   3. Verify your token has WRITE permission")
// 		fmt.Println("   4. Go to: Load Data → API Tokens")
// 		os.Exit(1)
// 	}

// 	fmt.Println("✅ Write successful!")
// 	fmt.Println()

// 	// Wait a bit for indexing
// 	fmt.Println("⏳ Step 3: Waiting 5 seconds for data to be indexed...")
// 	time.Sleep(5 * time.Second)
// 	fmt.Println()

// 	// Test 2: Try to read the data back
// 	fmt.Println("📖 Step 4: Reading data back...")

// 	queries := []string{
// 		"SHOW MEASUREMENTS",
// 		"SELECT * FROM test_connection LIMIT 1",
// 		fmt.Sprintf("SELECT * FROM test_connection WHERE time >= '%s'", now.Add(-1*time.Minute).Format(time.RFC3339)),
// 	}

// 	for i, query := range queries {
// 		fmt.Printf("\n   Query %d: %s\n", i+1, query)

// 		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
// 		iterator, err := client.Query(ctx2, query)
// 		cancel2()

// 		if err != nil {
// 			fmt.Printf("   ⚠️  Query failed: %v\n", err)
// 			continue
// 		}

// 		hasData := false
// 		rowCount := 0
// 		for iterator.Next() {
// 			hasData = true
// 			rowCount++
// 			value := iterator.Value()
// 			fmt.Printf("   ✅ Row %d: %v\n", rowCount, value)

// 			if rowCount >= 3 {
// 				fmt.Println("   ... (showing first 3 rows)")
// 				break
// 			}
// 		}

// 		if !hasData {
// 			fmt.Println("   ℹ️  No data returned (might take a few seconds to appear)")
// 		}
// 	}

// 	fmt.Println()
// 	fmt.Println("=" + repeatString("=", 50))
// 	fmt.Println("🎉 Test Complete!")
// 	fmt.Println()
// 	fmt.Println("📊 Next Steps:")
// 	fmt.Println("   1. Go to InfluxDB Data Explorer")
// 	fmt.Println("   2. Select bucket: solar_monitoring")
// 	fmt.Println("   3. Run query: SELECT * FROM test_connection")
// 	fmt.Println()
// 	fmt.Println("   If you see data, your connection works!")
// 	fmt.Println("   If not, check bucket name and token permissions.")
// 	fmt.Println()

// 	// Test 3: Write inverter-like data
// 	fmt.Println("📝 Step 5: Writing sample inverter data...")

// 	inverterPoint := influxdb3.NewPointWithMeasurement("inverter_data").
// 		SetTag("device_type", "inverter").
// 		SetTag("device_name", "TEST_DEVICE").
// 		SetTag("device_id", "test-123").
// 		SetTag("signal_strength", "-1").
// 		SetField("serial_no", "TEST001").
// 		SetField("voltage", int64(6200)).
// 		SetField("total_output_power", int64(147000)).
// 		SetField("temperature", int64(650)).
// 		SetField("fault_code", int64(0)).
// 		SetField("date", time.Now().Format("02/01/2006")).
// 		SetField("time", time.Now().Format("15:04:05")).
// 		SetTimestamp(time.Now())

// 	ctx3, cancel3 := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel3()

// 	err = client.WritePoints(ctx3, []*influxdb3.Point{inverterPoint}, influxdb3.WithDatabase(bucket))

// 	if err != nil {
// 		fmt.Printf("❌ Inverter data write FAILED: %v\n", err)
// 	} else {
// 		fmt.Println("✅ Sample inverter data written!")
// 		fmt.Println()
// 		fmt.Println("   Wait 10-30 seconds, then query:")
// 		fmt.Println("   SELECT * FROM inverter_data LIMIT 10")
// 	}
// }

// func getEnv(key, defaultValue string) string {
// 	value := os.Getenv(key)
// 	if value == "" {
// 		return defaultValue
// 	}
// 	return value
// }

// func repeatString(s string, count int) string {
// 	result := ""
// 	for i := 0; i < count; i++ {
// 		// result += s
// 	}
// 	return result
// }
