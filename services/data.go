package services

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"solar_project/config"
	"solar_project/constants"
	"solar_project/logger"
	"solar_project/models"
)

var (
	// Counters
	receivedCount      int64
	rawStoredCount     int64
	processedCount     int64
	insertedCount      int64
	failedCount        int64
	mappingFailCount   int64
	unknownSourceCount int64 // NEW: Track unknown sources
	
	// Buffers
	batchBuffer    []interface{}
	bufferMutex    sync.Mutex
	flushMutex     sync.Mutex
	batchSize      = 100
	
	// Collections
	dataWriter       DataWriter
	rawCollection    *mongo.Collection
	
	// Settings
	isShuttingDown   atomic.Bool
	
	// Stats
	faultStats       = make(map[int]int64)
	faultStatMutex   sync.Mutex
	
	// Mapping service
	globalMappingServiceRef *MongoMappingService
	
	// Auto-cleanup settings
	rawDataRetentionDays = 7
	cleanupInterval      = 24 * time.Hour
)

type DataWriter interface {
	WriteData(data []interface{}, requestID string) error
}

type InverterPayload struct {
	DeviceType     string                 `json:"device_type"`
	DeviceName     string                 `json:"device_name"`
	DeviceID       string                 `json:"device_id"`
	Date           string                 `json:"date"`
	Time           string                 `json:"time"`
	SignalStrength string                 `json:"signal_strength"`
	Data           models.InverterDetails `json:"data"`
}

type RawDataRecord struct {
	RequestID      string                 `bson:"request_id"`
	RawData        map[string]interface{} `bson:"raw_data"`
	ReceivedAt     time.Time              `bson:"received_at"`
	ProcessedAt    *time.Time             `bson:"processed_at,omitempty"`
	Processed      bool                   `bson:"processed"`
	SourceID       string                 `bson:"source_id,omitempty"`
	Error          string                 `bson:"error,omitempty"`
	MappingError   string                 `bson:"mapping_error,omitempty"`
	UnknownSource  bool                   `bson:"unknown_source"` // NEW: Flag for unknown sources
	NeedsAttention bool                   `bson:"needs_attention"` // NEW: Flag for manual review
}

func InitGenerator() {
	dbType := config.GetDBType()

	switch dbType {
	case config.MongoDB:
		dataWriter = NewMongoWriter()
		
		client := config.GetMongoClient()
		if client == nil {
			panic("MongoDB client not initialized")
		}
		
		dbName := getEnv("DB_NAME", "solar_monitoring")
		rawCollection = client.Database(dbName).Collection("raw_data")
		
		createRawDataIndexes()
		go autoCleanupRawData()
		
		logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT", 
			"Raw data collection initialized with auto-cleanup")
		
	case config.InfluxDB:
		dataWriter = NewInfluxWriter()
	default:
		panic(fmt.Sprintf("Unsupported database type: %s", dbType))
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT",
		fmt.Sprintf("Data receiver initialized with %s", dbType))
}

func createRawDataIndexes() {
	if rawCollection == nil {
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "request_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "received_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "processed", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "unknown_source", Value: 1}}, // NEW
		},
		{
			Keys: bson.D{{Key: "needs_attention", Value: 1}}, // NEW
		},
		{
			Keys: bson.D{
				{Key: "received_at", Value: 1},
				{Key: "processed", Value: 1},
			},
		},
	}

	_, err := rawCollection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, "", "INDEX",
			fmt.Sprintf("Failed to create indexes: %v", err))
	}
}

func autoCleanupRawData() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	cleanupOldRawData()

	for range ticker.C {
		if isShuttingDown.Load() {
			return
		}
		cleanupOldRawData()
	}
}

func cleanupOldRawData() {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cutoffDate := time.Now().Add(-time.Duration(rawDataRetentionDays) * 24 * time.Hour)

	filter := bson.M{
		"received_at": bson.M{"$lt": cutoffDate},
		"processed":   true,
		"unknown_source": false, // ✅ DON'T delete unknown sources automatically
	}

	result, err := rawCollection.DeleteMany(ctx, filter)
	if err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, "", "CLEANUP",
			fmt.Sprintf("Failed to cleanup raw data: %v", err))
		return
	}

	if result.DeletedCount > 0 {
		logger.WriteLog(constants.LOG_LEVEL_INFO, "", "CLEANUP",
			fmt.Sprintf("Cleaned up %d old raw records (older than %d days)",
				result.DeletedCount, rawDataRetentionDays))
	}
}

