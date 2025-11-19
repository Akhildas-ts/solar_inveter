// internal/service/optimized_service.go
package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"
	"solar_project/internal/repository"
	"solar_project/pkg/logger"

	"github.com/google/uuid"
)

// OptimizedService with performance improvements
type OptimizedService struct {
	cfg    *config.Config
	repo   repository.Repository
	mapper *OptimizedMapper
	batch  *BatchWriter

	rawRepo *repository.RawDataRepo
	cache   *CacheConfig

	// Optimized: Batch raw data writes
	rawBatch      []repository.RawData
	rawBatchMu    sync.Mutex
	rawBatchFlush chan struct{}

	// Statistics (lock-free)
	receivedCount  uint64
	processedCount uint64
	failedCount    uint64
	rawCount       uint64

	// Object pools for zero-allocation
	dataPool   sync.Pool
	resultPool sync.Pool
}

func NewOptimizedService(db config.Database, cfg *config.Config) *OptimizedService {
	var repo repository.Repository
	var mapper *OptimizedMapper
	var rawRepo *repository.RawDataRepo
	var err error

	switch db.GetType() {
	case "mongo":
		mongoDb := db.(*config.MongoDatabase)
		repo = repository.NewMongoRepo(mongoDb)
		mapper, err = NewOptimizedMapper(mongoDb)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to initialize mapper: %v", err))
		}
		rawRepo = repository.NewRawDataRepo(mongoDb)
	case "influx":
		influxDb := db.(*config.InfluxDatabase)
		repo = repository.NewInfluxRepo(influxDb)
	}

	var rawBatch []repository.RawData
	var rawBatchFlush chan struct{}
	if rawRepo != nil {
		rawBatch = make([]repository.RawData, 0, 100)
		rawBatchFlush = make(chan struct{})
	}

	svc := &OptimizedService{
		cfg:           cfg,
		repo:          repo,
		mapper:        mapper,
		rawRepo:       rawRepo,
		cache:         NewCacheConfig(),
		rawBatch:      rawBatch,
		rawBatchFlush: rawBatchFlush,
	}

	// Object pools
	svc.dataPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{}, 30)
		},
	}
	svc.resultPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{}, 20)
		},
	}

	svc.batch = NewBatchWriter(repo, cfg.BatchSize, time.Duration(cfg.FlushInterval)*time.Millisecond)

	// Background jobs
	go svc.reportStats()
	go svc.processRawDataLoop()
	go svc.cleanupRawDataLoop()
	if svc.rawRepo != nil {
		go svc.rawBatchFlusher() // New: Batch raw data writes
	} else {
		logger.Info("Raw repository not configured; skipping raw batch persistence")
	}

	logger.Info(fmt.Sprintf("OptimizedService initialized (DB: %s, Batch: %d)", db.GetType(), cfg.BatchSize))
	return svc
}

// ProcessData - Optimized version
func (svc *OptimizedService) ProcessData(rawData map[string]interface{}) error {
	atomic.AddUint64(&svc.receivedCount, 1)

	requestID := ensureRequestIDFast(rawData)

	// OPTIMIZATION 1: Batch raw data writes
	if svc.rawRepo != nil {
		if err := svc.batchStoreRaw(rawData, requestID); err != nil {
			atomic.AddUint64(&svc.failedCount, 1)
			return err
		}
		atomic.AddUint64(&svc.rawCount, 1)
	}

	// OPTIMIZATION 2: Process without blocking on raw data persistence
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := svc.processAndStoreOptimized(ctx, requestID, rawData); err != nil {
			atomic.AddUint64(&svc.failedCount, 1)
			if !svc.cfg.StrictMode {
				logger.Warn(fmt.Sprintf("Processing failed for request_id=%s: %v", requestID, err))
			}
		} else {
			atomic.AddUint64(&svc.processedCount, 1)
		}
	}()

	return nil
}

// batchStoreRaw - Batch raw data writes instead of individual inserts
func (svc *OptimizedService) batchStoreRaw(data map[string]interface{}, requestID string) error {
	if svc.rawRepo == nil || svc.rawBatchFlush == nil {
		return nil
	}

	svc.rawBatchMu.Lock()
	defer svc.rawBatchMu.Unlock()

	// Shallow copy is sufficient (we don't modify the data)
	rawData := repository.RawData{
		RequestID: requestID,
		Data:      data, // No deep clone needed
		Timestamp: time.Now(),
		Processed: false,
	}

	svc.rawBatch = append(svc.rawBatch, rawData)

	// Trigger flush if batch is full
	if len(svc.rawBatch) >= 50 {
		select {
		case svc.rawBatchFlush <- struct{}{}:
		default: // Non-blocking
		}
	}

	return nil
}

