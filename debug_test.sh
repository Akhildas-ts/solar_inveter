#!/bin/bash

echo "🔍 Testing Historical Timestamp Flow"
echo "====================================="
echo ""

# Test 1: Send a single historical record
echo "📤 Test 1: Sending historical data (2 months ago)..."
HISTORICAL_DATE="2024-09-15T08:30:00Z"

RESPONSE=$(curl -s -X POST "http://localhost:8080/api/data" \
  -H "Content-Type: application/json" \
  -d "{
    \"device_timestamp\": \"${HISTORICAL_DATE}\",
    \"device_type\": \"current_format\",
    \"device_name\": \"TEST_DEVICE\",
    \"device_id\": \"TEST_001\",
    \"date\": \"15/09/2024\",
    \"time\": \"08:30:00\",
    \"signal_strength\": \"-1\",
    \"data\": {
      \"serial_no\": \"TEST123\",
      \"s1v\": 6200,
      \"total_output_power\": 147000,
      \"f\": 750,
      \"today_e\": 500,
      \"total_e\": 500000,
      \"inv_temp\": 650,
      \"fault_code\": 0
    }
  }")

echo "Server Response:"
echo "$RESPONSE" | jq '.'
echo ""

# Check if historical_ts flag is true
HISTORICAL_FLAG=$(echo "$RESPONSE" | jq -r '.historical_ts')
RECORD_TIMESTAMP=$(echo "$RESPONSE" | jq -r '.record_timestamp')

echo "Analysis:"
echo "  Historical flag: $HISTORICAL_FLAG"
echo "  Record timestamp: $RECORD_TIMESTAMP"
echo "  Expected: $HISTORICAL_DATE"
echo ""

if [ "$HISTORICAL_FLAG" = "true" ]; then
    echo "✅ Server recognized historical timestamp"
else
    echo "❌ Server did NOT recognize historical timestamp"
fi

# Wait for flush
echo ""
echo "⏳ Waiting 3 seconds for batch flush..."
sleep 3

# Test 2: Query InfluxDB for the record
echo ""
echo "📊 Test 2: Querying InfluxDB for test record..."
QUERY_RESULT=$(curl -s -X POST "http://127.0.0.1:8086/api/v3/query_sql" \
  -H "Content-Type: application/json" \
  -d "{
    \"db\": \"solar_monitoring\",
    \"q\": \"SELECT time, device_name, device_id, total_output_power FROM inverter_data WHERE device_id = 'TEST_001' ORDER BY time DESC LIMIT 1\"
  }")

echo "Query Result:"
echo "$QUERY_RESULT" | jq '.'
echo ""

# Parse the timestamp from result
STORED_TIME=$(echo "$QUERY_RESULT" | jq -r '.[0].time' 2>/dev/null)

if [ "$STORED_TIME" != "null" ] && [ -n "$STORED_TIME" ]; then
    echo "✅ Record found in database"
    echo "  Stored time: $STORED_TIME"
    
    # Compare dates (ignoring time zone differences)
    STORED_DATE=$(echo "$STORED_TIME" | cut -d'T' -f1)
    EXPECTED_DATE="2024-09-15"
    
    if [ "$STORED_DATE" = "$EXPECTED_DATE" ]; then
        echo "  ✅ DATE MATCHES! Historical timestamp preserved!"
    else
        echo "  ❌ DATE MISMATCH! Expected: $EXPECTED_DATE, Got: $STORED_DATE"
    fi
else
    echo "❌ Record not found in database"
fi

# Test 3: Check recent records
echo ""
echo "📊 Test 3: Checking last 5 records..."
RECENT_RECORDS=$(curl -s -X POST "http://127.0.0.1:8086/api/v3/query_sql" \
  -H "Content-Type: application/json" \
  -d "{
    \"db\": \"solar_monitoring\",
    \"q\": \"SELECT time, device_name, device_id FROM inverter_data ORDER BY time DESC LIMIT 5\"
  }")

echo "Recent timestamps:"
echo "$RECENT_RECORDS" | jq -r '.[] | \"  \" + .time + \" - \" + .device_name'
echo ""

# Test 4: Check date range
echo "📊 Test 4: Checking date range in database..."
DATE_RANGE=$(curl -s -X POST "http://127.0.0.1:8086/api/v3/query_sql" \
  -H "Content-Type: application/json" \
  -d "{
    \"db\": \"solar_monitoring\",
    \"q\": \"SELECT MIN(time) as earliest, MAX(time) as latest, COUNT(*) as total FROM inverter_data\"
  }")

echo "Database Statistics:"
echo "$DATE_RANGE" | jq '.'
echo ""

EARLIEST=$(echo "$DATE_RANGE" | jq -r '.[0].earliest')
LATEST=$(echo "$DATE_RANGE" | jq -r '.[0].latest')
TOTAL=$(echo "$DATE_RANGE" | jq -r '.[0].total')

echo "Summary:"
echo "  Total records: $TOTAL"
echo "  Earliest: $EARLIEST"
echo "  Latest: $LATEST"
echo ""

# Final verdict
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📋 VERDICT"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

EARLIEST_DATE=$(echo "$EARLIEST" | cut -d'T' -f1)
LATEST_DATE=$(echo "$LATEST" | cut -d'T' -f1)
CURRENT_DATE=$(date +%Y-%m-%d)

if [ "$EARLIEST_DATE" != "$CURRENT_DATE" ]; then
    echo "✅ SUCCESS! Historical timestamps are being stored!"
    echo "   Data spans from $EARLIEST_DATE to $LATEST_DATE"
else
    echo "❌ FAILURE! All timestamps are current date: $CURRENT_DATE"
    echo ""
    echo "🔧 Debugging steps:"
    echo "   1. Check server logs for 'Using historical timestamp' messages"
    echo "   2. Verify extractDeviceTimestamp() is being called"
    echo "   3. Verify convertToInfluxPoint() is using payload.DeviceTimestamp"
    echo "   4. Add debug prints to trace timestamp flow"
fi
echo ""