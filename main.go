package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"solar_project/config"
	"solar_project/logger"
	"solar_project/routes"
	"solar_project/services"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using system environment variables")
	}

	rand.Seed(time.Now().UnixNano())
	logger.OpenLog()

	logger.WriteLog("INFO", "", "STARTUP", "Starting Solar Monitoring System with InfluxDB...")

	// Initialize InfluxDB connection
	if err := config.InitDB(); err != nil {
		log.Fatal("Failed to initialize InfluxDB:", err)
	}
	defer config.CloseDB()

	fmt.Println("✓ Connected to InfluxDB successfully!")

	// Initialize data generator service
	services.InitGenerator(config.GetClient())

	// Start background services
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

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// Start server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// ============ GRACEFUL SHUTDOWN HANDLING ============
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit

	fmt.Println("\n🛑 Shutdown signal received. Gracefully shutting down...")
	logger.WriteLog("INFO", "", "SHUTDOWN", "Shutdown signal received")

	// Gracefully shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Flush remaining data to InfluxDB
	services.GracefulShutdown()

	fmt.Println("✓ Server exited gracefully")
	logger.WriteLog("INFO", "", "SHUTDOWN", "Server exited gracefully")
}

func printStartupInfo() {
	fmt.Println("\n☀️  Solar Monitoring System with Fault Detection (InfluxDB)")
	fmt.Println("==========================================================")
	fmt.Println("Server: http://localhost:8080")
	fmt.Println("\n📊 Basic APIs:")
	fmt.Println("   /api/all        - All data (latest 100)")
	fmt.Println("   /api/stats      - Insertion statistics")
	fmt.Println("\n⚠️  Fault Detection APIs:")
	fmt.Println("   /api/faults/list     - List all fault codes")
	fmt.Println("   /api/faults/data?code=3  - Get data by fault code")
	fmt.Println("   /api/faults/stats    - Fault statistics")
	fmt.Println("   /api/faults/active   - Active faults only")
	fmt.Println("   /api/faults/latest   - Latest 50 faults")
	fmt.Println("\n💡 Press Ctrl+C to shutdown gracefully\n")
}
