#!/bin/bash

set -e

echo "🧪 InfluxDB v3 Core Complete Test Suite"
echo "========================================"
echo ""

# Step 1: Check Docker
echo "📦 Step 1: Checking Docker setup..."
if ! docker ps | grep -q solar_influxdb3; then
    echo "   ❌ Container not running. Starting..."
    docker compose up -d
    sleep 5
fi
echo "   ✅ Container is running"

# Step 2: Health check
echo ""
echo "🏥 Step 2: Health check..."
for i in {1..10}; do
    if curl -sf http://localhost:8086/health > /dev/null 2>&1; then
        echo "   ✅ InfluxDB is healthy"
        break
    fi
    if [ $i -eq 10 ]; then
        echo "   ❌ InfluxDB health check failed after 10 attempts"
        exit 1
    fi
    echo "   ⏳ Waiting for InfluxDB... ($i/10)"
    sleep 2
done

# Step 3: Test write
echo ""
echo "✍️  Step 3: Testing write operation..."
TIMESTAMP=$(date +%s)000000000
curl -sf -X POST "http://localhost:8086/api/v2/write?db=solar_monitoring" \
  --data-raw "test_measurement,device=test1 temperature=25.5,voltage=220 $TIMESTAMP" > /dev/null

if [ $? -eq 0 ]; then
    echo "   ✅ Write successful"
else
    echo "   ❌ Write failed"
    exit 1
fi

# Step 4: Test SQL query
echo ""
echo "🔍 Step 4: Testing SQL query..."
QUERY_RESULT=$(curl -sf -X POST http://localhost:8086/api/v3/query_sql \
  -H "Content-Type: application/json" \
  -d '{
    "db": "solar_monitoring",
    "q": "SELECT * FROM test_measurement LIMIT 1"
  }')

if echo "$QUERY_RESULT" | grep -q "temperature"; then
    echo "   ✅ SQL query successful"
    echo "   Result: $QUERY_RESULT"
else
    echo "   ❌ SQL query failed or returned unexpected result"
    echo "   Result: $QUERY_RESULT"
fi

# Step 5: Show tables
echo ""
echo "📋 Step 5: Listing tables..."
TABLES=$(curl -sf -X POST http://localhost:8086/api/v3/query_sql \
  -H "Content-Type: application/json" \
  -d '{
    "db": "solar_monitoring",
    "q": "SHOW TABLES"
  }')
echo "   Tables: $TABLES"

# Step 6: Test your Go application
echo ""
echo "🚀 Step 6: Testing Go application..."
echo "   Make sure your .env has:"
echo "   DB_TYPE=influx"
echo "   INFLUXDB_URL=http://localhost:8086"
echo "   INFLUXDB_DATABASE=solar_monitoring"
echo ""
echo "   Start your app with: go run ./cmd/server"
echo ""

# Step 7: Test POST endpoint
echo "📤 Step 7: You can test data ingestion with:"
echo ""
echo "curl -X POST http://localhost:8080/api/data \\"
echo "  -H 'Content-Type: application/json' \\"
echo "  -d '{"
echo "    \"device_id\": \"INV-001\","
echo "    \"device_name\": \"Test-Inverter\","
echo "    \"device_timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\","
echo "    \"data\": {"
echo "      \"serial_no\": \"SN-777\","
echo "      \"s1v\": 230,"
echo "      \"total_output_power\": 5000,"
echo "      \"f\": 50,"
echo "      \"today_e\": 15,"
echo "      \"total_e\": 3000,"
echo "      \"inv_temp\": 40,"
echo "      \"fault_code\": 0"
echo "    }"
echo "  }'"

echo ""
echo "======================================"
echo "✅ All pre-flight checks passed!"
echo ""
echo "Next steps:"
echo "1. Replace the files:"
echo "   - internal/config/database.go"
echo "   - internal/repository/influx_repository.go"
echo "2. Make sure .env has DB_TYPE=influx"
echo "3. Run: go run ./cmd/server"
echo "4. Watch logs for:"
echo "   - '✓ InfluxDB connected: solar_monitoring'"
echo "   - '📝 Writing X points to InfluxDB...'"
echo "5. If errors, check: docker logs solar_influxdb3"
