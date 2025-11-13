package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"solar_project/config"
	"solar_project/logger"
	"solar_project/models"
)

// MongoMappingService manages field mappings with MongoDB backend
type MongoMappingService struct {
	mu           sync.RWMutex
	cache        map[string]*models.DataSourceMapping
	collection   *mongo.Collection
	historyCol   *mongo.Collection
	ctx          context.Context
	watchCtx     context.Context
	watchCancel  context.CancelFunc
	autoReload   bool
	watchEnabled bool // ✅ Track if watch is available
}

// NewMongoMappingService creates and initializes the mapping service
func NewMongoMappingService(autoReload bool) (*MongoMappingService, error) {
	client := config.GetMongoClient()
	if client == nil {
		return nil, fmt.Errorf("MongoDB client not initialized")
	}

	dbName := getEnv("DB_NAME", "solar_monitoring")
	ctx := context.Background()
	watchCtx, watchCancel := context.WithCancel(ctx)

	service := &MongoMappingService{
		cache:        make(map[string]*models.DataSourceMapping),
		collection:   client.Database(dbName).Collection("mappings"),
		historyCol:   client.Database(dbName).Collection("mapping_history"),
		ctx:          ctx,
		watchCtx:     watchCtx,
		watchCancel:  watchCancel,
		autoReload:   autoReload,
		watchEnabled: false, // ✅ Will be set if watch succeeds
	}

	if err := service.createIndexes(); err != nil {
		return nil, err
	}

	if err := service.initialize(); err != nil {
		return nil, err
	}

	// ✅ FIXED: Try to enable watch, but don't fail if not available
	if autoReload {
		go service.watchChangesWithFallback()
	}

	logger.WriteLog("INFO", "", "MAPPING",
		fmt.Sprintf("Service initialized with %d mappings", len(service.cache)))

	return service, nil
}

// initialize loads mappings from MongoDB or creates defaults if empty
func (s *MongoMappingService) initialize() error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	count, err := s.collection.CountDocuments(ctx, bson.M{"active": true})
	if err != nil {
		return fmt.Errorf("failed to check mappings: %w", err)
	}

	if count == 0 {
		if err := s.seedDefaults(); err != nil {
			return fmt.Errorf("failed to seed defaults: %w", err)
		}
	}

	return s.LoadFromDB()
}

// seedDefaults inserts default mappings into MongoDB
func (s *MongoMappingService) seedDefaults() error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	defaults := GetDefaultMappings()

	for _, mapping := range defaults {
		if _, err := s.collection.InsertOne(ctx, mapping); err != nil {
			return err
		}
	}

	logger.WriteLog("INFO", "", "MAPPING",
		fmt.Sprintf("Created %d default mappings", len(defaults)))

	return nil
}

// loadFromDB loads all active mappings from MongoDB into cache
func (s *MongoMappingService) LoadFromDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cursor, err := s.collection.Find(ctx, bson.M{"active": true})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	newCache := make(map[string]*models.DataSourceMapping)

	for cursor.Next(ctx) {
		var mapping models.DataSourceMapping
		if err := cursor.Decode(&mapping); err != nil {
			continue
		}
		newCache[mapping.SourceID] = &mapping
	}

	s.cache = newCache

	logger.WriteLog("INFO", "", "MAPPING",
		fmt.Sprintf("Loaded %d mappings from MongoDB", len(newCache)))

	return cursor.Err()
}