func SetGlobalMappingService(service *MongoMappingService) {
	globalMappingServiceRef = service
}

func GetGlobalMappingService() *MongoMappingService {
	return globalMappingServiceRef
}

// ✅ FIXED: Main handler with proper error handling
func FlexibleDataHandler(c *gin.Context) {
	if isShuttingDown.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
		return
	}

	requestID := uuid.New().String()[:16]
	startTime := time.Now()
	
	atomic.AddInt64(&receivedCount, 1)

	var rawData map[string]interface{}
	if err := c.ShouldBindJSON(&rawData); err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "API",
			fmt.Sprintf("Bad request: %v", err))
		atomic.AddInt64(&failedCount, 1)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ✅ STEP 1: Store raw data IMMEDIATELY (always succeeds)
	go storeRawDataAsync(requestID, rawData)

	// ✅ STEP 2: Check if mapping service exists
	mappingService := GetGlobalMappingService()
	if mappingService == nil {
		go markAsUnknownSource(requestID, "mapping service not initialized")
		c.JSON(http.StatusOK, gin.H{
			"status":      "raw_stored",
			"request_id":  requestID,
			"message":     "Data stored but cannot process (mapping service unavailable)",
			"raw_stored":  true,
			"processed":   false,
		})
		return
	}

	// ✅ STEP 3: Try to detect source
	sourceID := extractSourceID(rawData, mappingService)
	if sourceID == "" {
		// ✅ NO DEFAULT MAPPING - Mark as unknown and store raw only
		atomic.AddInt64(&unknownSourceCount, 1)
		go markAsUnknownSource(requestID, "no matching mapping found")
		
		c.JSON(http.StatusOK, gin.H{
			"status":         "unknown_source",
			"request_id":     requestID,
			"message":        "Data stored but source unknown - needs mapping configuration",
			"raw_stored":     true,
			"processed":      false,
			"needs_attention": true,
			"hint":           "Check /api/raw/unknown to see all unmapped data",
		})
		return
	}

	go updateRawDataSource(requestID, sourceID)

	// ✅ STEP 4: Try to apply mapping
	standardized, err := mappingService.MapFields(sourceID, rawData)
	if err != nil {
		atomic.AddInt64(&mappingFailCount, 1)
		go updateRawDataError(requestID, fmt.Sprintf("mapping failed: %v", err))
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "MAPPING",
			fmt.Sprintf("Mapping failed: %v", err))
		
		c.JSON(http.StatusOK, gin.H{
			"status":      "mapping_failed",
			"request_id":  requestID,
			"source_id":   sourceID,
			"message":     "Data stored but mapping failed",
			"raw_stored":  true,
			"processed":   false,
			"error":       err.Error(),
		})
		return
	}

	// ✅ STEP 5: Successfully mapped - prepare for insertion
	if deviceName, ok := rawData["device_name"].(string); ok {
		standardized["device_name"] = deviceName
	}
	if deviceID, ok := rawData["device_id"].(string); ok {
		standardized["device_id"] = deviceID
	}
	standardized["device_type"] = sourceID
	standardized["request_id"] = requestID

	var record interface{}
	switch config.GetDBType() {
	case config.MongoDB:
		record = convertStandardizedToMongo(standardized)
	case config.InfluxDB:
		record = convertStandardizedToInflux(standardized)
	}

	// ✅ CRITICAL FIX: Add to buffer properly
	bufferMutex.Lock()
	batchBuffer = append(batchBuffer, record)
	currentBufferSize := len(batchBuffer)
	shouldFlush := len(batchBuffer) >= batchSize
	bufferMutex.Unlock()

	if faultCode, ok := standardized["fault_code"].(int); ok && faultCode > 0 {
		updateFaultStats(faultCode)
	}

	if shouldFlush {
		FlushBatch(requestID)
	}

	atomic.AddInt64(&processedCount, 1)

	processingTime := time.Since(startTime).Milliseconds()

	c.JSON(http.StatusOK, gin.H{
		"status":          "success",
		"request_id":      requestID,
		"source_id":       sourceID,
		"buffer_size":     currentBufferSize,
		"fields_mapped":   len(standardized),
		"processing_ms":   processingTime,
		"raw_stored":      true,
		"processed":       true,
	})
}