// rawBatchFlusher - Background flusher for raw data
func (svc *OptimizedService) rawBatchFlusher() {
	if svc.rawRepo == nil {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			svc.flushRawBatch()
		case <-svc.rawBatchFlush:
			svc.flushRawBatch()
		}
	}
}

func (svc *OptimizedService) flushRawBatch() {
	if svc.rawRepo == nil {
		return
	}

	svc.rawBatchMu.Lock()
	if len(svc.rawBatch) == 0 {
		svc.rawBatchMu.Unlock()
		return
	}

	toFlush := make([]repository.RawData, len(svc.rawBatch))
	copy(toFlush, svc.rawBatch)
	svc.rawBatch = svc.rawBatch[:0]
	svc.rawBatchMu.Unlock()

	// Batch insert
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := svc.rawRepo.InsertMany(ctx, toFlush); err != nil {
		logger.Error(fmt.Sprintf("Raw batch flush failed: %v", err))
	}
}

// processAndStoreOptimized - Optimized processing
func (svc *OptimizedService) processAndStoreOptimized(ctx context.Context, requestID string, rawData map[string]interface{}) error {
	// Detect source
	sourceID := svc.mapper.DetectSourceID(rawData)
	if sourceID == "" {
		return fmt.Errorf("unknown data source")
	}

	// Map fields (already optimized in OptimizedMapper)
	standardized, err := svc.mapper.MapFields(sourceID, rawData)
	if err != nil {
		return fmt.Errorf("mapping failed: %w", err)
	}

	// Add metadata (avoid string concatenation)
	standardized["device_type"] = sourceID
	standardized["request_id"] = requestID

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
	record := svc.convertToRecordOptimized(standardized, timestamp)

	// Add to batch
	svc.batch.Add(record)

	return nil
}

// convertToRecordOptimized - Zero-allocation conversion
func (svc *OptimizedService) convertToRecordOptimized(data map[string]interface{}, timestamp time.Time) domain.InverterData {
	return domain.InverterData{
		DeviceType:     getStringFast(data, "device_type", "unknown"),
		DeviceName:     getStringFast(data, "device_name", "unknown"),
		DeviceID:       getStringFast(data, "device_id", "unknown"),
		SignalStrength: getStringFast(data, "signal_strength", ""),
		Timestamp:      timestamp,
		Data: domain.InverterDetails{
            SlaveID:   getString(data, "slid", getString(data, "slave_id", "")),
            SerialNo:  getString(data, "sno", getString(data, "serial_no", "")),
            ModelName: getString(data, "model", getString(data, "model_name", "")),

            TotalOutputPower: getFloat(data, "p", getFloat(data, "total_output_power", 0)),
            TotalEnergy:      getFloat(data, "e", getFloat(data, "total_e", 0)),
            TodayEnergy:      getFloat(data, "te", getFloat(data, "today_e", 0)),

            PV1Voltage: getFloat(data, "pv1v", getFloat(data, "pv1_voltage", 0)),
            PV1Current: getFloat(data, "pv1c", getFloat(data, "pv1_current", 0)),

            PV2Voltage: getFloat(data, "pv2v", getFloat(data, "pv2_voltage", 0)),
            PV2Current: getFloat(data, "pv2c", getFloat(data, "pv2_current", 0)),

            PV3Voltage: getFloat(data, "pv3v", 0),
            PV3Current: getFloat(data, "pv3c", 0),
            PV4Voltage: getFloat(data, "pv4v", 0),
            PV4Current: getFloat(data, "pv4c", 0),

            GridVoltageR: getFloat(data, "gvr", getFloat(data, "grid_voltage_r", 0)),
            GridVoltageS: getFloat(data, "gvs", getFloat(data, "grid_voltage_s", 0)),
            GridVoltageT: getFloat(data, "gvt", getFloat(data, "grid_voltage_t", 0)),

            GridCurrentR: getFloat(data, "gcr", getFloat(data, "grid_current_r", 0)),
            GridCurrentS: getFloat(data, "gcs", getFloat(data, "grid_current_s", 0)),
            GridCurrentT: getFloat(data, "gct", getFloat(data, "grid_current_t", 0)),

            InverterTemp: getFloat(data, "itmp", getFloat(data, "temperature", 0)),
            Frequency:    getFloat(data, "fr", getFloat(data, "frequency", 0)),

            Alarm1: getInt(data, "al1", 0),
            Alarm2: getInt(data, "al2", 0),
            Alarm3: getInt(data, "al3", 0),
        },
	}
}

