package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// FieldMapping represents a single field transformation rule
type FieldMapping struct {
	SourceField   string      `json:"source_field" bson:"source_field"`
	StandardField string      `json:"standard_field" bson:"standard_field"`
	DataType      string      `json:"data_type" bson:"data_type"`
	DefaultValue  interface{} `json:"default_value" bson:"default_value"`
	Transform     string      `json:"transform" bson:"transform"`
	Required      bool        `json:"required" bson:"required"`
}

// DataSourceMapping represents the complete mapping configuration for a data source
type DataSourceMapping struct {
	ID          primitive.ObjectID `json:"id,omitempty" bson:"_id,omitempty"`
	SourceID    string             `json:"source_id" bson:"source_id"`
	Description string             `json:"description" bson:"description"`
	NestedPath  string             `json:"nested_path" bson:"nested_path"`
	Mappings    []FieldMapping     `json:"mappings" bson:"mappings"`
	Active      bool               `json:"active" bson:"active"`
	Version     int                `json:"version" bson:"version"`
	CreatedAt   time.Time          `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at" bson:"updated_at"`
	CreatedBy   string             `json:"created_by,omitempty" bson:"created_by,omitempty"`
	UpdatedBy   string             `json:"updated_by,omitempty" bson:"updated_by,omitempty"`
}

// MappingHistory tracks changes to mappings over time
type MappingHistory struct {
	ID            primitive.ObjectID `json:"id,omitempty" bson:"_id,omitempty"`
	SourceID      string             `json:"source_id" bson:"source_id"`
	Action        string             `json:"action" bson:"action"` // CREATE, UPDATE, DELETE
	OldMapping    *DataSourceMapping `json:"old_mapping,omitempty" bson:"old_mapping,omitempty"`
	NewMapping    *DataSourceMapping `json:"new_mapping,omitempty" bson:"new_mapping,omitempty"`
	ChangedBy     string             `json:"changed_by" bson:"changed_by"`
	ChangedAt     time.Time          `json:"changed_at" bson:"changed_at"`
	ChangeReason  string             `json:"change_reason,omitempty" bson:"change_reason,omitempty"`
}

// MappingValidationError represents validation errors
type MappingValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// MappingStats provides statistics about mapping usage
type MappingStats struct {
	SourceID      string    `json:"source_id" bson:"source_id"`
	UsageCount    int64     `json:"usage_count" bson:"usage_count"`
	LastUsed      time.Time `json:"last_used" bson:"last_used"`
	SuccessCount  int64     `json:"success_count" bson:"success_count"`
	FailureCount  int64     `json:"failure_count" bson:"failure_count"`
	AvgProcessMs  float64   `json:"avg_process_ms" bson:"avg_process_ms"`
}