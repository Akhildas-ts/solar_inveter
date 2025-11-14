#!/bin/bash

echo "🚀 InfluxDB v3 Core Setup (Free Version)"
echo "========================================"
echo ""

# Clean up old InfluxDB containers and images
echo "🧹 Cleaning up old InfluxDB installations..."
docker stop solar_influxdb3 2>/dev/null
docker rm solar_influxdb3 2>/dev/null
docker rmi influxdb:3-core 2>/dev/null
docker volume rm solar_influxdb3-data 2>/dev/null

echo ""
echo "📥 Pulling InfluxDB v3 Core (Free Edition)..."
docker pull influxdb:3-core

if [ $? -ne 0 ]; then
    echo "❌ Failed to pull influxdb:3-core"
    echo "Trying alternative: quay.io/influxdb/influxdb3-core:latest"
    docker pull quay.io/influxdb/influxdb3-core:latest
    IMAGE="quay.io/influxdb/influxdb3-core:latest"
else
    echo "✅ Successfully pulled influxdb:3-core"
    IMAGE="influxdb:3-core"
fi

echo ""
echo "📝 Creating docker-compose.yml (InfluxDB only)..."
cat > compose.yaml << EOF
services:
  influxdb3:
    image: ${IMAGE}
    container_name: solar_influxdb3
    ports:
      - "8086:8086"
    volumes:
      - influxdb3-data:/var/lib/influxdb3
    environment:
      - INFLUXD_HTTP_BIND_ADDRESS=:8086
    command:
      - serve
      - --http-bind
      - 0.0.0.0:8086
      - --object-store
      - file
      - --data-dir
      - /var/lib/influxdb3
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8086/health"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  influxdb3-data:
EOF

echo "✅ docker-compose.yml created"
echo ""

echo "🚀 Starting InfluxDB container..."
docker-compose up -d

echo ""
echo "⏳ Waiting for InfluxDB to be ready..."
sleep 15

# Check InfluxDB
echo ""
echo "🔍 Checking InfluxDB v3 Core..."
if curl -f -s http://localhost:8086/health > /dev/null; then
    HEALTH=$(curl -s http://localhost:8086/health)
    echo "✅ InfluxDB v3 Core is running!"
    echo "   Health: $HEALTH"
    echo "   URL: http://localhost:8086"
    echo "   Version: Free (Core)"
else
    echo "❌ InfluxDB v3 Core failed to start"
    echo "Showing logs:"
    docker logs solar_influxdb3 --tail 50
    exit 1
fi

# Check local MongoDB
echo ""
echo "🔍 Checking local MongoDB connection..."
if mongosh --quiet --eval "db.adminCommand('ping').ok" 2>/dev/null | grep -q 1; then
    echo "✅ Local MongoDB is accessible!"
else
    echo "⚠️  Cannot connect to local MongoDB"
    echo "   Make sure MongoDB is running locally"
    echo "   Run: sudo systemctl start mongodb"
    echo "   Or: brew services start mongodb-community"
fi

echo ""
echo "================================"
echo "✅ Setup Complete!"
echo "================================"
echo ""
echo "📊 Container Status:"
docker ps --filter "name=solar_influxdb3" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
echo ""
echo "🔗 Access Points:"
echo "   InfluxDB: http://localhost:8086"
echo "   MongoDB:  localhost:27017 (local)"
echo ""
echo "📝 Next Steps:"
echo "   1. Verify .env uses local MongoDB:"
echo "      MONGO_URI=mongodb://localhost:27017"
echo "   2. Run: go mod tidy"
echo "   3. Run: go run main.go"
echo ""
echo "💡 Useful Commands:"
echo "   View InfluxDB logs: docker logs solar_influxdb3 -f"
echo "   Stop InfluxDB:      docker-compose down"
echo "   Remove data:        docker-compose down -v"
echo "   Restart InfluxDB:   docker-compose restart