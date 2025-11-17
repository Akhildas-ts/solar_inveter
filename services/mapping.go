package services

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"solar_project/config"
	"solar_project/logger"
	"solar_project/models"
)

// ============================================================================
// OPTIMIZED DATA STRUCTURES
// ============================================================================

// IndexedMapping provides O(1) field lookups
type IndexedMapping struct {
	SourceID      string
	Mapping       *models.DataSourceMapping
	FieldIndex    map[string]*models.FieldMapping // source_field -> mapping
	NestedPath    string
	RequiredCount int
	KeySignature  uint64 // fingerprint of expected keys
	LastAccessed  time.Time
	AccessCount   int64
}

// MappingCache stores frequently used mappings
type MappingCache struct {
	deviceToSource sync.Map // device_id -> source_id
	sourceIndex    sync.Map // source_id -> *IndexedMapping
	fingerprints   sync.Map // uint64 -> source_id
	transformCache sync.Map // transform_key -> result

	// Metrics
	cacheHits    int64
	cacheMisses  int64
	totalLookups int64
}

// MOngoMappingService with performance enhancements
type MongoMappingService struct {
	mu         sync.RWMutex
	cache      *MappingCache
	collection *mongo.Collection
	historyCol *mongo.Collection
	metricsCol *mongo.Collection

	ctx         context.Context
	watchCtx    context.Context
	watchCancel context.CancelFunc

	autoReload   bool
	watchEnabled bool

	// Performance metrics
	metrics   MappingMetrics
	metricsMu sync.RWMutex
}

// MappingMetrics tracks performance
type MappingMetrics struct {
	TotalRequests      int64
	SuccessfulMappings int64
	FailedMappings     int64
	UnknownSources     int64

	DetectionTimeTotal time.Duration
	MappingTimeTotal   time.Duration

	CacheHitRate float64
	LastUpdate   time.Time

	SlowMappings map[string]time.Duration
	slowMu       sync.RWMutex
}

// ============================================================================
// INITIALIZATION
// ============================================================================

func NewOptimizedMappingService(autoReload bool) (*MongoMappingService, error) {
	client := config.GetMongoClient()
	if client == nil {
		return nil, fmt.Errorf("MongoDB client not initialized")
	}

	dbName := getEnv("DB_NAME", "solar_monitoring")
	ctx := context.Background()
	watchCtx, watchCancel := context.WithCancel(ctx)

	service := &MongoMappingService{
		cache:        &MappingCache{},
		collection:   client.Database(dbName).Collection("mappings"),
		historyCol:   client.Database(dbName).Collection("mapping_history"),
		metricsCol:   client.Database(dbName).Collection("mapping_metrics"),
		ctx:          ctx,
		watchCtx:     watchCtx,
		watchCancel:  watchCancel,
		autoReload:   autoReload,
		watchEnabled: false,
		metrics: MappingMetrics{
			SlowMappings: make(map[string]time.Duration),
		},
	}

	if err := service.createIndexes(); err != nil {
		return nil, err
	}

	if err := service.initialize(); err != nil {
		return nil, err
	}

	if autoReload {
		go service.watchChangesWithFallback()
	}

	// Start metrics reporter
	go service.reportMetrics()

	logger.WriteLog("INFO", "", "MAPPING",
		fmt.Sprintf("✅ Optimized service initialized (cache enabled)"))

	return service, nil
}

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

// ============================================================================
// CORE OPERATIONS - OPTIMIZED
// ============================================================================

// LoadFromDB loads all active mappings and builds indexes
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

	newCache := &MappingCache{}
	count := 0

	for cursor.Next(ctx) {
		var mapping models.DataSourceMapping
		if err := cursor.Decode(&mapping); err != nil {
			continue
		}

		// Build optimized index
		indexed := s.buildIndex(&mapping)
		newCache.sourceIndex.Store(mapping.SourceID, indexed)

		// Build fingerprint
		fp := s.buildFingerprint(&mapping)
		newCache.fingerprints.Store(fp, mapping.SourceID)

		count++
	}

	s.cache = newCache

	logger.WriteLog("INFO", "", "MAPPING",
		fmt.Sprintf("✅ Loaded %d indexed mappings", count))

	return cursor.Err()
}