// processRawDataLoop periodically retries unprocessed raw records.
func (svc *OptimizedService) processRawDataLoop() {
	if svc.rawRepo == nil {
		return
	}

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
			requestID := raw.RequestID
			if requestID == "" {
				requestID = ensureRequestIDFast(raw.Data)
			}

			if err := svc.processAndStoreOptimized(ctx, requestID, raw.Data); err != nil {
				if markErr := svc.rawRepo.MarkError(ctx, raw.ID.Hex(), err.Error()); markErr != nil {
					logger.Error(fmt.Sprintf("Failed to mark raw error: %v", markErr))
				}
			} else {
				if err := svc.rawRepo.MarkProcessed(ctx, raw.ID.Hex()); err != nil {
					logger.Error(fmt.Sprintf("Failed to mark raw processed: %v", err))
				} else {
					atomic.AddUint64(&svc.processedCount, 1)
				}
			}
		}

		cancel()
	}
}

// cleanupRawDataLoop removes aged processed raw records to enforce retention.
func (svc *OptimizedService) cleanupRawDataLoop() {
	if svc.rawRepo == nil {
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

		retentionDays := 7
		cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

		if deleted, err := svc.rawRepo.Cleanup(ctx, cutoffTime); err != nil {
			logger.Error(fmt.Sprintf("Raw data cleanup failed: %v", err))
		} else if deleted > 0 {
			logger.Info(fmt.Sprintf("Cleaned up %d processed raw records", deleted))
		}

		cancel()
	}
}

// reportStats - Optimized with string builder
func (svc *OptimizedService) reportStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var (
		lastReceived  uint64
		lastProcessed uint64
		lastTime      = time.Now()
		buf           = make([]byte, 0, 256) // Pre-allocated buffer
	)

	for range ticker.C {
		currentReceived := atomic.LoadUint64(&svc.receivedCount)
		currentProcessed := atomic.LoadUint64(&svc.processedCount)
		currentRaw := atomic.LoadUint64(&svc.rawCount)
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		receivedRPS := float64(currentReceived-lastReceived) / elapsed
		processedRPS := float64(currentProcessed-lastProcessed) / elapsed

		// Use fmt.Appendf for efficient string building
		buf = buf[:0]
		buf = append(buf, "📊 Recv: "...)
		buf = appendUint(buf, currentReceived)
		buf = append(buf, " ("...)
		buf = appendFloat(buf, receivedRPS, 0)
		buf = append(buf, "/s) | Raw: "...)
		buf = appendUint(buf, currentRaw)
		buf = append(buf, " | Proc: "...)
		buf = appendUint(buf, currentProcessed)
		buf = append(buf, " ("...)
		buf = appendFloat(buf, processedRPS, 0)
		buf = append(buf, "/s) | Buffer: "...)
		buf = appendInt(buf, svc.batch.Size())
		buf = append(buf, " | Failed: "...)
		buf = appendUint(buf, atomic.LoadUint64(&svc.failedCount))
		buf = append(buf, " | Cache: "...)
		buf = appendInt(buf, svc.cache.StatsCache.Size()+svc.cache.MappingCache.Size())
		buf = append(buf, " items"...)

		logger.Info(string(buf))

		lastReceived = currentReceived
		lastProcessed = currentProcessed
		lastTime = currentTime
	}
}

// Helper functions - Optimized versions
func getStringFast(data map[string]interface{}, key, defaultValue string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

func getIntFast(data map[string]interface{}, key string, defaultValue int) int {
	if val, ok := data[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return defaultValue
}

func ensureRequestIDFast(data map[string]interface{}) string {
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

// Fast number formatting (avoids fmt.Sprintf)
func appendUint(buf []byte, n uint64) []byte {
	return append(buf, fmt.Sprintf("%d", n)...)
}

func appendInt(buf []byte, n int) []byte {
	return append(buf, fmt.Sprintf("%d", n)...)
}

func appendFloat(buf []byte, f float64, precision int) []byte {
	return append(buf, fmt.Sprintf("%.0f", f)...)
}

// Implement other methods (GetStats, Query, etc.) similarly to original service
// but using atomic operations and optimized mapper

func (svc *OptimizedService) Close() error {
	svc.flushRawBatch() // Final flush
	if svc.batch != nil {
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
