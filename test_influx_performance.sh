#!/bin/bash

echo "🚀 InfluxDB v3 Performance Test"
echo "================================"
echo ""

# Configuration
API_URL="http://localhost:8080/api/data"
TOTAL_REQUESTS=36000  # 600 req/s * 60 seconds
CONCURRENT=50         # Parallel connections
DURATION=60           # Test duration in seconds

# Sample payload
PAYLOAD='{
  "device_type": "current_format",
  "device_name": "Inverter_001",
  "device_id": "INV-2024-001",
  "date": "2024-11-12",
  "time": "15:30:45",
  "signal_strength": "95%",
  "data": {
    "serial_no": "SN123456789",
    "s1v": 240,
    "total_output_power": 5500,
    "f": 50,
    "today_e": 35000,
    "total_e": 1250000,
    "inv_temp": 45,
    "fault_code": 0
  }
}'

echo "📊 Test Parameters:"
echo "   Target: 600 requests/second"
echo "   Duration: $DURATION seconds"
echo "   Total: $TOTAL_REQUESTS requests"
echo "   Concurrent: $CONCURRENT connections"
echo ""

# Check if server is running
echo "🔍 Checking server health..."
if ! curl -s http://localhost:8080/api/stats > /dev/null; then
    echo "❌ Server not responding at http://localhost:8080"
    echo "   Start server with: go run main.go"
    exit 1
fi
echo "✅ Server is running"
echo ""

# Get initial stats
echo "📈 Initial Stats:"
INITIAL_STATS=$(curl -s http://localhost:8080/api/stats)
INITIAL_TOTAL=$(echo $INITIAL_STATS | jq -r '.total_records // 0')
echo "   Records in DB: $INITIAL_TOTAL"
echo ""

# Install Apache Bench if not available
if ! command -v ab &> /dev/null; then
    echo "⚠️  Apache Bench (ab) not found. Install with:"
    echo "   Ubuntu/Debian: sudo apt-get install apache2-utils"
    echo "   MacOS: brew install apache2"
    echo ""
    echo "Falling back to curl-based test..."
    echo ""
    
    # Simple curl-based test
    echo "🔥 Starting test..."
    START_TIME=$(date +%s)
    SUCCESS=0
    FAILED=0
    
    for i in $(seq 1 $TOTAL_REQUESTS); do
        if curl -s -X POST "$API_URL" \
            -H "Content-Type: application/json" \
            -d "$PAYLOAD" > /dev/null 2>&1; then
            SUCCESS=$((SUCCESS + 1))
        else
            FAILED=$((FAILED + 1))
        fi
        
        # Progress update every 1000 requests
        if [ $((i % 1000)) -eq 0 ]; then
            ELAPSED=$(($(date +%s) - START_TIME))
            RPS=$((i / ELAPSED))
            echo "   Progress: $i/$TOTAL_REQUESTS ($RPS req/s)"
        fi
    done
    
    END_TIME=$(date +%s)
    TOTAL_TIME=$((END_TIME - START_TIME))
    
else
    # Use Apache Bench
    echo "🔥 Starting Apache Bench test..."
    echo ""
    
    # Save payload to temp file
    TEMP_FILE=$(mktemp)
    echo "$PAYLOAD" > "$TEMP_FILE"
    
    # Run ab test
    ab -n $TOTAL_REQUESTS \
       -c $CONCURRENT \
       -p "$TEMP_FILE" \
       -T "application/json" \
       -g ab_results.tsv \
       "$API_URL" | tee ab_output.txt
    
    rm "$TEMP_FILE"
    
    # Parse results
    SUCCESS=$(grep "Complete requests:" ab_output.txt | awk '{print $3}')
    FAILED=$(grep "Failed requests:" ab_output.txt | awk '{print $3}')
    TOTAL_TIME=$(grep "Time taken for tests:" ab_output.txt | awk '{print $5}')
fi

echo ""
echo "================================"
echo "📊 Test Results"
echo "================================"
echo ""

# Get final stats
sleep 3  # Wait for buffer flush
FINAL_STATS=$(curl -s http://localhost:8080/api/stats)
FINAL_TOTAL=$(echo $FINAL_STATS | jq -r '.total_records // 0')
BUFFER_SIZE=$(echo $FINAL_STATS | jq -r '.buffer_size // 0')
SUCCESS_RATE=$(echo $FINAL_STATS | jq -r '.success_rate // 0')

RECORDS_ADDED=$((FINAL_TOTAL - INITIAL_TOTAL))

echo "⏱️  Duration: ${TOTAL_TIME}s"
echo "✅ Successful: $SUCCESS"
echo "❌ Failed: $FAILED"
echo ""
echo "📈 Database Records:"
echo "   Initial: $INITIAL_TOTAL"
echo "   Final: $FINAL_TOTAL"
echo "   Added: $RECORDS_ADDED"
echo ""
echo "🎯 Performance:"
REQUEST_RATE=$((SUCCESS / TOTAL_TIME))
WRITE_RATE=$((RECORDS_ADDED / TOTAL_TIME))
echo "   Request Rate: $REQUEST_RATE req/s"
echo "   Write Rate: $WRITE_RATE records/s"
echo "   Buffer Size: $BUFFER_SIZE"
echo "   Success Rate: $SUCCESS_RATE%"
echo ""

# Compare with target
if [ $WRITE_RATE -ge 600 ]; then
    echo "🎉 SUCCESS! Write rate ($WRITE_RATE/s) meets target (600/s)"
else
    echo "⚠️  Write rate ($WRITE_RATE/s) below target (600/s)"
    echo "   Difference: $((600 - WRITE_RATE))/s"
fi
echo ""

# Data loss check
EXPECTED=$SUCCESS
ACTUAL=$RECORDS_ADDED
LOSS=$((EXPECTED - ACTUAL))
LOSS_PERCENT=$(awk "BEGIN {printf \"%.2f\", ($LOSS / $EXPECTED) * 100}")

echo "📉 Data Integrity:"
echo "   Expected: $EXPECTED"
echo "   Written: $ACTUAL"
if [ $LOSS -eq 0 ]; then
    echo "   ✅ No data loss!"
else
    echo "   ⚠️  Lost: $LOSS records ($LOSS_PERCENT%)"
fi
echo ""

# Recommendations
if [ $WRITE_RATE -lt 600 ]; then
    echo "💡 Optimization Tips:"
    echo "   1. Increase batch size (currently 1000)"
    echo "   2. Add more workers (currently 4)"
    echo "   3. Increase write queue buffer"
    echo "   4. Check InfluxDB resource limits"
    echo ""
fi

echo "📁 Full stats available at: http://localhost:8080/api/stats"
