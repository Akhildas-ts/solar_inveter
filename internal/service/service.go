// internal/service/service.go (UPDATED)
package service

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"
	"solar_project/internal/repository"
	"solar_project/pkg/logger"

	"github.com/google/uuid"
)

// Service handles business logic with raw data collection and caching
type Service struct {
	cfg    *config.Config
	repo   repository.Repository
	mapper *Mapper
	batch  *BatchWriter

	// Raw data repository
	rawRepo *repository.RawDataRepo

	// Cache
	cache *CacheConfig

	// Statistics
	receivedCount  int64
	processedCount int64
	failedCount    int64
	rawCount       int64
}

// NewService creates a new service instance with raw data collection and caching
func NewService(db config.Database, cfg *config.Config) *Service {
	var repo repository.Repository
	var mapper *Mapper
	var rawRepo *repository.RawDataRepo
	var err error

	// Initialize repository based on DB type
	switch db.GetType() {
	case "mongo":
		mongoDb := db.(*config.MongoDatabase)
		repo = repository.NewMongoRepo(mongoDb)
		mapper, err = NewMapper(mongoDb)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to initialize mapper: %v", err))
		}
		// Initialize raw data repository
		rawRepo = repository.NewRawDataRepo(mongoDb)
	case "influx":
		influxDb := db.(*config.InfluxDatabase)
		repo = repository.NewInfluxRepo(influxDb)
		// For InfluxDB, we still need MongoDB for mappings and raw data
	}

	svc := &Service{
		cfg:     cfg,
		repo:    repo,
		mapper:  mapper,
		rawRepo: rawRepo,
		cache:   NewCacheConfig(),
	}

	// Initialize batch writer
	svc.batch = NewBatchWriter(repo, cfg.BatchSize, time.Duration(cfg.FlushInterval)*time.Millisecond)

	// Start statistics reporter
	go svc.reportStats()

	// Start raw data processor (process unprocessed records periodically)
	go svc.processRawDataLoop()

	// Start cleanup job (remove old processed raw data)
	go svc.cleanupRawDataLoop()

	logger.Info(fmt.Sprintf("Service initialized (DB: %s, Batch: %d, Cache: enabled, RawData: enabled)",
		db.GetType(), cfg.BatchSize))
	return svc
}

// ProcessData handles incoming data with raw data collection
func (svc *Service) ProcessData(rawData map[string]interface{}) error {
	atomic.AddInt64(&svc.receivedCount, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	requestID := ensureRequestID(rawData)

	// STEP 1: Store raw data first (data safety)
	rawID, err := svc.rawRepo.Insert(ctx, rawData, "", requestID)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to store raw data (request_id=%s): %v", requestID, err))
		atomic.AddInt64(&svc.failedCount, 1)
		return fmt.Errorf("failed to store raw data: %w", err)
	}
	atomic.AddInt64(&svc.rawCount, 1)

	// STEP 2: Process and map data
	if err := svc.processAndStore(ctx, rawID, rawData); err != nil {
		// Mark raw data with error (can be reprocessed later)
		if markErr := svc.rawRepo.MarkError(ctx, rawID, err.Error()); markErr != nil {
			logger.Error(fmt.Sprintf("Failed to mark raw data error (raw_id=%s): %v", rawID, markErr))
		}
		atomic.AddInt64(&svc.failedCount, 1)

		if svc.cfg.StrictMode {
			return err
		}
		logger.Warn(fmt.Sprintf("Processing failed for raw_id=%s: %v", rawID, err))
		return nil
	}

	// STEP 3: Mark raw data as processed
	if err := svc.rawRepo.MarkProcessed(ctx, rawID); err != nil {
		logger.Error(fmt.Sprintf("Failed to mark raw data processed (raw_id=%s): %v", rawID, err))
	}
	atomic.AddInt64(&svc.processedCount, 1)

	return nil
}

// processAndStore handles the mapping and storing logic
func (svc *Service) processAndStore(ctx context.Context, rawID string, rawData map[string]interface{}) error {
	// Detect source
	sourceID := svc.mapper.DetectSourceID(rawData)
	if sourceID == "" {
		return fmt.Errorf("unknown data source")
	}

	// Map fields
	standardized, err := svc.mapper.MapFields(sourceID, rawData)
	if err != nil {
		return fmt.Errorf("mapping failed: %w", err)
	}

	// Add metadata
	standardized["device_type"] = sourceID
	standardized["raw_id"] = rawID // Link to raw data
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

	// Extract device timestamp if provided
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

		// Get unprocessed records
		unprocessed, err := svc.rawRepo.GetUnprocessed(ctx, 100)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to get unprocessed raw data: %v", err))
			cancel()
			continue
		}

		// Process each record
		for _, raw := range unprocessed {
			rawID := raw.ID.Hex()
			if err := svc.processAndStore(ctx, rawID, raw.Data); err != nil {
				if markErr := svc.rawRepo.MarkError(ctx, rawID, err.Error()); markErr != nil {
					logger.Error(fmt.Sprintf("Failed to mark raw data error (raw_id=%s): %v", rawID, markErr))
				}
			} else {
				if err := svc.rawRepo.MarkProcessed(ctx, rawID); err != nil {
					logger.Error(fmt.Sprintf("Failed to mark raw data processed (raw_id=%s): %v", rawID, err))
				} else {
					atomic.AddInt64(&svc.processedCount, 1)
				}
			}
		}

		cancel()
	}
}

