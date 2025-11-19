// internal/api/routes.go
// COMPLETE API ROUTES - Replace your existing routes.go

package api

import (
	"solar_project/internal/service"
	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine, svc *service.Service) {
	// Core handlers
	dataHandler := NewHandler(svc)
	rawHandler := NewRawDataHandler(svc)
	mappingHandler := NewMappingHandler(svc)

	api := r.Group("/api")
	{
		// ==================== DATA INGESTION ====================
		api.POST("/data", dataHandler.SubmitData)
		
		// ==================== STATISTICS ====================
		api.GET("/stats", dataHandler.GetStats)
		api.GET("/records", dataHandler.GetRecords)
		
		// ==================== FAULT MANAGEMENT ====================
		faults := api.Group("/faults")
		{
			faults.GET("/codes", dataHandler.GetFaultCodes)
		}
		
		// ==================== MAPPING MANAGEMENT (NEW!) ====================
		mappings := api.Group("/mappings")
		{
			// List all mappings
			mappings.GET("", mappingHandler.GetAllMappings)
			
			// Get specific mapping
			mappings.GET("/:source_id", mappingHandler.GetMapping)
			
			// Create new mapping
			mappings.POST("", mappingHandler.CreateMapping)
			
			// Update existing mapping
			mappings.PUT("/:source_id", mappingHandler.UpdateMapping)
			
			// Delete mapping (soft delete)
			mappings.DELETE("/:source_id", mappingHandler.DeleteMapping)
			
			// Test mapping without saving
			mappings.POST("/test", mappingHandler.TestMapping)
		}
		
		// ==================== RAW DATA MANAGEMENT ====================
		raw := api.Group("/raw")
		{
			raw.GET("/stats", rawHandler.GetRawStats)
			raw.POST("/reprocess/:id", rawHandler.ReprocessRawData)
		}
		
		// ==================== CACHE MANAGEMENT ====================
		cache := api.Group("/cache")
		{
			cache.GET("/stats", rawHandler.GetCacheStats)
			cache.POST("/clear", rawHandler.ClearCache)
		}
	}
}