package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"os"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"solar_project/config"
	"solar_project/logger"
	"solar_project/models"
)

// MongoMappingService handles all mapping operations with MongoDB backend
type MongoMappingService struct {
	mu              sync.RWMutex
	mappingCache    map[string]*models.DataSourceMapping
	collection      *mongo.Collection
	historyCol      *mongo.Collection
	statsCol        *mongo.Collection
	ctx             context.Context
	watchCtx        context.Context
	watchCancel     context.CancelFunc
	autoReload      bool
	lastReload      time.Time
}

// NewMongoMappingService creates a new MongoDB-backed mapping service
func NewMongoMappingService(autoReload bool) (*MongoMappingService, error) {
	client := config.GetMongoClient()
	if client == nil {
		return nil, fmt.Errorf("MongoDB client not initialized")
	}

	dbName := getEnv("DB_NAME", "solar_monitoring")
	
	ctx := context.Background()
	watchCtx, watchCancel := context.WithCancel(ctx)

	service := &MongoMappingService{
		mappingCache:    make(map[string]*models.DataSourceMapping),
		collection:      client.Database(dbName).Collection("mappings"),
		historyCol:      client.Database(dbName).Collection("mapping_history"),
		statsCol:        client.Database(dbName).Collection("mapping_stats"),
		ctx:             ctx,
		watchCtx:        watchCtx,
		watchCancel:     watchCancel,
		autoReload:      autoReload,
		lastReload:      time.Now(),
	}

	// Create indexes
	if err := service.createIndexes(); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	// Initial load from database
	if err := service.LoadMappingsFromDB(); err != nil {
		return nil, fmt.Errorf("failed to load initial mappings: %w", err)
	}

	// Start change stream watcher if auto-reload enabled
	if autoReload {
		go service.watchChanges()
	}

	logger.WriteLog("INFO", "", "MAPPING_SERVICE", 
		fmt.Sprintf("MongoDB Mapping Service initialized with %d mappings", len(service.mappingCache)))

	return service, nil
}

// createIndexes creates necessary indexes for optimal performance
func (s *MongoMappingService) createIndexes() error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// Index on source_id (unique)
	_, err := s.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "source_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// Index on active status
	_, err = s.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "active", Value: 1}},
	})
	if err != nil {
		return err
	}

	// Index on updated_at for sorting
	_, err = s.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "updated_at", Value: -1}},
	})
	if err != nil {
		return err
	}

	// Index for history collection
	_, err = s.historyCol.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "source_id", Value: 1},
			{Key: "changed_at", Value: -1},
		},
	})

	return err
}

// LoadMappingsFromDB loads all active mappings from MongoDB into cache
func (s *MongoMappingService) LoadMappingsFromDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// Query only active mappings
	filter := bson.M{"active": true}
	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to query mappings: %w", err)
	}
	defer cursor.Close(ctx)

	newCache := make(map[string]*models.DataSourceMapping)
	count := 0

	for cursor.Next(ctx) {
		var mapping models.DataSourceMapping
		if err := cursor.Decode(&mapping); err != nil {
			logger.WriteLog("ERROR", "", "MAPPING_LOAD", 
				fmt.Sprintf("Failed to decode mapping: %v", err))
			continue
		}
		newCache[mapping.SourceID] = &mapping
		count++
	}

	if err := cursor.Err(); err != nil {
		return fmt.Errorf("cursor error: %w", err)
	}

	s.mappingCache = newCache
	s.lastReload = time.Now()

	logger.WriteLog("INFO", "", "MAPPING_LOAD", 
		fmt.Sprintf("Loaded %d active mappings from MongoDB", count))

	return nil
}

