// Add these methods to internal/service/service.go

package service

import (
	"context"
	"fmt"
	"time"

	"solar_project/internal/domain"

	"go.mongodb.org/mongo-driver/bson"
)

// GetAllMappingsDetailed returns all mappings with full details
func (svc *Service) GetAllMappingsDetailed(ctx context.Context) ([]domain.DataSourceMapping, error) {
	if svc.mapper == nil || svc.mapper.collection == nil {
		return nil, fmt.Errorf("mapper not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := svc.mapper.collection.Find(ctx, bson.M{"active": true})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mappings: %w", err)
	}
	defer cursor.Close(ctx)

	var mappings []domain.DataSourceMapping
	if err := cursor.All(ctx, &mappings); err != nil {
		return nil, fmt.Errorf("failed to decode mappings: %w", err)
	}

	return mappings, nil
}

// GetMappingBySourceID returns a specific mapping
func (svc *Service) GetMappingBySourceID(ctx context.Context, sourceID string) (*domain.DataSourceMapping, error) {
	if svc.mapper == nil || svc.mapper.collection == nil {
		return nil, fmt.Errorf("mapper not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var mapping domain.DataSourceMapping
	err := svc.mapper.collection.FindOne(ctx, bson.M{
		"source_id": sourceID,
		"active":    true,
	}).Decode(&mapping)

	if err != nil {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	return &mapping, nil
}

// TestMapping tests a mapping against sample data without saving
func (svc *Service) TestMapping(mapping *domain.DataSourceMapping, sampleData map[string]interface{}) (map[string]interface{}, error) {
	if svc.mapper == nil {
		return nil, fmt.Errorf("mapper not initialized")
	}

	// Create a temporary compiled mapping for testing
	tempMapper := &UltraMapper{
		mappings:   make(map[string]*UltraMapping),
		collection: svc.mapper.collection,
	}

	// Compile the test mapping
	compiled := tempMapper.compileUltra(mapping)
	tempMapper.mappings[mapping.SourceID] = compiled

	// Test the mapping
	result, err := tempMapper.MapFields(mapping.SourceID, sampleData)
	if err != nil {
		return nil, fmt.Errorf("mapping test failed: %w", err)
	}

	return result, nil
}

// ValidateMappingExists checks if a mapping already exists
func (svc *Service) ValidateMappingExists(ctx context.Context, sourceID string) (bool, error) {
	if svc.mapper == nil || svc.mapper.collection == nil {
		return false, fmt.Errorf("mapper not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	count, err := svc.mapper.collection.CountDocuments(ctx, bson.M{
		"source_id": sourceID,
		"active":    true,
	})

	return count > 0, err
}

// GetMappingStats returns statistics about mappings
func (svc *Service) GetMappingStats(ctx context.Context) (map[string]interface{}, error) {
	if svc.mapper == nil || svc.mapper.collection == nil {
		return nil, fmt.Errorf("mapper not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	totalActive, _ := svc.mapper.collection.CountDocuments(ctx, bson.M{"active": true})
	totalInactive, _ := svc.mapper.collection.CountDocuments(ctx, bson.M{"active": false})

	// Get all active mappings to analyze
	cursor, err := svc.mapper.collection.Find(ctx, bson.M{"active": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mappings []domain.DataSourceMapping
	cursor.All(ctx, &mappings)

	fieldCount := 0
	requiredCount := 0
	transformCount := 0

	for _, mapping := range mappings {
		fieldCount += len(mapping.Mappings)
		for _, field := range mapping.Mappings {
			if field.Required {
				requiredCount++
			}
			if field.Transform != "" {
				transformCount++
			}
		}
	}

	return map[string]interface{}{
		"total_active":           totalActive,
		"total_inactive":         totalInactive,
		"total_fields":           fieldCount,
		"required_fields":        requiredCount,
		"transformed_fields":     transformCount,
		"avg_fields_per_mapping": float64(fieldCount) / float64(totalActive),
	}, nil
}
