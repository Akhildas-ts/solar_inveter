package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	fmt.Println("🧪 Testing Historical Timestamp Preservation")
	fmt.Println("==========================================")
	fmt.Println()

	// Create a timestamp from 2 months ago
	historicalTime := time.Now().AddDate(0, -2, 0)
	historicalTime = time.Date(2024, 9, 15, 8, 30, 0, 0, time.UTC)

	fmt.Printf("📅 Historical timestamp: %s\n", historicalTime.Format(time.RFC3339))
	fmt.Printf("📅 Current time: %s\n", time.Now().Format(time.RFC3339))
	fmt.Println()

	// Create test payload
	payload := map[string]interface{}{
		"device_timestamp": historicalTime.Format(time.RFC3339),
		"device_type":      "current_format",
		"device_name":      "TEST_DEVICE_001",
		"device_id":        "TEST_ID_001",
		"date":             historicalTime.Format("02/01/2006"),
		"time":             historicalTime.Format("15:04:05"),
		"signal_strength":  "-1",
		"data": map[string]interface{}{
			"serial_no":          "TEST_SERIAL_123",
			"s1v":                6200,
			"total_output_power": 147000,
			"f":                  750,
			"today_e":            500,
			"total_e":            500000,
			"inv_temp":           650,
			"fault_code":         0,
		},
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Printf("❌ Error marshaling JSON: %v\n", err)
		return
	}

	fmt.Println("📤 Sending payload:")
	fmt.Println(string(jsonData))
	fmt.Println()

	// Send to server
	resp, err := http.Post(
		"http://localhost:8080/api/data",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		fmt.Printf("❌ Error sending request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Error reading response: %v\n", err)
		return
	}

	fmt.Printf("📥 Server response (Status: %d):\n", resp.StatusCode)

	// Pretty print JSON response
	var responseMap map[string]interface{}
	if err := json.Unmarshal(body, &responseMap); err == nil {
		prettyResponse, _ := json.MarshalIndent(responseMap, "", "  ")
		fmt.Println(string(prettyResponse))
	} else {
		fmt.Println(string(body))
	}
	fmt.Println()

	// Check response
	if resp.StatusCode == http.StatusOK {
		fmt.Println("✅ Request successful!")

		// Check if historical timestamp was recognized
		if historicalTS, ok := responseMap["historical_ts"].(bool); ok && historicalTS {
			fmt.Println("✅ Server recognized historical timestamp!")

			if recordTS, ok := responseMap["record_timestamp"].(string); ok {
				fmt.Printf("   Stored as: %s\n", recordTS)

				// Parse and compare
				storedTime, err := time.Parse(time.RFC3339, recordTS)
				if err == nil {
					if storedTime.Format("2006-01-02") == historicalTime.Format("2006-01-02") {
						fmt.Println("✅ DATE MATCHES! Historical timestamp preserved!")
					} else {
						fmt.Printf("❌ DATE MISMATCH!\n")
						fmt.Printf("   Expected: %s\n", historicalTime.Format("2006-01-02"))
						fmt.Printf("   Got: %s\n", storedTime.Format("2006-01-02"))
					}
				}
			}
		} else {
			fmt.Println("⚠️  Server did not recognize historical timestamp")
			fmt.Println("   This means extractDeviceTimestamp() might not be working")
		}
	} else {
		fmt.Printf("❌ Request failed with status %d\n", resp.StatusCode)
	}

	fmt.Println()
	fmt.Println("⏳ Waiting 3 seconds for data to flush to InfluxDB...")
	time.Sleep(3 * time.Second)

	fmt.Println()
	fmt.Println("💡 Next steps:")
	fmt.Println("   1. Run: ./debug_test.sh")
	fmt.Println("   2. Check InfluxDB query interface: http://localhost:8080/static/sql_querry.html")
	fmt.Println("   3. Query: SELECT time, device_name FROM inverter_data WHERE device_id = 'TEST_ID_001'")
	fmt.Println()
}
