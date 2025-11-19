// internal/service/service.go
// REPLACE your existing service.go with this file

package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"
	"solar_project/internal/repository"
	"solar_project/pkg/logger"

	"github.com/google/uuid"
)

// Service handles business logic with ultra-fast mapping
type Service struct {
	cfg    *config.Config
	repo   repository.Repository
	mapper *UltraMapper  // Using ultra-fast mapper
	batch  *BatchWriter

	rawRepo *repository.RawDataRepo
	cache   *CacheConfig

	// Lock-free statistics
	receivedCount  uint64
	processedCount uint64
	failedCount    uint64
	rawCount       uint64
}

// NewService creates service with ultra-optimized mapper
func NewService(db config.Database, cfg *config.Config) *Service {
	var repo repository.Repository
	var mapper *UltraMapper
	var rawRepo *repository.RawDataRepo
	var err error

	switch db.GetType() {
	case "mongo":
		mongoDb := db.(*config.MongoDatabase)
		repo = repository.NewMongoRepo(mongoDb)
		
		// Try to create UltraMapper, fallback to original on error
		mapper, err = NewUltraMapper(mongoDb)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to initialize UltraMapper: %v", err))
			// Fallback: create basic mapper or handle error
			panic(err) // Or handle gracefully
		}
		
		rawRepo = repository.NewRawDataRepo(mongoDb)
		
	case "influx":
		influxDb := db.(*config.InfluxDatabase)
		repo = repository.NewInfluxRepo(influxDb)
		// For InfluxDB, you still need MongoDB for mappings
		// You'll need to handle this case properly
	}

	svc := &Service{
		cfg:     cfg,
		repo:    repo,
		mapper:  mapper,
		rawRepo: rawRepo,
		cache:   NewCacheConfig(),
	}

	svc.batch = NewBatchWriter(repo, cfg.BatchSize, time.Duration(cfg.FlushInterval)*time.Millisecond)

	// Start background jobs (matching original names - NO rawBatchFlusher)
	go svc.reportStats()
	go svc.processRawDataLoop()
	go svc.cleanupRawDataLoop()

	logger.Info(fmt.Sprintf("Service initialized with UltraMapper (DB: %s, Batch: %d, ZeroCost: enabled)",
		db.GetType(), cfg.BatchSize))
	
	return svc
}

// ProcessData handles incoming data with ultra-fast mapping
func (svc *Service) ProcessData(rawData map[string]interface{}) error {
	atomic.AddUint64(&svc.receivedCount, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	requestID := ensureRequestIDZero(rawData)

	// STEP 1: Store raw data (using existing RawDataRepo.Insert)
	rawID, err := svc.rawRepo.Insert(ctx, rawData, "", requestID)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to store raw data (request_id=%s): %v", requestID, err))
		atomic.AddUint64(&svc.failedCount, 1)
		return fmt.Errorf("failed to store raw data: %w", err)
	}
	atomic.AddUint64(&svc.rawCount, 1)

	// STEP 2: Process with ultra mapper
	if err := svc.processAndStoreUltra(ctx, rawID, rawData); err != nil {
		// Mark error in raw data
		if markErr := svc.rawRepo.MarkError(ctx, rawID, err.Error()); markErr != nil {
			logger.Error(fmt.Sprintf("Failed to mark raw data error: %v", markErr))
		}
		atomic.AddUint64(&svc.failedCount, 1)

		if svc.cfg.StrictMode {
			return err
		}
		logger.Warn(fmt.Sprintf("Processing failed for raw_id=%s (non-strict mode): %v", rawID, err))
		return nil
	}

	// STEP 3: Mark as processed
	if err := svc.rawRepo.MarkProcessed(ctx, rawID); err != nil {
		logger.Error(fmt.Sprintf("Failed to mark processed: %v", err))
	}
	atomic.AddUint64(&svc.processedCount, 1)

	return nil
}

// processAndStoreUltra uses ultra-fast mapper
func (svc *Service) processAndStoreUltra(ctx context.Context, rawID string, rawData map[string]interface{}) error {
	// Detect source (ultra-fast with bitmask checking)
	sourceID := svc.mapper.DetectSourceID(rawData)
	if sourceID == "" {
		return fmt.Errorf("unknown data source")
	}

	// Map fields (zero-allocation path with pre-compiled scale functions)
	standardized, err := svc.mapper.MapFields(sourceID, rawData)
	if err != nil {
		return fmt.Errorf("mapping failed: %w", err)
	}

	// Add metadata
	standardized["device_type"] = sourceID
	standardized["raw_id"] = rawID
	if requestID, ok := rawData["request_id"].(string); ok && requestID != "" {
		standardized["request_id"] = requestID
	} else {
		standardized["request_id"] = rawID
	}
	if deviceName, ok := rawData["device_name"].(string); ok {
		standardized["device_name"] = deviceName
	}
	if deviceID, ok := rawData["device_id"].(string); ok {
		standardized["device_id"] = deviceID
	}

	// Parse timestamp
	timestamp := time.Now()
	if ts, ok := rawData["device_timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			timestamp = parsed
		}
	}

	// Convert to domain model
	record := svc.convertToRecord(standardized, timestamp)

	// Add to batch
	svc.batch.Add(record)

	return nil
}