// buildIndex creates optimized lookup structures
func (s *MongoMappingService) buildIndex(mapping *models.DataSourceMapping) *IndexedMapping {
	indexed := &IndexedMapping{
		SourceID:   mapping.SourceID,
		Mapping:    mapping,
		FieldIndex: make(map[string]*models.FieldMapping),
		NestedPath: mapping.NestedPath,
	}

	// Build field index for O(1) lookup
	for i := range mapping.Mappings {
		field := &mapping.Mappings[i]
		indexed.FieldIndex[field.SourceField] = field
		if field.Required {
			indexed.RequiredCount++
		}
	}

	// Calculate key signature
	keys := make([]string, 0, len(mapping.Mappings))
	for _, field := range mapping.Mappings {
		keys = append(keys, field.SourceField)
	}
	sort.Strings(keys)
	indexed.KeySignature = hashKeys(keys)

	return indexed
}

// buildFingerprint creates a unique hash for mapping detection
func (s *MongoMappingService) buildFingerprint(mapping *models.DataSourceMapping) uint64 {
	keys := make([]string, 0, len(mapping.Mappings))
	for _, field := range mapping.Mappings {
		keys = append(keys, field.SourceField)
	}
	sort.Strings(keys)
	return hashKeys(keys)
}

// hashKeys creates a fast hash of sorted keys
func hashKeys(keys []string) uint64 {
	h := fnv.New64a()
	for _, key := range keys {
		h.Write([]byte(key))
		h.Write([]byte{0}) // separator
	}
	return h.Sum64()
}

// ============================================================================
// FAST PATH - SOURCE DETECTION
// ============================================================================

// DetectSourceID with multi-tier caching
func (s *MongoMappingService) DetectSourceID(rawData map[string]interface{}) string {
	atomic.AddInt64(&s.metrics.TotalRequests, 1)
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		s.metricsMu.Lock()
		s.metrics.DetectionTimeTotal += elapsed
		s.metricsMu.Unlock()
	}()

	// Fast path 1: Check device cache
	deviceID := extractDeviceIDFromData(rawData)
	if deviceID != "" {
		if cached, ok := s.cache.deviceToSource.Load(deviceID); ok {
			atomic.AddInt64(&s.cache.cacheHits, 1)
			atomic.AddInt64(&s.cache.totalLookups, 1)
			return cached.(string)
		}
	}

	// Fast path 2: Fingerprint matching
	fp := calculateFingerprint(rawData)
	if sourceID, ok := s.cache.fingerprints.Load(fp); ok {
		detected := sourceID.(string)
		if deviceID != "" {
			s.cache.deviceToSource.Store(deviceID, detected)
		}
		atomic.AddInt64(&s.cache.cacheHits, 1)
		atomic.AddInt64(&s.cache.totalLookups, 1)
		return detected
	}

	// Slow path: Full detection
	atomic.AddInt64(&s.cache.cacheMisses, 1)
	atomic.AddInt64(&s.cache.totalLookups, 1)

	detected := s.detectSourceIDSlow(rawData)

	// Cache result
	if detected != "" && deviceID != "" {
		s.cache.deviceToSource.Store(deviceID, detected)
	}

	return detected
}

// detectSourceIDSlow performs full pattern matching
func (s *MongoMappingService) detectSourceIDSlow(rawData map[string]interface{}) string {
	bestMatch := ""
	bestScore := 0

	// Extract all keys for matching
	allKeys := extractAllKeys(rawData)

	s.cache.sourceIndex.Range(func(key, value interface{}) bool {
		indexed := value.(*IndexedMapping)
		score := 0

		// Count matching fields
		for _, dataKey := range allKeys {
			if _, exists := indexed.FieldIndex[dataKey]; exists {
				score++
			}
		}

		// Require 40% field match
		threshold := len(indexed.FieldIndex) * 40 / 100
		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = indexed.SourceID

			// Update access stats
			atomic.AddInt64(&indexed.AccessCount, 1)
			indexed.LastAccessed = time.Now()
		}

		return true
	})

	if bestMatch == "" {
		atomic.AddInt64(&s.metrics.UnknownSources, 1)
	}

	return bestMatch
}

