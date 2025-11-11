#!/bin/bash

echo "🚀 Setting up Solar Monitoring with LOCAL InfluxDB v3..."

# 1. Stop and remove old containers
echo "📦 Stopping old containers..."
docker-compose down -v

# 2. Start new containers
echo "🐳 Starting InfluxDB v3 and MongoDB..."
docker-compose up -d

# 3. Wait for services to be ready
echo "⏳ Waiting for services to start..."
sleep 10

# 4. Check InfluxDB v3 health
echo "🔍 Checking InfluxDB v3..."
curl -s http://localhost:8086/health

# 5. Install Go dependencies
echo "📥 Installing Go dependencies..."
go mod tidy

# 6. Run the application
echo "✨ Starting application..."
go run main.go