// watchChanges monitors MongoDB for changes and auto-reloads
func (s *MongoMappingService) watchChanges() {
	logger.WriteLog("INFO", "", "MAPPING_WATCH", "Starting change stream watcher")

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "operationType", Value: bson.D{
				{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}},
			}},
		}}},
	}

	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	stream, err := s.collection.Watch(s.watchCtx, pipeline, opts)
	if err != nil {
		logger.WriteLog("ERROR", "", "MAPPING_WATCH", 
			fmt.Sprintf("Failed to create change stream: %v", err))
		return
	}
	defer stream.Close(s.watchCtx)

	for stream.Next(s.watchCtx) {
		var changeDoc struct {
			OperationType string                     `bson:"operationType"`
			FullDocument  *models.DataSourceMapping  `bson:"fullDocument"`
			DocumentKey   struct {
				ID primitive.ObjectID `bson:"_id"`
			} `bson:"documentKey"`
		}

		if err := stream.Decode(&changeDoc); err != nil {
			logger.WriteLog("ERROR", "", "MAPPING_WATCH", 
				fmt.Sprintf("Failed to decode change: %v", err))
			continue
		}

		logger.WriteLog("INFO", "", "MAPPING_WATCH", 
			fmt.Sprintf("Detected change: %s", changeDoc.OperationType))

		// Reload all mappings on any change
		if err := s.LoadMappingsFromDB(); err != nil {
			logger.WriteLog("ERROR", "", "MAPPING_WATCH", 
				fmt.Sprintf("Failed to reload mappings: %v", err))
		}
	}

	if err := stream.Err(); err != nil {
		logger.WriteLog("ERROR", "", "MAPPING_WATCH", 
			fmt.Sprintf("Change stream error: %v", err))
	}
}

// GetMapping retrieves a mapping from cache (fast) or DB (fallback)
func (s *MongoMappingService) GetMapping(sourceID string) (*models.DataSourceMapping, error) {
	// Try cache first
	s.mu.RLock()
	if mapping, exists := s.mappingCache[sourceID]; exists {
		s.mu.RUnlock()
		return mapping, nil
	}
	s.mu.RUnlock()

	// Cache miss - try database
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	var mapping models.DataSourceMapping
	filter := bson.M{"source_id": sourceID, "active": true}
	err := s.collection.FindOne(ctx, filter).Decode(&mapping)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("mapping not found for source: %s", sourceID)
		}
		return nil, err
	}

	// Update cache
	s.mu.Lock()
	s.mappingCache[sourceID] = &mapping
	s.mu.Unlock()

	return &mapping, nil
}

// CreateMapping creates a new mapping in MongoDB
func (s *MongoMappingService) CreateMapping(mapping *models.DataSourceMapping, createdBy string) error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// Validate mapping
	if err := s.validateMapping(mapping); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Set metadata
	mapping.ID = primitive.NewObjectID()
	mapping.CreatedAt = time.Now()
	mapping.UpdatedAt = time.Now()
	mapping.CreatedBy = createdBy
	mapping.Active = true
	mapping.Version = 1

	// Insert into database
	_, err := s.collection.InsertOne(ctx, mapping)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("mapping already exists for source_id: %s", mapping.SourceID)
		}
		return fmt.Errorf("failed to insert mapping: %w", err)
	}

	// Add to history
	s.recordHistory(mapping.SourceID, "CREATE", nil, mapping, createdBy, "Initial creation")

	// Update cache
	s.mu.Lock()
	s.mappingCache[mapping.SourceID] = mapping
	s.mu.Unlock()

	logger.WriteLog("INFO", "", "MAPPING_CREATE", 
		fmt.Sprintf("Created mapping for source: %s", mapping.SourceID))

	return nil
}

// UpdateMapping updates an existing mapping
func (s *MongoMappingService) UpdateMapping(sourceID string, mapping *models.DataSourceMapping, updatedBy string, reason string) error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// Get old mapping for history
	oldMapping, err := s.GetMapping(sourceID)
	if err != nil {
		return fmt.Errorf("mapping not found: %w", err)
	}

	// Validate new mapping
	if err := s.validateMapping(mapping); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Update metadata
	mapping.UpdatedAt = time.Now()
	mapping.UpdatedBy = updatedBy
	mapping.Version = oldMapping.Version + 1
	mapping.CreatedAt = oldMapping.CreatedAt
	mapping.CreatedBy = oldMapping.CreatedBy

	// Update in database
	filter := bson.M{"source_id": sourceID}
	update := bson.M{"$set": mapping}
	
	result, err := s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update mapping: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("mapping not found for source: %s", sourceID)
	}

	// Add to history
	s.recordHistory(sourceID, "UPDATE", oldMapping, mapping, updatedBy, reason)

	// Update cache
	s.mu.Lock()
	s.mappingCache[sourceID] = mapping
	s.mu.Unlock()

	logger.WriteLog("INFO", "", "MAPPING_UPDATE", 
		fmt.Sprintf("Updated mapping for source: %s (v%d)", sourceID, mapping.Version))

	return nil
}