// ✅ Store raw data (always succeeds)
func storeRawDataAsync(requestID string, data map[string]interface{}) {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rawDoc := RawDataRecord{
		RequestID:      requestID,
		RawData:        data,
		ReceivedAt:     time.Now(),
		Processed:      false,
		UnknownSource:  false,
		NeedsAttention: false,
	}

	_, err := rawCollection.InsertOne(ctx, rawDoc)
	if err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "RAW_STORE",
			fmt.Sprintf("Failed to store raw data: %v", err))
		return
	}

	atomic.AddInt64(&rawStoredCount, 1)
}

// ✅ NEW: Mark as unknown source (no mapping available)
func markAsUnknownSource(requestID, reason string) {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	filter := bson.M{"request_id": requestID}
	update := bson.M{"$set": bson.M{
		"unknown_source":  true,
		"needs_attention": true,
		"mapping_error":   reason,
		"processed":       false,
	}}

	rawCollection.UpdateOne(ctx, filter, update)
}

func updateRawDataSource(requestID, sourceID string) {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	filter := bson.M{"request_id": requestID}
	update := bson.M{"$set": bson.M{"source_id": sourceID}}

	rawCollection.UpdateOne(ctx, filter, update)
}

func updateRawDataError(requestID, errorMsg string) {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	filter := bson.M{"request_id": requestID}
	update := bson.M{"$set": bson.M{
		"mapping_error":   errorMsg,
		"needs_attention": true,
		"processed":       false,
	}}

	rawCollection.UpdateOne(ctx, filter, update)
}

func markRawAsProcessed(requestID string) {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	now := time.Now()
	filter := bson.M{"request_id": requestID}
	update := bson.M{"$set": bson.M{
		"processed":    true,
		"processed_at": now,
	}}

	rawCollection.UpdateOne(ctx, filter, update)
}

// ✅ CRITICAL FIX: Proper batch flushing
func FlushBatch(requestID string) {
	flushMutex.Lock()
	defer flushMutex.Unlock()

	bufferMutex.Lock()
	if len(batchBuffer) == 0 {
		bufferMutex.Unlock()
		return
	}
	toInsert := make([]interface{}, len(batchBuffer))
	copy(toInsert, batchBuffer)
	batchBuffer = batchBuffer[:0] // ✅ Clear buffer properly
	bufferMutex.Unlock()

	err := dataWriter.WriteData(toInsert, requestID)

	if err != nil {
		atomic.AddInt64(&failedCount, int64(len(toInsert)))
	} else {
		atomic.AddInt64(&insertedCount, int64(len(toInsert)))
		// Mark as processed
		go markRawAsProcessed(requestID)
	}
}

func extractSourceID(data map[string]interface{}, mappingService *MongoMappingService) string {
	if sid, ok := data["source_id"].(string); ok {
		return sid
	}
	if dtype, ok := data["device_type"].(string); ok {
		return dtype
	}
	return mappingService.DetectSourceID(data)
}

func updateFaultStats(faultCode int) {
	faultStatMutex.Lock()
	faultStats[faultCode]++
	faultStatMutex.Unlock()
}

func GetBufferSize() int {
	bufferMutex.Lock()
	defer bufferMutex.Unlock()
	return len(batchBuffer)
}

func GetInsertedCount() int64    { return atomic.LoadInt64(&insertedCount) }
func GetFailedCount() int64      { return atomic.LoadInt64(&failedCount) }
func GetReceivedCount() int64    { return atomic.LoadInt64(&receivedCount) }
func GetRawStoredCount() int64   { return atomic.LoadInt64(&rawStoredCount) }
func GetProcessedCount() int64   { return atomic.LoadInt64(&processedCount) }
func GetUnknownSourceCount() int64 { return atomic.LoadInt64(&unknownSourceCount) }