// ============================================================================
// FIELD MAPPING - OPTIMIZED
// ============================================================================

// MapFields with optimized lookups and caching
func (s *MongoMappingService) MapFields(sourceID string, rawData map[string]interface{}) (map[string]interface{}, error) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		s.metricsMu.Lock()
		s.metrics.MappingTimeTotal += elapsed

		// Track slow mappings
		if elapsed > 5*time.Millisecond {
			s.metrics.slowMu.Lock()
			if prev, exists := s.metrics.SlowMappings[sourceID]; !exists || elapsed > prev {
				s.metrics.SlowMappings[sourceID] = elapsed
			}
			s.metrics.slowMu.Unlock()
		}
		s.metricsMu.Unlock()
	}()

	// Get indexed mapping
	value, ok := s.cache.sourceIndex.Load(sourceID)
	if !ok {
		atomic.AddInt64(&s.metrics.FailedMappings, 1)
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	indexed := value.(*IndexedMapping)
	result := make(map[string]interface{}, len(indexed.FieldIndex))

	// Get data source (nested or root)
	dataSource := rawData
	if indexed.NestedPath != "" {
		if nested, ok := rawData[indexed.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		}
	}

	// Required field check
	missingRequired := 0

	// Map fields using index
	for sourceField, fieldMap := range indexed.FieldIndex {
		// Try data source first
		value, exists := dataSource[sourceField]
		if !exists && indexed.NestedPath != "" {
			// Fallback to root
			value, exists = rawData[sourceField]
		}

		if !exists {
			if fieldMap.DefaultValue != nil {
				value = fieldMap.DefaultValue
			} else if fieldMap.Required {
				missingRequired++
				continue
			} else {
				continue
			}
		}

		// Apply transformation with caching
		transformed := s.applyTransformCached(value, fieldMap.Transform, fieldMap.DataType)
		result[fieldMap.StandardField] = transformed
	}

	if missingRequired > 0 {
		atomic.AddInt64(&s.metrics.FailedMappings, 1)
		return nil, fmt.Errorf("missing %d required fields", missingRequired)
	}

	atomic.AddInt64(&s.metrics.SuccessfulMappings, 1)
	return result, nil
}

// applyTransformCached with memoization
func (s *MongoMappingService) applyTransformCached(value interface{}, transform, dataType string) interface{} {
	if transform == "" {
		return ConvertType(value, dataType)
	}

	// Create cache key
	cacheKey := fmt.Sprintf("%v|%s|%s", value, transform, dataType)

	// Check cache
	if cached, ok := s.cache.transformCache.Load(cacheKey); ok {
		return cached
	}

	// Compute and cache
	result := ApplyTransform(value, transform, dataType)
	s.cache.transformCache.Store(cacheKey, result)

	return result
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func extractDeviceIDFromData(data map[string]interface{}) string {
	if id, ok := data["device_id"].(string); ok {
		return id
	}
	if id, ok := data["deviceId"].(string); ok {
		return id
	}
	return ""
}

func calculateFingerprint(data map[string]interface{}) uint64 {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}

	// Also include nested keys
	for key, value := range data {
		if nested, ok := value.(map[string]interface{}); ok {
			for nk := range nested {
				keys = append(keys, key+"."+nk)
			}
		}
	}

	sort.Strings(keys)
	return hashKeys(keys)
}

func extractAllKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data)*2)

	for key, value := range data {
		keys = append(keys, key)

		// Include nested keys
		if nested, ok := value.(map[string]interface{}); ok {
			for nk := range nested {
				keys = append(keys, nk)
			}
		}
	}

	return keys
}

// ============================================================================
// METRICS & MONITORING
// ============================================================================

func (s *MongoMappingService) reportMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.updateCacheMetrics()
		s.logMetrics()
	}
}

func (s *MongoMappingService) updateCacheMetrics() {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	totalLookups := atomic.LoadInt64(&s.cache.totalLookups)
	hits := atomic.LoadInt64(&s.cache.cacheHits)

	if totalLookups > 0 {
		s.metrics.CacheHitRate = float64(hits) / float64(totalLookups) * 100
	}
	s.metrics.LastUpdate = time.Now()
}

