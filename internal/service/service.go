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
)

// Service handles business logic
type Service struct {
	cfg    *config.Config
	repo   repository.Repository
	mapper *Mapper
	batch  *BatchWriter

	// Statistics
	receivedCount  int64
	processedCount int64
	failedCount    int64
}

// NewService creates a new service instance
func NewService(db config.Database, cfg *config.Config) *Service {
	var repo repository.Repository
	var mapper *Mapper
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
	case "influx":
		influxDb := db.(*config.InfluxDatabase)
		repo = repository.NewInfluxRepo(influxDb)
		// For InfluxDB, we still need MongoDB for mappings
		// In production, you'd pass a separate MongoDB connection here
	}

	svc := &Service{
		cfg:    cfg,
		repo:   repo,
		mapper: mapper,
	}

	// Initialize batch writer
	svc.batch = NewBatchWriter(repo, cfg.BatchSize, time.Duration(cfg.FlushInterval)*time.Millisecond)

	// Start statistics reporter
	go svc.reportStats()

	logger.Info(fmt.Sprintf("Service initialized (DB: %s, Batch: %d)", db.GetType(), cfg.BatchSize))
	return svc
}

// ProcessData handles incoming data
func (svc *Service) ProcessData(rawData map[string]interface{}) error {
	atomic.AddInt64(&svc.receivedCount, 1)

	// Detect source
	sourceID := svc.mapper.DetectSourceID(rawData)
	if sourceID == "" {
		atomic.AddInt64(&svc.failedCount, 1)
		if svc.cfg.StrictMode {
			return fmt.Errorf("unknown data source")
		}
		logger.Warn("Unknown data source, skipping")
		return nil
	}

	// Map fields
	standardized, err := svc.mapper.MapFields(sourceID, rawData)
	if err != nil {
		atomic.AddInt64(&svc.failedCount, 1)
		if svc.cfg.StrictMode {
			return fmt.Errorf("mapping failed: %w", err)
		}
		logger.Error(fmt.Sprintf("Mapping failed: %v", err))
		return nil
	}

	// Add metadata
	standardized["device_type"] = sourceID
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
	atomic.AddInt64(&svc.processedCount, 1)

	return nil
}

// GetStats returns current statistics
func (svc *Service) GetStats(ctx context.Context) (*domain.Stats, error) {
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

	return &domain.Stats{
		TotalRecords:  total,
		NormalRecords: total - faults,
		FaultRecords:  faults,
		InsertedCount: inserted,
		FailedCount:   failed,
		BufferSize:    svc.batch.Size(),
		SuccessRate:   successRate,
		DatabaseType:  svc.repo.Type(),
	}, nil
}

// Query retrieves records based on filter
func (svc *Service) Query(ctx context.Context, filter domain.QueryFilter) ([]domain.InverterData, error) {
	return svc.repo.Query(ctx, filter)
}

// Mapper operations
func (svc *Service) GetMappings() map[string]*domain.DataSourceMapping {
	if svc.mapper == nil {
		return make(map[string]*domain.DataSourceMapping)
	}
	return svc.mapper.GetAll()
}

func (svc *Service) CreateMapping(mapping *domain.DataSourceMapping) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	return svc.mapper.Create(mapping)
}

func (svc *Service) UpdateMapping(sourceID string, mapping *domain.DataSourceMapping) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	return svc.mapper.Update(sourceID, mapping)
}

func (svc *Service) DeleteMapping(sourceID string) error {
	if svc.mapper == nil {
		return fmt.Errorf("mapper not initialized")
	}
	return svc.mapper.Delete(sourceID)
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
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		receivedRPS := float64(currentReceived-lastReceived) / elapsed
		processedRPS := float64(currentProcessed-lastProcessed) / elapsed

		logger.Info(fmt.Sprintf("📊 Recv: %d (%.0f/s) | Proc: %d (%.0f/s) | Buffer: %d | Failed: %d",
			currentReceived, receivedRPS,
			currentProcessed, processedRPS,
			svc.batch.Size(),
			atomic.LoadInt64(&svc.failedCount)))

		lastReceived = currentReceived
		lastProcessed = currentProcessed
		lastTime = currentTime
	}
}

func (svc *Service) Close() error {
	if svc.batch != nil {
		svc.batch.Flush()
		svc.batch.Close()
	}
	if svc.mapper != nil {
		svc.mapper.Close()
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
