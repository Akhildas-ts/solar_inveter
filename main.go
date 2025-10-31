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
	"solar_project/mapper" // ✅ ADD THIS LINE
	"solar_project/routes"
	"solar_project/services"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// ✅ ADD THIS: Global mapper variable
var globalMapper *mapper.FlexibleMapper

func main() {
	// Load environment variables from .env file

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

	// ✅ ADD THIS: Initialize flexible mapper
	globalMapper = mapper.NewFlexibleMapper("./field_mappings.json", true)
	services.SetGlobalMapper(globalMapper) // ✅ PUT IT HERE
	logger.WriteLog("INFO", "", "MAPPER", "Flexible field mapper initialized")

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

	// ✅ ADD THIS: Setup mapping management routes
	setupMappingRoutes(r)

	// Setup API routes (your existing routes)
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

// ✅ ADD THIS ENTIRE FUNCTION: Mapping management API routes
func setupMappingRoutes(r *gin.Engine) {
	api := r.Group("/api/mappings")
	{
		api.GET("", getAllMappings)
		api.GET("/:source_id", getMapping)
		api.POST("", createMapping)
		api.PUT("/:source_id", updateMapping)
		api.DELETE("/:source_id", deleteMapping)
		api.POST("/reload", reloadMappings)
		api.POST("/test", testMapping)
	}
}

// ✅ ADD THESE HANDLER FUNCTIONS:

func getAllMappings(c *gin.Context) {
	mappings := globalMapper.GetAllMappings()
	c.JSON(http.StatusOK, gin.H{
		"count":    len(mappings),
		"mappings": mappings,
	})
}

func getMapping(c *gin.Context) {
	sourceID := c.Param("source_id")
	mappings := globalMapper.GetAllMappings()

	mapping, exists := mappings[sourceID]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "mapping not found"})
		return
	}

	c.JSON(http.StatusOK, mapping)
}

func createMapping(c *gin.Context) {
	var mapping mapper.DataSourceMapping
	if err := c.ShouldBindJSON(&mapping); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid request body",
			"details": err.Error(),
		})
		return
	}

	if mapping.SourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_id is required"})
		return
	}

	mapping.CreatedAt = time.Now()
	mapping.UpdatedAt = time.Now()

	if err := globalMapper.AddMapping(&mapping); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to add mapping",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "created",
		"mapping": mapping,
	})
}

func updateMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	var mapping mapper.DataSourceMapping
	if err := c.ShouldBindJSON(&mapping); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid request body",
			"details": err.Error(),
		})
		return
	}

	mapping.SourceID = sourceID

	if err := globalMapper.UpdateMapping(sourceID, &mapping); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "updated",
		"mapping": mapping,
	})
}

func deleteMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	if err := globalMapper.DeleteMapping(sourceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to delete mapping",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "deleted",
		"source_id": sourceID,
	})
}

func reloadMappings(c *gin.Context) {
	if err := globalMapper.LoadMappings(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to reload mappings",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "reloaded",
		"timestamp": time.Now(),
	})
}

func testMapping(c *gin.Context) {
	var request struct {
		SourceID string                 `json:"source_id"`
		RawData  map[string]interface{} `json:"raw_data"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if request.SourceID == "" {
		request.SourceID = globalMapper.DetectSourceID(request.RawData)
		if request.SourceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "could not detect source_id from data",
				"hint":  "provide source_id explicitly",
			})
			return
		}
	}

	standardized, err := globalMapper.MapFields(request.SourceID, request.RawData)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"detected_source":   request.SourceID,
		"original_data":     request.RawData,
		"standardized_data": standardized,
	})
}

// ✅ ADD THIS: Helper function to get mapper
func GetMapper() *mapper.FlexibleMapper {
	return globalMapper
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
	fmt.Println("\n🔄 Dynamic Mapping APIs:") // ✅ ADD THIS
	fmt.Println("   /api/mappings           - Get all mappings")
	fmt.Println("   /api/mappings/:id       - Get specific mapping")
	fmt.Println("   /api/mappings (POST)    - Create new mapping")
	fmt.Println("   /api/mappings/test      - Test a mapping")
	fmt.Println("\n💡 Press Ctrl+C to shutdown gracefully\n")
}
