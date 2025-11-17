// internal/api/routes.go (UPDATED)
package api

import (
	"solar_project/internal/service"
	"github.com/gin-gonic/gin"
)

// SetupRoutes configures all API routes including raw data management
func SetupRoutes(r *gin.Engine, svc *service.Service) {
	h := NewHandler(svc)
	rawHandler := NewRawDataHandler(svc)

	api := r.Group("/api")
	{
		// Core endpoints
		api.POST("/data", h.SubmitData)
		api.GET("/stats", h.GetStats)
		api.GET("/records", h.GetRecords)

		// Fault codes
		api.GET("/faults/codes", h.GetFaultCodes)

		// Mapping management
		mappings := api.Group("/mappings")
		{
			mappings.GET("", h.GetMappings)
			mappings.POST("", h.CreateMapping)
			mappings.PUT("/:id", h.UpdateMapping)
			mappings.DELETE("/:id", h.DeleteMapping)
		}

		// Raw data management 
		raw := api.Group("/raw")
		{
			raw.GET("/stats", rawHandler.GetRawStats)
			raw.POST("/reprocess/:id", rawHandler.ReprocessRawData)
		}

		// Cache management 
		cache := api.Group("/cache")
		{
			cache.GET("/stats", rawHandler.GetCacheStats)
			cache.POST("/clear", rawHandler.ClearCache)
		}
	}
}