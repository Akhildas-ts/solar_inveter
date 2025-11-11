#!/bin/bash

echo "🔍 InfluxDB 3 Core Diagnostic & Setup"
echo "======================================"
echo ""

# Step 1: Stop existing containers
echo "1️⃣ Stopping existing containers..."
docker-compose down -v
echo ""

# Step 2: Try pulling both images
echo "2️⃣ Pulling InfluxDB 3 Core images..."
echo "   Trying official image: influxdb:3-core"
docker pull influxdb:3-core 2>/dev/null
if [ $? -eq 0 ]; then
    echo "   ✅ Official image pulled successfully"
    IMAGE="influxdb:3-core"
else
    echo "   ❌ Official image failed, trying Quay.io..."
    docker pull quay.io/influxdb/influxdb3-core:latest 2>/dev/null
    if [ $? -eq 0 ]; then
        echo "   ✅ Quay.io image pulled successfully"
        IMAGE="quay.io/influxdb/influxdb3-core:latest"
    else
        echo "   ❌ Both images failed to pull!"
        exit 1
    fi
fi
echo ""

# Step 3: Test running InfluxDB 3 Core
echo "3️⃣ Testing InfluxDB 3 Core..."
echo "   Starting test container..."
docker run --rm -d \
    --name influxdb3_test \
    -p 8086:8086 \
    $IMAGE \
    serve \
    --http-bind-addr=0.0.0.0:8086 \
    --object-store=file \
    --data-dir=/var/lib/influxdb3

# Wait for startup
sleep 10

# Check if it's running
if docker ps | grep -q influxdb3_test; then
    echo "   ✅ Test container is running!"
    
    # Check health endpoint
    if curl -s http://127.0.0.1:8086/health > /dev/null 2>&1; then
        echo "   ✅ Health endpoint responding!"
        RESPONSE=$(curl -s http://127.0.0.1:8086/health)
        echo "   Response: $RESPONSE"
    else
        echo "   ❌ Health endpoint not responding"
    fi
    
    # Stop test container
    docker stop influxdb3_test > /dev/null 2>&1
else
    echo "   ❌ Test container crashed!"
    echo "   Logs:"
    docker logs influxdb3_test 2>&1 | tail -20
    docker rm influxdb3_test > /dev/null 2>&1
    exit 1
fi
echo ""

# Step 4: Create working compose.yaml
echo "4️⃣ Creating compose.yaml..."
cat > compose.yaml << 'INNEREOF'
services:
  influxdb3:
    image: IMAGE_PLACEHOLDER
    container_name: solar_influxdb3
    ports:
      - "8086:8086"
    volumes:
      - influxdb3-data:/var/lib/influxdb3
    command:
      - serve
      - --http-bind-addr=0.0.0.0:8086
      - --object-store=file
      - --data-dir=/var/lib/influxdb3
    restart: unless-stopped

  mongodb:
    image: mongo:6.0
    container_name: solar_mongodb
    ports:
      - "27017:27017"
    volumes:
      - mongodb-data:/data/db
    restart: unless-stopped

volumes:
  influxdb3-data:
  mongodb-data:
INNEREOF

# Replace placeholder with actual image
sed -i.bak "s|IMAGE_PLACEHOLDER|$IMAGE|g" compose.yaml
rm compose.yaml.bak 2>/dev/null

echo "   ✅ compose.yaml created with image: $IMAGE"
echo ""

# Step 5: Start final containers
echo "5️⃣ Starting containers..."
docker-compose up -d
echo ""

# Step 6: Wait and check
echo "6️⃣ Waiting for services (20 seconds)..."
sleep 20
echo ""

echo "7️⃣ Checking status..."
echo ""

# Check InfluxDB
if curl -s http://127.0.0.1:8086/health > /dev/null 2>&1; then
    echo "✅ InfluxDB 3 Core is running!"
    echo "   URL: http://127.0.0.1:8086"
    echo "   Health: $(curl -s http://127.0.0.1:8086/health)"
else
    echo "❌ InfluxDB 3 Core failed to start"
    echo "   Logs:"
    docker logs solar_influxdb3 | tail -20
fi
echo ""

# Check MongoDB
if docker exec solar_mongodb mongosh --quiet --eval "db.adminCommand('ping').ok" 2>/dev/null | grep -q 1; then
    echo "✅ MongoDB is running!"
else
    echo "❌ MongoDB failed to start"
fi
echo ""

echo "======================================"
echo "📊 Container Status"
echo "======================================"
docker ps
echo ""

if curl -s http://127.0.0.1:8086/health > /dev/null 2>&1; then
    echo "🎉 SUCCESS! InfluxDB 3 Core is ready!"
    echo ""
    echo "Next steps:"
    echo "  1. Run: go mod tidy"
    echo "  2. Run: go run main.go"
    echo "  3. Your SQL queries will work! 🚀"
else
    echo "❌ Setup failed. Showing detailed logs..."
    echo ""
    echo "InfluxDB Logs:"
    docker logs solar_influxdb3 2>&1 | tail -30
fi
echo ""