// processRawDataLoop periodically processes unprocessed raw data
func (svc *Service) processRawDataLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		unprocessed, err := svc.rawRepo.GetUnprocessed(ctx, 100)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to get unprocessed raw data: %v", err))
			cancel()
			continue
		}

		for _, raw := range unprocessed {
			rawID := raw.ID.Hex()
			if err := svc.processAndStoreUltra(ctx, rawID, raw.Data); err != nil {
				if markErr := svc.rawRepo.MarkError(ctx, rawID, err.Error()); markErr != nil {
					logger.Error(fmt.Sprintf("Failed to mark error: %v", markErr))
				}
			} else {
				if err := svc.rawRepo.MarkProcessed(ctx, rawID); err != nil {
					logger.Error(fmt.Sprintf("Failed to mark processed: %v", err))
				} else {
					atomic.AddUint64(&svc.processedCount, 1)
				}
			}
		}

		cancel()
	}
}

// cleanupRawDataLoop removes old processed raw data
func (svc *Service) cleanupRawDataLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)

		retentionDays := 7
		cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

		deleted, err := svc.rawRepo.Cleanup(ctx, cutoffTime)
		if err != nil {
			logger.Error(fmt.Sprintf("Raw data cleanup failed: %v", err))
		} else if deleted > 0 {
			logger.Info(fmt.Sprintf("Cleaned up %d old raw data records", deleted))
		}

		cancel()
	}
}

// GetStats returns current statistics with caching
func (svc *Service) GetStats(ctx context.Context) (*domain.Stats, error) {
	cacheKey := "stats:current"
	if cached, found := svc.cache.StatsCache.Get(cacheKey); found {
		return cached.(*domain.Stats), nil
	}

	total, err := svc.repo.Count(ctx, domain.QueryFilter{})
	if err != nil {
		return nil, err
	}

	faultCode := 0
	faultFilter := domain.QueryFilter{FaultCode: &faultCode}
	faults, _ := svc.repo.Count(ctx, faultFilter)

	inserted := atomic.LoadUint64(&svc.processedCount)
	failed := atomic.LoadUint64(&svc.failedCount)

	successRate := 100.0
	if inserted+failed > 0 {
		successRate = float64(inserted) / float64(inserted+failed) * 100
	}

	stats := &domain.Stats{
		TotalRecords:  total,
		NormalRecords: total - faults,
		FaultRecords:  faults,
		InsertedCount: int64(inserted),
		FailedCount:   int64(failed),
		BufferSize:    svc.batch.Size(),
		SuccessRate:   successRate,
		DatabaseType:  svc.repo.Type(),
	}

	svc.cache.StatsCache.Set(cacheKey, stats, 5*time.Second)
	return stats, nil
}

// GetRawDataStats returns raw data statistics
func (svc *Service) GetRawDataStats(ctx context.Context) (map[string]interface{}, error) {
	total, _ := svc.rawRepo.Count(ctx)
	unprocessed, _ := svc.rawRepo.CountUnprocessed(ctx)
	errors, _ := svc.rawRepo.CountErrors(ctx)

	return map[string]interface{}{
		"total_raw":           total,
		"unprocessed":         unprocessed,
		"with_errors":         errors,
		"processed":           total - unprocessed,
		"processing_rate":     calculateRate(total, unprocessed),
		"raw_count_memory":    atomic.LoadUint64(&svc.rawCount),
		"cache_stats":         svc.cache.StatsCache.Stats(),
		"mapping_cache_stats": svc.cache.MappingCache.Stats(),
	}, nil
}

func calculateRate(total, unprocessed int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(total-unprocessed) / float64(total) * 100
}

// Query retrieves records with caching
func (svc *Service) Query(ctx context.Context, filter domain.QueryFilter) ([]domain.InverterData, error) {
	cacheKey := fmt.Sprintf("query:%s:%d:%d:%v", filter.DeviceID, filter.Limit, filter.Offset, filter.FaultCode)

	if cached, found := svc.cache.QueryCache.Get(cacheKey); found {
		return cached.([]domain.InverterData), nil
	}

	results, err := svc.repo.Query(ctx, filter)
	if err != nil {
		return nil, err
	}

	svc.cache.QueryCache.Set(cacheKey, results, 30*time.Second)
	return results, nil
}