// ✅ FIXED: watchChangesWithFallback - Try watch, fallback to polling if unavailable
func (s *MongoMappingService) watchChangesWithFallback() {
	// First, try to use change streams
	if s.tryWatchChanges() {
		s.watchEnabled = true
		return
	}

	// ✅ Fallback: Poll for changes every 10 seconds
	logger.WriteLog("INFO", "", "MAPPING",
		"Change streams not available - using polling mode (10s interval)")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastModTime time.Time
	fileInfo, err := s.collection.Aggregate(s.watchCtx, mongo.Pipeline{
		bson.D{{Key: "$sort", Value: bson.D{{Key: "updated_at", Value: -1}}}},
		bson.D{{Key: "$limit", Value: 1}},
		bson.D{{Key: "$project", Value: bson.D{{Key: "updated_at", Value: 1}}}},
	})
	if err == nil {
		defer fileInfo.Close(s.watchCtx)
		if fileInfo.Next(s.watchCtx) {
			var result bson.M
			fileInfo.Decode(&result)
			if updatedAt, ok := result["updated_at"].(time.Time); ok {
				lastModTime = updatedAt
			}
		}
	}

	for range ticker.C {
		// Get latest modification time
		cursor, err := s.collection.Aggregate(s.watchCtx, mongo.Pipeline{
			bson.D{{Key: "$sort", Value: bson.D{{Key: "updated_at", Value: -1}}}},
			bson.D{{Key: "$limit", Value: 1}},
			bson.D{{Key: "$project", Value: bson.D{{Key: "updated_at", Value: 1}}}},
		})

		if err != nil {
			continue
		}

		if cursor.Next(s.watchCtx) {
			var result bson.M
			cursor.Decode(&result)
			if updatedAt, ok := result["updated_at"].(time.Time); ok {
				if updatedAt.After(lastModTime) {
					logger.WriteLog("INFO", "", "MAPPING", "Change detected (polling) - reloading")
					s.LoadFromDB()
					lastModTime = updatedAt
				}
			}
		}
		cursor.Close(s.watchCtx)
	}
}

// ✅ NEW: tryWatchChanges - Attempts to use change streams
func (s *MongoMappingService) tryWatchChanges() bool {
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
		logger.WriteLog("WARNING", "", "MAPPING",
			fmt.Sprintf("Change streams not available (replica sets required): %v. Using polling fallback.", err))
		return false
	}
	defer stream.Close(s.watchCtx)

	logger.WriteLog("INFO", "", "MAPPING", "✅ Change streams enabled - real-time mapping updates")

	for stream.Next(s.watchCtx) {
		logger.WriteLog("INFO", "", "MAPPING", "Change detected - reloading")
		s.LoadFromDB()
	}

	return true
}

// GetMapping retrieves a mapping from cache
func (s *MongoMappingService) GetMapping(sourceID string) (*models.DataSourceMapping, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mapping, exists := s.cache[sourceID]
	if !exists {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	return mapping, nil
}

// GetAllMappings returns all cached mappings
func (s *MongoMappingService) GetAllMappings() map[string]*models.DataSourceMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*models.DataSourceMapping)
	for k, v := range s.cache {
		result[k] = v
	}
	return result
}

// MapFields transforms raw JSON to standardized format
func (s *MongoMappingService) MapFields(sourceID string, rawData map[string]interface{}) (map[string]interface{}, error) {
	mapping, err := s.GetMapping(sourceID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	dataSource := s.getDataSource(rawData, mapping.NestedPath)

	for _, fieldMap := range mapping.Mappings {
		value := s.extractValue(dataSource, rawData, fieldMap)

		if value == nil {
			if fieldMap.Required {
				return nil, fmt.Errorf("required field missing: %s", fieldMap.SourceField)
			}
			continue
		}

		result[fieldMap.StandardField] = ApplyTransform(value, fieldMap.Transform, fieldMap.DataType)
	}

	return result, nil
}

// getDataSource returns the nested data object or root
func (s *MongoMappingService) getDataSource(raw map[string]interface{}, nestedPath string) map[string]interface{} {
	if nestedPath == "" {
		return raw
	}

	if nested, ok := raw[nestedPath].(map[string]interface{}); ok {
		return nested
	}

	return raw
}

// extractValue gets field value from nested or root data
func (s *MongoMappingService) extractValue(dataSource, rawData map[string]interface{}, field models.FieldMapping) interface{} {
	if value, exists := dataSource[field.SourceField]; exists {
		return value
	}

	if value, exists := rawData[field.SourceField]; exists {
		return value
	}

	return field.DefaultValue
}

// DetectSourceID identifies the source from raw data
func (s *MongoMappingService) DetectSourceID(rawData map[string]interface{}) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range s.cache {
		score := s.calculateMatchScore(rawData, mapping)
		threshold := len(mapping.Mappings) * 40 / 100

		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = sourceID
		}
	}

	return bestMatch
}

