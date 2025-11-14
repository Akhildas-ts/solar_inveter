#!/bin/bash

echo "🧪 Testing Historical Timestamp Preservation"
echo "==========================================="
echo ""

# Generate timestamp from 1 month ago
HISTORICAL_DATE=$(date -u -v-1m +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "1 month ago" +"%Y-%m-%dT%H:%M:%SZ")

echo "📅 Historical timestamp: $HISTORICAL_DATE"
echo "📅 Current timestamp:    $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo ""

# Create test payload
cat > /tmp/test_payload.json << EOF
{
  "device_timestamp": "$HISTORICAL_DATE",
  "device_type": "current_format",
  "device_name": "TEST_DEVICE",
  "device_id": "TEST_001",
  "date": "$(date -u -v-1m +"%d/%m/%Y" 2>/dev/null || date -u -d "1 month ago" +"%d/%m/%Y")",
  "time": "$(date -u -v-1m +"%H:%M:%S" 2>/dev/null || date -u -d "1 month ago" +"%H:%M:%S")",
  "signal_strength": "-1",
  "data": {
    "serial_no": "TEST_SN_001",
    "s1v": 6200,
    "total_output_power": 147000,
    "f": 750,
    "today_e": 500,
    "total_e": 500000,
    "inv_temp": 650,
    "fault_code": 0
  }
}
EOF

echo "📤 Sending test payload..."
echo ""

RESPONSE=$(curl -s -X POST http://localhost:8080/api/data \
  -H "Content-Type: application/json" \
  -d @/tmp/test_payload.json)

echo "📥 Server response:"
echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

# Wait for data to flush
echo "⏳ Waiting 3 seconds for data to flush to InfluxDB..."
sleep 3
echo ""

# Query InfluxDB to verify timestamp
echo "🔍 Querying InfluxDB for the test record..."
docker exec solar_influxdb3 \
  influxdb3 query --database solar_monitoring \
  "SELECT time, device_name, device_id, voltage, total_output_power 
   FROM inverter_data 
   WHERE device_id = 'TEST_001' 
   ORDER BY time DESC 
   LIMIT 1"

echo ""
echo "📊 Expected timestamp: $HISTORICAL_DATE"
echo ""
echo "✅ If the time column matches the historical date, it worked!"
echo "❌ If the time column is recent, the timestamp was not preserved"
echo ""

# Cleanup
rm /tmp/test_payload.json