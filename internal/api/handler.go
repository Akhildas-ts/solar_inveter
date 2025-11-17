package api

import (
	"net/http"
	"strconv"
	"time"

	"solar_project/internal/domain"
	"solar_project/internal/service"
	"solar_project/pkg/logger"

	"github.com/gin-gonic/gin"
)

// Handler handles HTTP requests
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new handler
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// SubmitData handles POST /api/data
func (h *Handler) SubmitData(c *gin.Context) {
	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	if err := h.svc.ProcessData(data); err != nil {
		logger.Error("Process failed: " + err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Data processed successfully",
	})
}

// GetStats handles GET /api/stats
func (h *Handler) GetStats(c *gin.Context) {
	stats, err := h.svc.GetStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetRecords handles GET /api/records
func (h *Handler) GetRecords(c *gin.Context) {
	filter := domain.QueryFilter{
		Limit:  getIntParam(c, "limit", 100),
		Offset: getIntParam(c, "offset", 0),
	}

	// Optional filters
	if deviceID := c.Query("device_id"); deviceID != "" {
		filter.DeviceID = deviceID
	}

	if faultCodeStr := c.Query("fault_code"); faultCodeStr != "" {
		if faultCode, err := strconv.Atoi(faultCodeStr); err == nil {
			filter.FaultCode = &faultCode
		}
	}

	if startStr := c.Query("start_time"); startStr != "" {
		if start, err := time.Parse(time.RFC3339, startStr); err == nil {
			filter.StartTime = &start
		}
	}

	if endStr := c.Query("end_time"); endStr != "" {
		if end, err := time.Parse(time.RFC3339, endStr); err == nil {
			filter.EndTime = &end
		}
	}

	records, err := h.svc.Query(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":   len(records),
		"records": records,
		"filter":  filter,
	})
}

// GetFaultCodes handles GET /api/faults/codes
func (h *Handler) GetFaultCodes(c *gin.Context) {
	codes := make([]domain.FaultInfo, 0, len(domain.FaultCodes))
	for _, info := range domain.FaultCodes {
		codes = append(codes, info)
	}

	c.JSON(http.StatusOK, gin.H{
		"count": len(codes),
		"codes": codes,
	})
}

// Mapping handlers

// GetMappings handles GET /api/mappings
func (h *Handler) GetMappings(c *gin.Context) {
	mappings := h.svc.GetMappings()

	c.JSON(http.StatusOK, gin.H{
		"count":    len(mappings),
		"mappings": mappings,
	})
}

// CreateMapping handles POST /api/mappings
func (h *Handler) CreateMapping(c *gin.Context) {
	var mapping domain.DataSourceMapping
	if err := c.ShouldBindJSON(&mapping); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid mapping"})
		return
	}

	if err := h.svc.CreateMapping(&mapping); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "Mapping created",
		"mapping": mapping,
	})
}

// UpdateMapping handles PUT /api/mappings/:id
func (h *Handler) UpdateMapping(c *gin.Context) {
	sourceID := c.Param("id")

	var mapping domain.DataSourceMapping
	if err := c.ShouldBindJSON(&mapping); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid mapping"})
		return
	}

	mapping.SourceID = sourceID
	if err := h.svc.UpdateMapping(sourceID, &mapping); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Mapping updated",
		"mapping": mapping,
	})
}

// DeleteMapping handles DELETE /api/mappings/:id
func (h *Handler) DeleteMapping(c *gin.Context) {
	sourceID := c.Param("id")

	if err := h.svc.DeleteMapping(sourceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Mapping deleted",
	})
}

// Helper functions
func getIntParam(c *gin.Context, key string, defaultValue int) int {
	if value := c.Query(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil && intValue > 0 {
			return intValue
		}
	}
	return defaultValue
}