// cleanupRawDataLoop removes old processed raw data (data retention)
func (svc *Service) cleanupRawDataLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)

		// Keep raw data for 7 days (configurable)
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
	// Try to get from cache first
	cacheKey := "stats:current"
	if cached, found := svc.cache.StatsCache.Get(cacheKey); found {
		return cached.(*domain.Stats), nil
	}

	// Calculate stats
	total, err := svc.repo.Count(ctx, domain.QueryFilter{})
	if err != nil {
		return nil, err
	}

	faultFilter := domain.QueryFilter{}
	faultCode := 0
	faultFilter.FaultCode = &faultCode
	faults, _ := svc.repo.Count(ctx, faultFilter)

	inserted := atomic.LoadInt64(&svc.processedCount)
	failed := atomic.LoadInt64(&svc.failedCount)

	successRate := 100.0
	if inserted+failed > 0 {
		successRate = float64(inserted) / float64(inserted+failed) * 100
	}

	stats := &domain.Stats{
		TotalRecords:  total,
		NormalRecords: total - faults,
		FaultRecords:  faults,
		InsertedCount: inserted,
		FailedCount:   failed,
		BufferSize:    svc.batch.Size(),
		SuccessRate:   successRate,
		DatabaseType:  svc.repo.Type(),
	}

	// Cache for 5 seconds
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
		"processing_rate":     float64(total-unprocessed) / float64(total) * 100,
		"raw_count_memory":    atomic.LoadInt64(&svc.rawCount),
		"cache_stats":         svc.cache.StatsCache.Stats(),
		"mapping_cache_stats": svc.cache.MappingCache.Stats(),
	}, nil
}

// Query retrieves records with caching
func (svc *Service) Query(ctx context.Context, filter domain.QueryFilter) ([]domain.InverterData, error) {
	// Generate cache key
	cacheKey := fmt.Sprintf("query:%s:%d:%d:%d", filter.DeviceID, filter.Limit, filter.Offset, filter.FaultCode)

	// Try cache first
	if cached, found := svc.cache.QueryCache.Get(cacheKey); found {
		return cached.([]domain.InverterData), nil
	}

	// Query database
	results, err := svc.repo.Query(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Cache for 30 seconds
	svc.cache.QueryCache.Set(cacheKey, results, 30*time.Second)

	return results, nil
}

// GetMappings retrieves mappings with caching
func (svc *Service) GetMappings() map[string]*domain.DataSourceMapping {
	if svc.mapper == nil {
		return make(map[string]*domain.DataSourceMapping)
	}

	// Check cache
	cacheKey := "mappings:all"
	if cached, found := svc.cache.MappingCache.Get(cacheKey); found {
		return cached.(map[string]*domain.DataSourceMapping)
	}

	// Get from mapper
	mappings := svc.mapper.GetAll()

	// Cache for 5 minutes
	svc.cache.MappingCache.Set(cacheKey, mappings, 5*time.Minute)

	return mappings
}

// CreateMapping creates a mapping and invalidates cache
func (svc *Service) CreateMapping(mapping *domain.DataSourceMapping) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	svc.cache.MappingCache.Clear() // Invalidate cache
	return svc.mapper.Create(mapping)
}

// UpdateMapping updates a mapping and invalidates cache
func (svc *Service) UpdateMapping(sourceID string, mapping *domain.DataSourceMapping) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	svc.cache.MappingCache.Clear() // Invalidate cache
	return svc.mapper.Update(sourceID, mapping)
}

// DeleteMapping deletes a mapping and invalidates cache
func (svc *Service) DeleteMapping(sourceID string) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	svc.cache.MappingCache.Clear() // Invalidate cache
	return svc.mapper.Delete(sourceID)
}

// ReprocessRawData reprocesses a failed raw data record
func (svc *Service) ReprocessRawData(ctx context.Context, rawID string) error {
	// Mark for reprocessing
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

	lastReceived := int64(0)
	lastProcessed := int64(0)
	lastTime := time.Now()

	for range ticker.C {
		currentReceived := atomic.LoadInt64(&svc.receivedCount)
		currentProcessed := atomic.LoadInt64(&svc.processedCount)
		currentRaw := atomic.LoadInt64(&svc.rawCount)
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		receivedRPS := float64(currentReceived-lastReceived) / elapsed
		processedRPS := float64(currentProcessed-lastProcessed) / elapsed

		logger.Info(fmt.Sprintf("📊 Recv: %d (%.0f/s) | Raw: %d | Proc: %d (%.0f/s) | Buffer: %d | Failed: %d | Cache: %d items",
			currentReceived, receivedRPS,
			currentRaw,
			currentProcessed, processedRPS,
			svc.batch.Size(),
			atomic.LoadInt64(&svc.failedCount),
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

func ensureRequestID(data map[string]interface{}) string {
	if data != nil {
		if value, ok := data["request_id"].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				data["request_id"] = trimmed
				return trimmed
			}
		}
	}

	newID := uuid.NewString()
	if data != nil {
		data["request_id"] = newID
	}
	return newID
}