// DeleteMapping soft-deletes a mapping (sets active=false)
func (s *MongoMappingService) DeleteMapping(sourceID string, deletedBy string, reason string) error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// Get mapping for history
	oldMapping, err := s.GetMapping(sourceID)
	if err != nil {
		return fmt.Errorf("mapping not found: %w", err)
	}

	// Soft delete
	filter := bson.M{"source_id": sourceID}
	update := bson.M{
		"$set": bson.M{
			"active":     false,
			"updated_at": time.Now(),
			"updated_by": deletedBy,
		},
	}

	result, err := s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to delete mapping: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("mapping not found for source: %s", sourceID)
	}

	// Add to history
	s.recordHistory(sourceID, "DELETE", oldMapping, nil, deletedBy, reason)

	// Remove from cache
	s.mu.Lock()
	delete(s.mappingCache, sourceID)
	s.mu.Unlock()

	logger.WriteLog("INFO", "", "MAPPING_DELETE", 
		fmt.Sprintf("Deleted mapping for source: %s", sourceID))

	return nil
}

// GetAllMappings returns all active mappings from cache
func (s *MongoMappingService) GetAllMappings() map[string]*models.DataSourceMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*models.DataSourceMapping)
	for k, v := range s.mappingCache {
		result[k] = v
	}
	return result
}

// DetectSourceID attempts to identify data source from raw JSON
func (s *MongoMappingService) DetectSourceID(rawData map[string]interface{}) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range s.mappingCache {
		score := 0
		
		dataToCheck := []map[string]interface{}{rawData}
		if mapping.NestedPath != "" {
			if nested, ok := rawData[mapping.NestedPath].(map[string]interface{}); ok {
				dataToCheck = append(dataToCheck, nested)
			}
		}

		for _, fieldMap := range mapping.Mappings {
			for _, data := range dataToCheck {
				if _, exists := data[fieldMap.SourceField]; exists {
					score++
					break
				}
			}
		}

		threshold := int(float64(len(mapping.Mappings)) * 0.4)
		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = sourceID
		}
	}

	return bestMatch
}

// MapFields transforms raw JSON data to standardized format
func (s *MongoMappingService) MapFields(sourceID string, rawData map[string]interface{}) (map[string]interface{}, error) {
	mapping, err := s.GetMapping(sourceID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})

	var dataSource map[string]interface{}
	if mapping.NestedPath != "" {
		if nested, ok := rawData[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		} else {
			dataSource = rawData
		}
	} else {
		dataSource = rawData
	}

	for _, fieldMap := range mapping.Mappings {
		var value interface{}
		var exists bool

		value, exists = dataSource[fieldMap.SourceField]
		if !exists && mapping.NestedPath != "" {
			value, exists = rawData[fieldMap.SourceField]
		}

		if !exists {
			if fieldMap.DefaultValue != nil {
				value = fieldMap.DefaultValue
			} else if fieldMap.Required {
				return nil, fmt.Errorf("required field '%s' is missing", fieldMap.SourceField)
			} else {
				continue
			}
		}

		transformedValue := s.applyTransform(value, fieldMap.Transform, fieldMap.DataType)
		result[fieldMap.StandardField] = transformedValue
	}

	// Update usage stats
	go s.updateStats(sourceID, true, time.Now())

	return result, nil
}