// calculateMatchScore counts matching fields
func (s *MongoMappingService) calculateMatchScore(raw map[string]interface{}, mapping *models.DataSourceMapping) int {
	score := 0
	sources := []map[string]interface{}{raw}

	if mapping.NestedPath != "" {
		if nested, ok := raw[mapping.NestedPath].(map[string]interface{}); ok {
			sources = append(sources, nested)
		}
	}

	for _, field := range mapping.Mappings {
		for _, src := range sources {
			if _, exists := src[field.SourceField]; exists {
				score++
				break
			}
		}
	}

	return score
}

// CreateMapping adds a new mapping
func (s *MongoMappingService) CreateMapping(mapping *models.DataSourceMapping, createdBy string) error {
	if err := ValidateMapping(mapping); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	mapping.CreatedAt = time.Now()
	mapping.UpdatedAt = time.Now()
	mapping.CreatedBy = createdBy
	mapping.Active = true
	mapping.Version = 1

	_, err := s.collection.InsertOne(ctx, mapping)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("mapping exists: %s", mapping.SourceID)
		}
		return err
	}

	s.recordHistory(mapping.SourceID, "CREATE", nil, mapping, createdBy, "Created")

	return nil
}

// UpdateMapping modifies an existing mapping
func (s *MongoMappingService) UpdateMapping(sourceID string, mapping *models.DataSourceMapping, updatedBy, reason string) error {
	oldMapping, err := s.GetMapping(sourceID)
	if err != nil {
		return err
	}

	if err := ValidateMapping(mapping); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	mapping.UpdatedAt = time.Now()
	mapping.UpdatedBy = updatedBy
	mapping.Version = oldMapping.Version + 1
	mapping.CreatedAt = oldMapping.CreatedAt
	mapping.CreatedBy = oldMapping.CreatedBy

	filter := bson.M{"source_id": sourceID}
	update := bson.M{"$set": mapping}

	_, err = s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	s.recordHistory(sourceID, "UPDATE", oldMapping, mapping, updatedBy, reason)

	return nil
}

// DeleteMapping soft-deletes a mapping
func (s *MongoMappingService) DeleteMapping(sourceID, deletedBy, reason string) error {
	oldMapping, err := s.GetMapping(sourceID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	filter := bson.M{"source_id": sourceID}
	update := bson.M{"$set": bson.M{
		"active":     false,
		"updated_at": time.Now(),
		"updated_by": deletedBy,
	}}

	_, err = s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	s.recordHistory(sourceID, "DELETE", oldMapping, nil, deletedBy, reason)

	return nil
}

// recordHistory logs mapping changes
func (s *MongoMappingService) recordHistory(sourceID, action string, old, new *models.DataSourceMapping, by, reason string) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	history := models.MappingHistory{
		SourceID:     sourceID,
		Action:       action,
		OldMapping:   old,
		NewMapping:   new,
		ChangedBy:    by,
		ChangedAt:    time.Now(),
		ChangeReason: reason,
	}

	s.historyCol.InsertOne(ctx, history)
}

// createIndexes creates MongoDB indexes
func (s *MongoMappingService) createIndexes() error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "source_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "active", Value: 1}}},
		{Keys: bson.D{{Key: "updated_at", Value: -1}}},
	}

	_, err := s.collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// Close stops the change watcher
func (s *MongoMappingService) Close() {
	if s.watchCancel != nil {
		s.watchCancel()
	}
}