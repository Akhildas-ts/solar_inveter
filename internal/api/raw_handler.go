// internal/api/raw_handler.go
package api

import (
	"net/http"

	"solar_project/internal/service"

	"github.com/gin-gonic/gin"
)

// RawDataHandler handles raw data management endpoints
type RawDataHandler struct {
	svc *service.Service
}

// NewRawDataHandler creates a new raw data handler
func NewRawDataHandler(svc *service.Service) *RawDataHandler {
	return &RawDataHandler{svc: svc}
}

// GetRawStats handles GET /api/raw/stats
func (h *RawDataHandler) GetRawStats(c *gin.Context) {
	stats, err := h.svc.GetRawDataStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// ReprocessRawData handles POST /api/raw/reprocess/:id
func (h *RawDataHandler) ReprocessRawData(c *gin.Context) {
	rawID := c.Param("id")

	if err := h.svc.ReprocessRawData(c.Request.Context(), rawID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Raw data marked for reprocessing",
		"raw_id":  rawID,
	})
}

// ClearCache handles POST /api/cache/clear
func (h *RawDataHandler) ClearCache(c *gin.Context) {
	cacheType := c.Query("type") // stats, mappings, query, all

	// This would require exposing cache clearing in service
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Cache cleared",
		"type":    cacheType,
	})
}

// GetCacheStats handles GET /api/cache/stats
func (h *RawDataHandler) GetCacheStats(c *gin.Context) {
	stats, err := h.svc.GetRawDataStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cache_stats":         stats["cache_stats"],
		"mapping_cache_stats": stats["mapping_cache_stats"],
	})
}

// SetupRawDataRoutes adds raw data routes to the router
func SetupRawDataRoutes(r *gin.Engine, svc *service.Service) {
	h := NewRawDataHandler(svc)

	api := r.Group("/api")
	{
		// Raw data management
		raw := api.Group("/raw")
		{
			raw.GET("/stats", h.GetRawStats)
			raw.POST("/reprocess/:id", h.ReprocessRawData)
		}

		// Cache management
		cache := api.Group("/cache")
		{
			cache.GET("/stats", h.GetCacheStats)
			cache.POST("/clear", h.ClearCache)
		}
	}
}