// applyTransform applies transformations (same as before)
func (s *MongoMappingService) applyTransform(value interface{}, transform string, dataType string) interface{} {
	typedValue := s.convertType(value, dataType)

	if transform == "" {
		return typedValue
	}

	parts := strings.Split(transform, ":")
	if len(parts) != 2 {
		return typedValue
	}

	operation := parts[0]
	var factor float64
	fmt.Sscanf(parts[1], "%f", &factor)

	var numValue float64
	switch v := typedValue.(type) {
	case int:
		numValue = float64(v)
	case int64:
		numValue = float64(v)
	case float64:
		numValue = v
	case float32:
		numValue = float64(v)
	default:
		return typedValue
	}

	var result float64
	switch operation {
	case "multiply":
		result = numValue * factor
	case "divide":
		if factor != 0 {
			result = numValue / factor
		} else {
			result = numValue
		}
	case "add":
		result = numValue + factor
	case "subtract":
		result = numValue - factor
	default:
		return typedValue
	}

	if dataType == "int" {
		return int(result)
	}
	return result
}

// convertType converts value to specified data type (same as before)
func (s *MongoMappingService) convertType(value interface{}, dataType string) interface{} {
	switch dataType {
	case "int":
		switch v := value.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case float32:
			return int(v)
		case string:
			var i int
			fmt.Sscanf(v, "%d", &i)
			return i
		default:
			return 0
		}
	case "float":
		switch v := value.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			var f float64
			fmt.Sscanf(v, "%f", &f)
			return f
		default:
			return 0.0
		}
	case "string":
		return fmt.Sprintf("%v", value)
	case "bool":
		switch v := value.(type) {
		case bool:
			return v
		case int:
			return v != 0
		case string:
			return v == "true" || v == "1" || v == "yes"
		default:
			return false
		}
	default:
		return value
	}
}

// validateMapping performs validation checks
func (s *MongoMappingService) validateMapping(mapping *models.DataSourceMapping) error {
	if mapping.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}

	if len(mapping.Mappings) == 0 {
		return fmt.Errorf("at least one field mapping is required")
	}

	// Validate each field mapping
	for i, fm := range mapping.Mappings {
		if fm.SourceField == "" {
			return fmt.Errorf("mapping[%d]: source_field is required", i)
		}
		if fm.StandardField == "" {
			return fmt.Errorf("mapping[%d]: standard_field is required", i)
		}
		validTypes := map[string]bool{"int": true, "float": true, "string": true, "bool": true}
		if !validTypes[fm.DataType] {
			return fmt.Errorf("mapping[%d]: invalid data_type '%s'", i, fm.DataType)
		}
	}

	return nil
}

// recordHistory saves change history
func (s *MongoMappingService) recordHistory(sourceID, action string, oldMapping, newMapping *models.DataSourceMapping, changedBy, reason string) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	history := models.MappingHistory{
		SourceID:     sourceID,
		Action:       action,
		OldMapping:   oldMapping,
		NewMapping:   newMapping,
		ChangedBy:    changedBy,
		ChangedAt:    time.Now(),
		ChangeReason: reason,
	}

	_, err := s.historyCol.InsertOne(ctx, history)
	if err != nil {
		logger.WriteLog("ERROR", "", "MAPPING_HISTORY", 
			fmt.Sprintf("Failed to record history: %v", err))
	}
}

// updateStats updates usage statistics
func (s *MongoMappingService) updateStats(sourceID string, success bool, startTime time.Time) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	processTime := time.Since(startTime).Milliseconds()

	filter := bson.M{"source_id": sourceID}
	update := bson.M{
		"$inc": bson.M{
			"usage_count": 1,
		},
		"$set": bson.M{
			"last_used": time.Now(),
		},
	}

	if success {
		update["$inc"].(bson.M)["success_count"] = 1
	} else {
		update["$inc"].(bson.M)["failure_count"] = 1
	}

	opts := options.Update().SetUpsert(true)
	_, err := s.statsCol.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		logger.WriteLog("ERROR", "", "MAPPING_STATS", 
			fmt.Sprintf("Failed to update stats: %v", err))
	}

	// Update average process time (simplified - could use $avg aggregation)
	_ = processTime
}

// Close stops the change stream watcher
func (s *MongoMappingService) Close() {
	if s.watchCancel != nil {
		s.watchCancel()
	}
	logger.WriteLog("INFO", "", "MAPPING_SERVICE", "Mapping service closed")
}

// Helper function
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
