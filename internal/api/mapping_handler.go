// internal/api/mapping_handler.go
// ENHANCED Mapping Management API - Replace your existing mapping handlers

package api

import (
	"fmt"
	"net/http"

	"solar_project/internal/domain"
	"solar_project/internal/service"
	"solar_project/pkg/logger"

	"github.com/gin-gonic/gin"
)

type MappingHandler struct {
	svc *service.Service
}

func NewMappingHandler(svc *service.Service) *MappingHandler {
	return &MappingHandler{svc: svc}
}

// GetAllMappings - GET /api/mappings
// Returns all active mappings with detailed info
func (h *MappingHandler) GetAllMappings(c *gin.Context) {
	mappings, err := h.svc.GetAllMappingsDetailed(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch mappings",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"count":    len(mappings),
		"mappings": mappings,
	})
}

// GetMapping - GET /api/mappings/:source_id
// Get a specific mapping by source_id
func (h *MappingHandler) GetMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	mapping, err := h.svc.GetMappingBySourceID(c.Request.Context(), sourceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("Mapping not found: %s", sourceID),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"mapping": mapping,
	})
}

// CreateMapping - POST /api/mappings
// Create a new mapping dynamically
func (h *MappingHandler) CreateMapping(c *gin.Context) {
	var req CreateMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
			"example": getCreateMappingExample(),
		})
		return
	}

	// Validate request
	if err := validateCreateMappingRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Validation failed",
			"details": err.Error(),
		})
		return
	}

	// Convert to domain model
	mapping := convertToMappingDomain(&req)

	// Create mapping
	if err := h.svc.CreateMapping(&mapping); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create mapping",
			"details": err.Error(),
		})
		return
	}

	logger.Info(fmt.Sprintf("✅ Created mapping: %s", mapping.SourceID))

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Mapping created successfully",
		"mapping": mapping,
		"note":    "Mapping will be active immediately. Raw data remains unchanged.",
	})
}

// UpdateMapping - PUT /api/mappings/:source_id
// Update an existing mapping
func (h *MappingHandler) UpdateMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	var req CreateMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Validate
	if err := validateCreateMappingRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Validation failed",
			"details": err.Error(),
		})
		return
	}

	// Convert to domain model
	mapping := convertToMappingDomain(&req)
	mapping.SourceID = sourceID

	// Update mapping
	if err := h.svc.UpdateMapping(sourceID, &mapping); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to update mapping",
			"details": err.Error(),
		})
		return
	}

	logger.Info(fmt.Sprintf("✅ Updated mapping: %s", sourceID))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Mapping updated successfully",
		"mapping": mapping,
	})
}

// DeleteMapping - DELETE /api/mappings/:source_id
// Soft-delete a mapping (sets active=false)
func (h *MappingHandler) DeleteMapping(c *gin.Context) {
	sourceID := c.Param("source_id")

	if err := h.svc.DeleteMapping(sourceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to delete mapping",
			"details": err.Error(),
		})
		return
	}

	logger.Info(fmt.Sprintf("🗑️ Deleted mapping: %s", sourceID))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Mapping '%s' deactivated", sourceID),
	})
}

// TestMapping - POST /api/mappings/test
// Test a mapping against sample data without saving
func (h *MappingHandler) TestMapping(c *gin.Context) {
	var req TestMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Convert to mapping
	mapping := convertToMappingDomain(&req.Mapping)

	// Test against sample data
	result, err := h.svc.TestMapping(&mapping, req.SampleData)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Mapping test failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Mapping test successful",
		"input":   req.SampleData,
		"output":  result,
		"note":    "This is a test only. Data was not saved.",
	})
}

// Request/Response Models

type CreateMappingRequest struct {
	SourceID    string                `json:"source_id" binding:"required"`
	Description string                `json:"description"`
	NestedPath  string                `json:"nested_path"`
	Mappings    []FieldMappingRequest `json:"mappings" binding:"required,min=1"`
}

type FieldMappingRequest struct {
	SourceField   string      `json:"source_field" binding:"required"`
	StandardField string      `json:"standard_field" binding:"required"`
	DataType      string      `json:"data_type" binding:"required,oneof=string int float bool"`
	Transform     string      `json:"transform"`
	DefaultValue  interface{} `json:"default_value"`
	Required      bool        `json:"required"`
}

type TestMappingRequest struct {
	Mapping    CreateMappingRequest   `json:"mapping" binding:"required"`
	SampleData map[string]interface{} `json:"sample_data" binding:"required"`
}

// Validation
func validateCreateMappingRequest(req *CreateMappingRequest) error {
	if req.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}
	if len(req.Mappings) == 0 {
		return fmt.Errorf("at least one field mapping is required")
	}

	// Validate transforms
	validTransforms := map[string]bool{
		"multiply": true, "divide": true, "add": true, "subtract": true,
	}

	for i, field := range req.Mappings {
		if field.SourceField == "" {
			return fmt.Errorf("mapping[%d]: source_field is required", i)
		}
		if field.StandardField == "" {
			return fmt.Errorf("mapping[%d]: standard_field is required", i)
		}

		// Validate transform format
		if field.Transform != "" {
			// Parse transform (e.g., "divide:1000")
			var op string
			var val float64
			if n, _ := fmt.Sscanf(field.Transform, "%s:%f", &op, &val); n != 2 {
				return fmt.Errorf("mapping[%d]: invalid transform format (use 'operation:value', e.g., 'divide:1000')", i)
			}
			if !validTransforms[op] {
				return fmt.Errorf("mapping[%d]: invalid transform operation '%s' (use: multiply, divide, add, subtract)", i, op)
			}
		}
	}

	return nil
}

// Conversion
func convertToMappingDomain(req *CreateMappingRequest) domain.DataSourceMapping {
	mapping := domain.DataSourceMapping{
		SourceID:    req.SourceID,
		Description: req.Description,
		NestedPath:  req.NestedPath,
		Mappings:    make([]domain.FieldMapping, len(req.Mappings)),
		Active:      true,
	}

	for i, field := range req.Mappings {
		mapping.Mappings[i] = domain.FieldMapping{
			SourceField:   field.SourceField,
			StandardField: field.StandardField,
			DataType:      field.DataType,
			Transform:     field.Transform,
			DefaultValue:  field.DefaultValue,
			Required:      field.Required,
		}
	}

	return mapping
}

// Example response
func getCreateMappingExample() map[string]interface{} {
	return map[string]interface{}{
		"source_id":   "MyDevice",
		"description": "My custom device mapping",
		"nested_path": "data",
		"mappings": []map[string]interface{}{
			{
				"source_field":   "sno",
				"standard_field": "serial_no",
				"data_type":      "string",
				"required":       true,
			},
			{
				"source_field":   "p",
				"standard_field": "power",
				"data_type":      "int",
				"transform":      "divide:1000",
				"required":       false,
			},
		},
	}
}
