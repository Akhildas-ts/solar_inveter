package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
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
	unknownSourceCount int64

	// Buffers
	batchBuffer []interface{}
	bufferMutex sync.Mutex
	flushMutex  sync.Mutex
	batchSize   = 100 // reason 100 - 50, ctx time at influx 10-30 second

	// Collections
	dataWriter    DataWriter
	rawCollection *mongo.Collection

	// Settings
	isShuttingDown    atomic.Bool
	strictMappingMode bool // ✅ NEW: Strict mode flag

	// Stats
	faultStats     = make(map[int]int64)
	faultStatMutex sync.Mutex

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
	UnknownSource  bool                   `bson:"unknown_source"`
	NeedsAttention bool                   `bson:"needs_attention"`
}

func InitGenerator() {
	// ✅ Load strict mapping mode setting
	strictMappingMode = getEnvBool("STRICT_MAPPING_MODE", true)

	if strictMappingMode {
		logger.WriteLog(constants.LOG_LEVEL_WARNING, "", "INIT",
			"⚠️  STRICT MAPPING MODE ENABLED - Program will STOP if unmapped data is received")
	}

	dbType := config.GetDBType()

	switch dbType {
	case config.MongoDB:
		dataWriter = NewMongoWriter()
		batchSize = 500

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
		batchSize = 1000
		dataWriter = NewInfluxWriter()
	default:
		panic(fmt.Sprintf("Unsupported database type: %s", dbType))
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT",
		fmt.Sprintf("Data receiver initialized with %s (Strict Mode: %v)", dbType, strictMappingMode))
}

func createRawDataIndexes() {
	if rawCollection == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "request_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "received_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "processed", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "unknown_source", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "needs_attention", Value: 1}},
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
		"received_at":    bson.M{"$lt": cutoffDate},
		"processed":      true,
		"unknown_source": false,
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

// ✅ UPDATED: Main handler with STRICT MODE
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

	// ✅ STEP 1: Store raw data (only if not in strict mode or for logging)
	if !strictMappingMode {
		go storeRawDataAsync(requestID, rawData)
	}

	// ✅ STEP 2: Check if mapping service exists
	mappingService := GetGlobalMappingService()
	if mappingService == nil {
		errorMsg := "Mapping service not initialized"
		logger.WriteLog(constants.LOG_LEVEL_FATAL, requestID, "MAPPING",
			"🚨 FATAL: "+errorMsg)

		if strictMappingMode {
			// ✅ STOP THE PROGRAM
			fmt.Printf("\n\n🚨 FATAL ERROR: %s\n", errorMsg)
			fmt.Println("🛑 STRICT_MAPPING_MODE=true - Shutting down server...")
			os.Exit(1)
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       errorMsg,
			"strict_mode": strictMappingMode,
		})
		return
	}

	// ✅ STEP 3: Try to detect source
	sourceID := extractSourceID(rawData, mappingService)
	if sourceID == "" {
		errorMsg := "No mapping found for this data format"
		atomic.AddInt64(&unknownSourceCount, 1)

		logger.WriteLog(constants.LOG_LEVEL_FATAL, requestID, "MAPPING",
			fmt.Sprintf("🚨 FATAL: %s | Data: %+v", errorMsg, rawData))

		if strictMappingMode {
			// ✅ STOP THE PROGRAM
			fmt.Printf("\n\n🚨 FATAL ERROR: %s\n", errorMsg)
			fmt.Println("📄 Sample data:")
			fmt.Printf("%+v\n\n", rawData)
			fmt.Println("💡 Available mappings:")
			for sid := range mappingService.GetAllMappings() {
				fmt.Printf("   - %s\n", sid)
			}
			fmt.Println("\n🛑 STRICT_MAPPING_MODE=true - Shutting down server...")
			fmt.Println("ℹ️  Fix: Create mapping via POST /api/mappings or set STRICT_MAPPING_MODE=false\n")
			os.Exit(1)
		}

		// Non-strict mode: store and continue
		go markAsUnknownSource(requestID, errorMsg)
		c.JSON(http.StatusOK, gin.H{
			"status":          "unknown_source",
			"request_id":      requestID,
			"message":         errorMsg,
			"strict_mode":     false,
			"needs_attention": true,
		})
		return
	}

	if !strictMappingMode {
		go updateRawDataSource(requestID, sourceID)
	}

	// ✅ STEP 4: Try to apply mapping
	standardized, err := mappingService.MapFields(sourceID, rawData)
	if err != nil {
		atomic.AddInt64(&mappingFailCount, 1)
		errorMsg := fmt.Sprintf("Mapping failed: %v", err)

		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "MAPPING", errorMsg)

		if strictMappingMode {
			// ✅ STOP THE PROGRAM
			fmt.Printf("\n\n🚨 FATAL ERROR: %s\n", errorMsg)
			fmt.Printf("📄 Source ID: %s\n", sourceID)
			fmt.Printf("📄 Data: %+v\n\n", rawData)
			fmt.Println("🛑 STRICT_MAPPING_MODE=true - Shutting down server...")
			fmt.Println("ℹ️  Fix: Update mapping via PUT /api/mappings/:source_id or set STRICT_MAPPING_MODE=false\n")
			os.Exit(1)
		}

		// Non-strict mode: store error and continue
		go updateRawDataError(requestID, errorMsg)
		c.JSON(http.StatusOK, gin.H{
			"status":      "mapping_failed",
			"request_id":  requestID,
			"source_id":   sourceID,
			"error":       err.Error(),
			"strict_mode": false,
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

	// ✅ Add to buffer
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

	if !strictMappingMode {
		go markRawAsProcessed(requestID)
	}

	processingTime := time.Since(startTime).Milliseconds()

	c.JSON(http.StatusOK, gin.H{
		"status":        "success",
		"request_id":    requestID,
		"source_id":     sourceID,
		"buffer_size":   currentBufferSize,
		"fields_mapped": len(standardized),
		"processing_ms": processingTime,
		"strict_mode":   strictMappingMode,
	})
}

// ✅ Store raw data (only in non-strict mode)
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
	batchBuffer = batchBuffer[:0]
	bufferMutex.Unlock()

	err := dataWriter.WriteData(toInsert, requestID)

	if err != nil {
		atomic.AddInt64(&failedCount, int64(len(toInsert)))
	} else {
		atomic.AddInt64(&insertedCount, int64(len(toInsert)))
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

func GetInsertedCount() int64      { return atomic.LoadInt64(&insertedCount) }
func GetFailedCount() int64        { return atomic.LoadInt64(&failedCount) }
func GetReceivedCount() int64      { return atomic.LoadInt64(&receivedCount) }
func GetRawStoredCount() int64     { return atomic.LoadInt64(&rawStoredCount) }
func GetProcessedCount() int64     { return atomic.LoadInt64(&processedCount) }
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

		modeIndicator := ""
		if strictMappingMode {
			modeIndicator = " [STRICT]"
		}

		fmt.Printf("📊%s Recv=%d (%.0f/s) | Proc=%d | Insert=%d (%.0f/s) | Failed=%d | Buffer=%d\n",
			modeIndicator,
			currentReceived,
			receivedRPS,
			atomic.LoadInt64(&processedCount),
			currentInserted,
			insertedRPS,
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
		fmt.Sprintf("Shutdown complete. Recv: %d, Proc: %d, Insert: %d, Failed: %d",
			GetReceivedCount(), GetProcessedCount(),
			GetInsertedCount(), GetFailedCount()))
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

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	result, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return result
}
