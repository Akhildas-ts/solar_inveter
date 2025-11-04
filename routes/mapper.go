package routes

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"

	"solar_project/models"
	"solar_project/services"
)

// MappingRoutes handles mapping management endpoints
type MappingRoutes struct {
	service *services.MongoMappingService
}

// NewMappingRoutes creates a new mapping routes handler
func NewMappingRoutes(service *services.MongoMappingService) *MappingRoutes {
	return &MappingRoutes{service: service}
}

// SetupMappingRoutes configures all mapping-related routes
func SetupMappingRoutes(r *gin.Engine, service *services.MongoMappingService) {
	handler := NewMappingRoutes(service)

	api := r.Group("/api/mappings")
	{
		// Basic CRUD
		api.GET("", handler.GetAllMappings)
		api.GET("/:source_id", handler.GetMapping)
		api.POST("", handler.CreateMapping)
		api.PUT("/:source_id", handler.UpdateMapping)
		api.DELETE("/:source_id", handler.DeleteMapping)

		// Utility endpoints
		api.POST("/reload", handler.ReloadMappings)
		api.POST("/test", handler.TestMapping)
		api.GET("/detect", handler.DetectSourceID)
		
		// History and stats
		api.GET("/:source_id/history", handler.GetMappingHistory)
		api.GET("/:source_id/stats", handler.GetMappingStats)
		api.GET("/stats/summary", handler.GetAllStats)
	}
}

// GetAllMappings returns all active mappings
func (h *MappingRoutes) GetAllMappings(c *gin.Context) {
	mappings := h.service.GetAllMappings()

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"count":   len(mappings),
		"mappings": mappings,
	})
}

// GetMapping returns a specific mapping
func (h *MappingRoutes) GetMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	mapping, err := h.service.GetMapping(sourceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "mapping not found",
			"source_id": sourceID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"mapping": mapping,
	})
}

// CreateMapping creates a new mapping
func (h *MappingRoutes) CreateMapping(c *gin.Context) {
	var mapping models.DataSourceMapping
	if err := c.ShouldBindJSON(&mapping); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Get user from context (if auth is implemented)
	createdBy := c.GetString("user")
	if createdBy == "" {
		createdBy = "system"
	}

	if err := h.service.CreateMapping(&mapping, createdBy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "failed to create mapping",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "mapping created successfully",
		"mapping": mapping,
	})
}

// UpdateMapping updates an existing mapping
func (h *MappingRoutes) UpdateMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	var request struct {
		Mapping models.DataSourceMapping `json:"mapping"`
		Reason  string                   `json:"reason"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Ensure source_id matches
	request.Mapping.SourceID = sourceID

	updatedBy := c.GetString("user")
	if updatedBy == "" {
		updatedBy = "system"
	}

	if err := h.service.UpdateMapping(sourceID, &request.Mapping, updatedBy, request.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "failed to update mapping",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "mapping updated successfully",
		"mapping": request.Mapping,
	})
}

// DeleteMapping soft-deletes a mapping
func (h *MappingRoutes) DeleteMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	var request struct {
		Reason string `json:"reason"`
	}
	c.ShouldBindJSON(&request)

	deletedBy := c.GetString("user")
	if deletedBy == "" {
		deletedBy = "system"
	}

	if err := h.service.DeleteMapping(sourceID, deletedBy, request.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "failed to delete mapping",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"message":   "mapping deleted successfully",
		"source_id": sourceID,
	})
}

// ReloadMappings manually reloads all mappings from database
func (h *MappingRoutes) ReloadMappings(c *gin.Context) {
	if err := h.service.LoadMappingsFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "failed to reload mappings",
			"details": err.Error(),
		})
		return
	}

	mappings := h.service.GetAllMappings()

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"message":   "mappings reloaded successfully",
		"count":     len(mappings),
		"timestamp": time.Now(),
	})
}

// TestMapping tests a mapping with sample data
func (h *MappingRoutes) TestMapping(c *gin.Context) {
	var request struct {
		SourceID string                 `json:"source_id"`
		RawData  map[string]interface{} `json:"raw_data"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request body",
		})
		return
	}

	// Auto-detect if not provided
	if request.SourceID == "" {
		request.SourceID = h.service.DetectSourceID(request.RawData)
		if request.SourceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "could not detect source_id from data",
				"hint":   "provide source_id explicitly",
			})
			return
		}
	}

	// Get mapping
	mapping, err := h.service.GetMapping(request.SourceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Apply mapping
	standardized, err := h.service.MapFields(request.SourceID, request.RawData)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "mapping failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "success",
		"detected_source":   request.SourceID,
		"mapping_version":   mapping.Version,
		"original_data":     request.RawData,
		"standardized_data": standardized,
		"fields_mapped":     len(standardized),
	})
}

// DetectSourceID detects source from raw data
func (h *MappingRoutes) DetectSourceID(c *gin.Context) {
	var rawData map[string]interface{}
	if err := c.ShouldBindJSON(&rawData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request body",
		})
		return
	}

	sourceID := h.service.DetectSourceID(rawData)
	if sourceID == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "could not detect data source",
			"hint":   "no matching mapping found",
		})
		return
	}

	mapping, _ := h.service.GetMapping(sourceID)

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"source_id": sourceID,
		"mapping":   mapping,
	})
}

// GetMappingHistory returns change history for a mapping
func (h *MappingRoutes) GetMappingHistory(c *gin.Context) {
	sourceID := c.Param("source_id")


	// This would need the historyCol from the service
	// For now, return placeholder
	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"source_id": sourceID,
		"history":   []interface{}{},
		"message":   "History tracking enabled - check mapping_history collection",
	})
}

// GetMappingStats returns usage statistics for a mapping
func (h *MappingRoutes) GetMappingStats(c *gin.Context) {
	sourceID := c.Param("source_id")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query stats collection
	var stats models.MappingStats
	filter := bson.M{"source_id": sourceID}
	
	// This would need the statsCol from the service
	// For now, return placeholder
	_ = ctx
	_ = filter

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"source_id": sourceID,
		"stats":     stats,
	})
}

// GetAllStats returns summary statistics for all mappings
func (h *MappingRoutes) GetAllStats(c *gin.Context) {
	mappings := h.service.GetAllMappings()

	c.JSON(http.StatusOK, gin.H{
		"status":         "success",
		"total_mappings": len(mappings),
		"active_mappings": len(mappings),
		"message":        "Check mapping_stats collection for detailed usage stats",
	})
}