// GetMappings retrieves mappings (Note: UltraMapper stores differently)
func (svc *Service) GetMappings() map[string]*domain.DataSourceMapping {
	// UltraMapper uses compiled format, would need conversion
	// For now, return empty or implement conversion layer
	return make(map[string]*domain.DataSourceMapping)
}

// CreateMapping creates a new mapping
func (svc *Service) CreateMapping(mapping *domain.DataSourceMapping) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	svc.cache.MappingCache.Clear()
	
	// Insert into MongoDB and reload
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	mapping.Active = true
	mapping.CreatedAt = time.Now()
	mapping.UpdatedAt = time.Now()
	
	_, err := svc.mapper.collection.InsertOne(ctx, mapping)
	if err != nil {
		return err
	}
	
	return svc.mapper.Load()
}

// UpdateMapping updates an existing mapping
func (svc *Service) UpdateMapping(sourceID string, mapping *domain.DataSourceMapping) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	svc.cache.MappingCache.Clear()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	mapping.UpdatedAt = time.Now()
	filter := map[string]interface{}{"source_id": sourceID}
	update := map[string]interface{}{"$set": mapping}
	
	_, err := svc.mapper.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	
	return svc.mapper.Load()
}

// DeleteMapping soft-deletes a mapping
func (svc *Service) DeleteMapping(sourceID string) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	svc.cache.MappingCache.Clear()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	filter := map[string]interface{}{"source_id": sourceID}
	update := map[string]interface{}{
		"$set": map[string]interface{}{
			"active":     false,
			"updated_at": time.Now(),
		},
	}
	
	_, err := svc.mapper.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	
	return svc.mapper.Load()
}

// ReprocessRawData marks a raw data record for reprocessing
func (svc *Service) ReprocessRawData(ctx context.Context, rawID string) error {
	if err := svc.rawRepo.Reprocess(ctx, rawID); err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("Marked raw_id=%s for reprocessing", rawID))
	return nil
}

// convertToRecord converts standardized map to domain model
func (svc *Service) convertToRecord(data map[string]interface{}, timestamp time.Time) domain.InverterData {
	return domain.InverterData{
		DeviceType:     getString(data, "device_type", "unknown"),
		DeviceName:     getString(data, "device_name", "unknown"),
		DeviceID:       getString(data, "device_id", "unknown"),
		SignalStrength: getString(data, "signal_strength", ""),
		Timestamp:      timestamp,
		Data: domain.InverterDetails{
			SerialNo:    getString(data, "serial_no", "UNKNOWN"),
			Voltage:     getInt(data, "voltage", 0),
			Power:       getInt(data, "power", 0),
			Frequency:   getInt(data, "frequency", 0),
			TodayEnergy: getInt(data, "today_energy", 0),
			TotalEnergy: getInt(data, "total_energy", 0),
			Temperature: getInt(data, "temperature", 0),
			FaultCode:   getInt(data, "fault_code", 0),
		},
	}
}

// reportStats prints statistics periodically
func (svc *Service) reportStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastReceived := uint64(0)
	lastProcessed := uint64(0)
	lastTime := time.Now()

	for range ticker.C {
		currentReceived := atomic.LoadUint64(&svc.receivedCount)
		currentProcessed := atomic.LoadUint64(&svc.processedCount)
		currentRaw := atomic.LoadUint64(&svc.rawCount)
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		receivedRPS := float64(currentReceived-lastReceived) / elapsed
		processedRPS := float64(currentProcessed-lastProcessed) / elapsed

		logger.Info(fmt.Sprintf("⚡ Recv: %d (%.0f/s) | Raw: %d | Proc: %d (%.0f/s) | Buf: %d | Fail: %d | Cache: %d",
			currentReceived, receivedRPS,
			currentRaw,
			currentProcessed, processedRPS,
			svc.batch.Size(),
			atomic.LoadUint64(&svc.failedCount),
			svc.cache.StatsCache.Size()+svc.cache.MappingCache.Size()+svc.cache.QueryCache.Size()))

		lastReceived = currentReceived
		lastProcessed = currentProcessed
		lastTime = currentTime
	}
}

// Close cleanup resources
func (svc *Service) Close() error {
	if svc.batch != nil {
		svc.batch.Flush()
		svc.batch.Close()
	}
	if svc.mapper != nil {
		svc.mapper.Close()
	}
	if svc.cache != nil {
		svc.cache.CloseAll()
	}
	return nil
}

// Helper functions
func getString(data map[string]interface{}, key, defaultValue string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return defaultValue
}

func getInt(data map[string]interface{}, key string, defaultValue int) int {
	switch val := data[key].(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	default:
		return defaultValue
	}
}

func ensureRequestIDZero(data map[string]interface{}) string {
	if data != nil {
		if value, ok := data["request_id"].(string); ok && value != "" {
			return value
		}
	}
	newID := uuid.NewString()
	if data != nil {
		data["request_id"] = newID
	}
	return newID
}