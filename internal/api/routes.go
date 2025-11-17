package api

import (
	"solar_project/internal/service"
	"github.com/gin-gonic/gin"
)

// SetupRoutes configures all API routes
func SetupRoutes(r *gin.Engine, svc *service.Service) {
	h := NewHandler(svc)

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
	}
}