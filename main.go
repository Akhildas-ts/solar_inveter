package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"solar_project/config"
	"solar_project/routes"
	"solar_project/services"

	"github.com/gin-gonic/gin"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	// Initialize database connection
	if err := config.InitDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer config.CloseDB()

	fmt.Println("✓ Connected to MongoDB successfully!")

	// Initialize data generator service
	services.InitGenerator(config.GetCollection())

	// Start background services
	go services.Generate600RecordsPerSecond()
	go services.PeriodicBatchFlush()
	go services.ReportStats()

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Serve static files
	r.Static("/static", "./static")
	r.StaticFile("/", "./static/index.html")

	// Setup API routes
	routes.SetupRoutes(r)

	// Print startup information
	printStartupInfo()

	// Start server
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func printStartupInfo() {
	fmt.Println("\n🚀 Solar Monitoring System with Fault Detection")
	fmt.Println("================================================")
	fmt.Println("Server: http://localhost:8080")
	fmt.Println("\n📊 Basic APIs:")
	fmt.Println("   /api/all        - All data (latest 100)")
	fmt.Println("   /api/stats      - Insertion statistics")
	fmt.Println("\n⚠️  Fault Detection APIs:")
	fmt.Println("   /api/faults/list     - List all fault codes")
	fmt.Println("   /api/faults/data?code=3  - Get data by fault code")
	fmt.Println("   /api/faults/stats    - Fault statistics")
	fmt.Println("   /api/faults/active   - Active faults only")
	fmt.Println("   /api/faults/latest   - Latest 50 faults\n")
}
