#!/bin/bash

echo "🔍 InfluxDB v3 Core Verification Script"
echo "========================================"
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. Check if Docker container is running
echo "1️⃣  Checking Docker container..."
if docker ps | grep -q solar_influxdb3; then
    echo -e "${GREEN}✅ Container is running${NC}"
else
    echo -e "${RED}❌ Container is not running${NC}"
    echo "   Start with: docker-compose up -d"
    exit 1
fi
echo ""

# 2. Check container logs for errors
echo "2️⃣  Checking container logs (last 10 lines)..."
docker logs --tail 10 solar_influxdb3
echo ""

# 3. Test database connection
echo "3️⃣  Testing database connection..."
if docker exec solar_influxdb3 \
    influxdb3 query --database solar_monitoring "SELECT 1" > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Database connection successful${NC}"
else
    echo -e "${RED}❌ Database connection failed${NC}"
    exit 1
fi
echo ""

# 4. Show existing tables
echo "4️⃣  Listing existing tables..."
docker exec solar_influxdb3 \
    influxdb3 query --database solar_monitoring \
    "SHOW TABLES" | grep -E "inverter_data|table_name"
echo ""

# 5. Check if inverter_data table exists and has data
echo "5️⃣  Checking inverter_data table..."
COUNT=$(docker exec solar_influxdb3 \
    influxdb3 query --database solar_monitoring \
    "SELECT COUNT(*) FROM inverter_data" 2>/dev/null | grep -oP '\d+' | tail -1)

if [ -z "$COUNT" ]; then
    echo -e "${YELLOW}⚠️  Table does not exist or is empty${NC}"
    echo "   This is OK if you haven't sent data yet"
else
    echo -e "${GREEN}✅ Table exists with $COUNT records${NC}"
fi
echo ""

# 6. Test write with Go client
echo "6️⃣  Testing Go client write..."
cat > /tmp/test_influx.go << 'EOF'
package main

import (
	"context"
	"fmt"
	"time"
	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

func main() {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     "http://127.0.0.1:8086",
		Token:    "",
		Database: "solar_monitoring",
	})
	if err != nil {
		fmt.Printf("❌ Failed to create client: %v\n", err)
		return
	}
	defer client.Close()

	// Create test point
	point := influxdb3.NewPoint(
		"test_measurement",
		map[string]string{"test": "true"},
		map[string]interface{}{"value": 42},
		time.Now(),
	)

	// Write
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = client.WritePoints(ctx, []*influxdb3.Point{point})
	if err != nil {
		fmt.Printf("❌ Write failed: %v\n", err)
		return
	}

	fmt.Println("✅ Go client write successful")
}
EOF

cd /tmp && go run test_influx.go 2>&1
rm /tmp/test_influx.go
echo ""

# 7. Network connectivity check
echo "7️⃣  Checking network connectivity..."
if curl -s http://127.0.0.1:8086/health > /dev/null 2>&1; then
    echo -e "${GREEN}✅ HTTP endpoint reachable at 127.0.0.1:8086${NC}"
else
    echo -e "${RED}❌ HTTP endpoint not reachable${NC}"
fi
echo ""

# 8. Port binding check
echo "8️⃣  Checking port bindings..."
docker port solar_influxdb3 | grep 8086
echo ""

# Summary
echo "========================================"
echo "Summary:"
echo "  - If all checks pass, your InfluxDB is ready"
echo "  - Update .env: INFLUXDB_URL=http://127.0.0.1:8086"
echo "  - Run: go run main.go"
echo "  - Then: go run simulator.go"
echo ""
echo "To monitor data:"
echo "  docker exec -it solar_influxdb3 \\"
echo "    influxdb3 query --database solar_monitoring \\"
echo "    'SELECT * FROM inverter_data ORDER BY time DESC LIMIT 10'"