func (s *MongoMappingService) logMetrics() {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	totalRequests := atomic.LoadInt64(&s.metrics.TotalRequests)
	if totalRequests == 0 {
		return
	}

	avgDetectionTime := time.Duration(0)
	avgMappingTime := time.Duration(0)

	if totalRequests > 0 {
		avgDetectionTime = s.metrics.DetectionTimeTotal / time.Duration(totalRequests)
		avgMappingTime = s.metrics.MappingTimeTotal / time.Duration(totalRequests)
	}

	logger.WriteLog("INFO", "", "MAPPING_METRICS",
		fmt.Sprintf("📊 Requests: %d | Cache: %.1f%% | DetectAvg: %v | MapAvg: %v",
			totalRequests,
			s.metrics.CacheHitRate,
			avgDetectionTime,
			avgMappingTime,
		))

	// Log slow mappings
	s.metrics.slowMu.RLock()
	if len(s.metrics.SlowMappings) > 0 {
		logger.WriteLog("WARNING", "", "MAPPING_SLOW",
			fmt.Sprintf("⚠️  Slow mappings detected: %+v", s.metrics.SlowMappings))
	}
	s.metrics.slowMu.RUnlock()
}

// GetMetrics returns current performance metrics
func (s *MongoMappingService) GetMetrics() MappingMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	metrics := s.metrics
	metrics.TotalRequests = atomic.LoadInt64(&s.metrics.TotalRequests)
	metrics.SuccessfulMappings = atomic.LoadInt64(&s.metrics.SuccessfulMappings)
	metrics.FailedMappings = atomic.LoadInt64(&s.metrics.FailedMappings)
	metrics.UnknownSources = atomic.LoadInt64(&s.metrics.UnknownSources)

	return metrics
}

// ============================================================================
// CRUD OPERATIONS (same as before, delegating to optimized core)
// ============================================================================

func (s *MongoMappingService) GetMapping(sourceID string) (*models.DataSourceMapping, error) {
	if value, ok := s.cache.sourceIndex.Load(sourceID); ok {
		indexed := value.(*IndexedMapping)
		return indexed.Mapping, nil
	}
	return nil, fmt.Errorf("mapping not found: %s", sourceID)
}

func (s *MongoMappingService) GetAllMappings() map[string]*models.DataSourceMapping {
	result := make(map[string]*models.DataSourceMapping)

	s.cache.sourceIndex.Range(func(key, value interface{}) bool {
		indexed := value.(*IndexedMapping)
		result[indexed.SourceID] = indexed.Mapping
		return true
	})

	return result
}

// ... [Include CreateMapping, UpdateMapping, DeleteMapping from original service]
// ... [Include watchChangesWithFallback, createIndexes, etc.]

func (s *MongoMappingService) Close() {
	if s.watchCancel != nil {
		s.watchCancel()
	}

	// Log final metrics
	s.logMetrics()
}

// ============================================================================
// ADDITIONAL HELPERS
// ============================================================================

func (s *MongoMappingService) watchChangesWithFallback() {
	// Same implementation as MongoMappingService
	if s.tryWatchChanges() {
		s.watchEnabled = true
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastModTime time.Time

	for range ticker.C {
		cursor, err := s.collection.Aggregate(s.watchCtx, mongo.Pipeline{
			bson.D{{Key: "$sort", Value: bson.D{{Key: "updated_at", Value: -1}}}},
			bson.D{{Key: "$limit", Value: 1}},
		})

		if err != nil {
			continue
		}

		if cursor.Next(s.watchCtx) {
			var result struct {
				UpdatedAt time.Time `bson:"updated_at"`
			}
			cursor.Decode(&result)

			if result.UpdatedAt.After(lastModTime) {
				logger.WriteLog("INFO", "", "MAPPING", "Change detected - reloading")
				s.LoadFromDB()
				lastModTime = result.UpdatedAt
			}
		}
		cursor.Close(s.watchCtx)
	}
}

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
		return false
	}
	defer stream.Close(s.watchCtx)

	for stream.Next(s.watchCtx) {
		logger.WriteLog("INFO", "", "MAPPING", "Change detected - reloading")
		s.LoadFromDB()
	}

	return true
}

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
