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

	// Global mapping service variable
	var globalMappingService *services.MongoMappingService

	func main() {
		// Load environment variables
		if err := godotenv.Load(); err != nil {
			log.Println("Warning: .env file not found, using system environment variables")
		}

		rand.Seed(time.Now().UnixNano())
		logger.OpenLog()

		logger.WriteLog("INFO", "", "STARTUP", "Starting Solar Monitoring System...")

		// Initialize database connection
		if err := config.InitDB(); err != nil {
			log.Fatal("Failed to initialize database:", err)
		}
		defer config.CloseDB()

		//  Initialize MongoDB-backed mapping service (replaces JSON file)
		var err error
		globalMappingService, err = services.NewMongoMappingService(true) // true = auto-reload enabled
		if err != nil {
			log.Fatal("Failed to initialize mapping service:", err)
		}
		defer globalMappingService.Close()

		// Set the global mapper for services package
		services.SetGlobalMappingService(globalMappingService)
		logger.WriteLog("INFO", "", "MAPPER", "MongoDB-backed mapping service initialized")

		// Initialize data generator service
		services.InitGenerator()

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
		
		// ✅ NEW: Setup mapping management routes
		routes.SetupMappingRoutes(r, globalMappingService)

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

		// Graceful shutdown handling
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

		<-quit

		fmt.Println("\n🛑 Shutdown signal received. Gracefully shutting down...")
		logger.WriteLog("INFO", "", "SHUTDOWN", "Shutdown signal received")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}

		services.GracefulShutdown()

		fmt.Println("✓ Server exited gracefully")
		logger.WriteLog("INFO", "", "SHUTDOWN", "Server exited gracefully")
	}

	func printStartupInfo() {
		dbType := config.GetDBType()

		fmt.Printf("\n☀️  Solar Monitoring System with %s Database\n", dbType)
		fmt.Println("================================================================")
		fmt.Println("Server: http://localhost:8080")
		fmt.Println("\n📊 Basic APIs:")
		fmt.Println("   /api/all        - All data (paginated)")
		fmt.Println("   /api/stats      - Insertion statistics")
		fmt.Println("   /api/data       - POST endpoint for sending data")
		fmt.Println("\n⚠️  Fault Detection APIs:")
		fmt.Println("   /api/faults/list     - List all fault codes")
		fmt.Println("   /api/faults/data?code=3  - Get data by fault code")
		fmt.Println("   /api/faults/stats    - Fault statistics")
		fmt.Println("   /api/faults/active   - Active faults only")
		fmt.Println("   /api/faults/latest   - Latest 50 faults")
		
		// ✅ NEW: MongoDB Mapping APIs
		fmt.Println("\n🔄 MongoDB Dynamic Mapping APIs:")
		fmt.Println("   GET    /api/mappings              - Get all mappings")
		fmt.Println("   GET    /api/mappings/:source_id   - Get specific mapping")
		fmt.Println("   POST   /api/mappings              - Create new mapping")
		fmt.Println("   PUT    /api/mappings/:source_id   - Update mapping")
		fmt.Println("   DELETE /api/mappings/:source_id   - Delete mapping")
		fmt.Println("   POST   /api/mappings/reload       - Manually reload mappings")
		fmt.Println("   POST   /api/mappings/test         - Test mapping with data")
		fmt.Println("   GET    /api/mappings/detect       - Auto-detect source")
		fmt.Println("   GET    /api/mappings/:id/history  - View change history")
		fmt.Println("   GET    /api/mappings/:id/stats    - View usage statistics")
		
		fmt.Println("\n💡 Press Ctrl+C to shutdown gracefully")
		fmt.Println("\n✨ Features:")
		fmt.Println("   • Auto-reload mappings on change (MongoDB Change Streams)")
		fmt.Println("   • Change history tracking")
		fmt.Println("   • Usage statistics")
		fmt.Println("   • No restart required for mapping updates")
		fmt.Println()
	}