func PeriodicBatchFlush() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if isShuttingDown.Load() {
			FlushBatch("PERIODIC_FINAL")
			return
		}
		FlushBatch("PERIODIC")
	}
}

// ✅ IMPROVED: Better stats reporting
func ReportStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastReceived := int64(0)
	lastInserted := int64(0)
	lastTime := time.Now()

	for range ticker.C {
		if isShuttingDown.Load() {
			return
		}

		currentReceived := atomic.LoadInt64(&receivedCount)
		currentInserted := atomic.LoadInt64(&insertedCount)
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		receivedRPS := float64(currentReceived-lastReceived) / elapsed
		insertedRPS := float64(currentInserted-lastInserted) / elapsed

		faultStatMutex.Lock()
		faultCount := int64(0)
		for _, count := range faultStats {
			faultCount += count
		}
		faultStatMutex.Unlock()

		fmt.Printf("📊 Recv=%d (%.0f/s) | Raw=%d | Proc=%d | Insert=%d (%.0f/s) | Unknown=%d | Failed=%d | Buffer=%d\n",
			currentReceived,
			receivedRPS,
			atomic.LoadInt64(&rawStoredCount),
			atomic.LoadInt64(&processedCount),
			currentInserted,
			insertedRPS,
			atomic.LoadInt64(&unknownSourceCount),
			atomic.LoadInt64(&failedCount),
			GetBufferSize(),
		)

		lastReceived = currentReceived
		lastInserted = currentInserted
		lastTime = currentTime
	}
}

func GracefulShutdown() {
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "SHUTDOWN", "Starting graceful shutdown...")
	isShuttingDown.Store(true)
	time.Sleep(500 * time.Millisecond)
	FlushBatch("SHUTDOWN")
	
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "SHUTDOWN",
		fmt.Sprintf("Shutdown complete. Recv: %d, Raw: %d, Proc: %d, Insert: %d, Unknown: %d, Failed: %d",
			GetReceivedCount(), GetRawStoredCount(), GetProcessedCount(),
			GetInsertedCount(), GetUnknownSourceCount(), GetFailedCount()))
}

func convertStandardizedToMongo(data map[string]interface{}) interface{} {
	return models.InverterData{
		DeviceType: getStringOrDefault(data, "device_type", "unknown"),
		DeviceName: getStringOrDefault(data, "device_name", "unknown"),
		DeviceID:   getStringOrDefault(data, "device_id", "unknown"),
		Timestamp:  time.Now(),
		Data: models.InverterDetails{
			SerialNo:         getStringOrDefault(data, "serial_no", "UNKNOWN"),
			S1V:              getIntOrDefault(data, "voltage", 0),
			TotalOutputPower: getIntOrDefault(data, "power", 0),
			InvTemp:          getIntOrDefault(data, "temperature", 0),
			FaultCode:        getIntOrDefault(data, "fault_code", 0),
		},
	}
}

func convertStandardizedToInflux(data map[string]interface{}) InverterPayload {
	return InverterPayload{
		DeviceType: getStringOrDefault(data, "device_type", "unknown"),
		DeviceName: getStringOrDefault(data, "device_name", "unknown"),
		DeviceID:   getStringOrDefault(data, "device_id", "unknown"),
		Data: models.InverterDetails{
			SerialNo:         getStringOrDefault(data, "serial_no", "UNKNOWN"),
			S1V:              getIntOrDefault(data, "voltage", 0),
			TotalOutputPower: getIntOrDefault(data, "power", 0),
			InvTemp:          getIntOrDefault(data, "temperature", 0),
			FaultCode:        getIntOrDefault(data, "fault_code", 0),
		},
	}
}

func getStringOrDefault(data map[string]interface{}, key string, defaultValue string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return defaultValue
}

func getIntOrDefault(data map[string]interface{}, key string, defaultValue int) int {
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

// func getEnv(key, defaultValue string) string {
// 	if value := os.Getenv(key); value != "" {
// 		return value
// 	}
// 	return defaultValue
// }