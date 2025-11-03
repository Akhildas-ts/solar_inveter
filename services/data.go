package services

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"solar_project/config"
	"solar_project/constants"
	"solar_project/logger"
	"solar_project/models"
)

var (
	insertedCount  int64
	failedCount    int64
	batchBuffer    []interface{}
	bufferMutex    sync.Mutex
	flushMutex     sync.Mutex
	batchSize      = 100
	faultStats     = make(map[int]int64)
	faultStatMutex sync.Mutex
	isShuttingDown atomic.Bool
	dataWriter     DataWriter
	
	// ✅ NEW: MongoDB mapping service reference
	globalMappingServiceRef *MongoMappingService
)

// DataWriter interface for database operations
type DataWriter interface {
	WriteData(data []interface{}, requestID string) error
}

// InverterPayload matches the client's JSON structure
type InverterPayload struct {
	DeviceType     string                 `json:"device_type"`
	DeviceName     string                 `json:"device_name"`
	DeviceID       string                 `json:"device_id"`
	Date           string                 `json:"date"`
	Time           string                 `json:"time"`
	SignalStrength string                 `json:"signal_strength"`
	Data           models.InverterDetails `json:"data"`
}

// InitGenerator initializes the data service based on DB type
func InitGenerator() {
	dbType := config.GetDBType()

	switch dbType {
	case config.MongoDB:
		dataWriter = NewMongoWriter()
	case config.InfluxDB:
		dataWriter = NewInfluxWriter()
	default:
		panic(fmt.Sprintf("Unsupported database type: %s", dbType))
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT",
		fmt.Sprintf("Data receiver initialized with %s", dbType))
}

// ✅ NEW: Set the global mapping service (called from main.go)
func SetGlobalMappingService(service *MongoMappingService) {
	globalMappingServiceRef = service
}

// ✅ NEW: Get the global mapping service
func GetGlobalMappingService() *MongoMappingService {
	return globalMappingServiceRef
}

// FlexibleDataHandler - the main entry point for all incoming data
// Uses MongoDB-backed mapping system instead of JSON file
func FlexibleDataHandler(c *gin.Context) {
	if isShuttingDown.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
		return
	}

	requestID := uuid.New().String()[:16]
	startTime := time.Now()

	// Parse raw JSON (accepts any structure)
	var rawData map[string]interface{}
	if err := c.ShouldBindJSON(&rawData); err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "API",
			fmt.Sprintf("Bad request: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ✅ Get mapping service (MongoDB-backed)
	mappingService := GetGlobalMappingService()
	if mappingService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "mapping service not initialized",
		})
		return
	}

	// Extract or detect source_id
	sourceID := extractSourceID(rawData, mappingService)
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      "could not detect data source",
			"hint":       "add 'source_id' or 'device_type' field",
			"request_id": requestID,
		})
		return
	}

	logger.WriteLog(constants.LOG_LEVEL_DEBUG, requestID, "MAPPING",
		fmt.Sprintf("Using source: %s", sourceID))

	// ✅ Apply field mapping using MongoDB mappings
	standardized, err := mappingService.MapFields(sourceID, rawData)
	if err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "MAPPING",
			fmt.Sprintf("Mapping failed: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      err.Error(),
			"source_id":  sourceID,
			"request_id": requestID,
		})
		return
	}

	// Preserve metadata
	if deviceName, ok := rawData["device_name"].(string); ok {
		standardized["device_name"] = deviceName
	}
	if deviceID, ok := rawData["device_id"].(string); ok {
		standardized["device_id"] = deviceID
	}
	standardized["device_type"] = sourceID

	// Convert to DB format
	var record interface{}
	switch config.GetDBType() {
	case config.MongoDB:
		record = convertStandardizedToMongo(standardized)
	case config.InfluxDB:
		record = convertStandardizedToInflux(standardized)
	}

	// Add to buffer
	bufferMutex.Lock()
	batchBuffer = append(batchBuffer, record)
	currentBufferSize := len(batchBuffer)
	shouldFlush := len(batchBuffer) >= batchSize
	bufferMutex.Unlock()

	// Update fault stats
	if faultCode, ok := standardized["fault_code"].(int); ok && faultCode > 0 {
		updateFaultStats(faultCode)
	}

	// Flush if needed
	if shouldFlush {
		FlushBatch(requestID)
	}

	// Calculate processing time
	processingTime := time.Since(startTime).Milliseconds()

	// Respond
	c.JSON(http.StatusOK, gin.H{
		"status":          "received",
		"request_id":      requestID,
		"source_id":       sourceID,
		"buffer_size":     currentBufferSize,
		"fields_mapped":   len(standardized),
		"processing_ms":   processingTime,
	})
}

// extractSourceID tries to determine the data source
func extractSourceID(data map[string]interface{}, mappingService *MongoMappingService) string {
	// First check explicit fields
	if sid, ok := data["source_id"].(string); ok {
		return sid
	}
	if dtype, ok := data["device_type"].(string); ok {
		return dtype
	}
	
	// ✅ Use MongoDB mapping service to auto-detect
	return mappingService.DetectSourceID(data)
}

// FlushBatch writes buffered data to database (thread-safe)
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

// updateFaultStats increments fault occurrence counters
func updateFaultStats(faultCode int) {
	faultStatMutex.Lock()
	faultStats[faultCode]++
	faultStatMutex.Unlock()
}

// PeriodicBatchFlush flushes every 200ms regardless of batch size
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

// ReportStats prints running performance info every 5 seconds
func ReportStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastCount := int64(0)
	lastTime := time.Now()

	for range ticker.C {
		if isShuttingDown.Load() {
			return
		}

		currentCount := atomic.LoadInt64(&insertedCount)
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		rps := float64(currentCount-lastCount) / elapsed

		faultStatMutex.Lock()
		faultCount := int64(0)
		for _, count := range faultStats {
			faultCount += count
		}
		faultStatMutex.Unlock()

		fmt.Printf("📊 Total=%d | Failed=%d | RPS=%.0f | Faults=%d | Buffer=%d\n",
			currentCount,
			atomic.LoadInt64(&failedCount),
			rps,
			faultCount,
			GetBufferSize(),
		)

		lastCount = currentCount
		lastTime = currentTime
	}
}

// GracefulShutdown ensures all data is flushed before exit
func GracefulShutdown() {
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "SHUTDOWN", "Starting graceful shutdown...")
	isShuttingDown.Store(true)

	time.Sleep(500 * time.Millisecond)

	FlushBatch("SHUTDOWN")

	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "SHUTDOWN",
		fmt.Sprintf("Shutdown complete. Total inserted: %d, Failed: %d",
			GetInsertedCount(), GetFailedCount()))
}

// Helper functions for metrics
func GetBufferSize() int {
	bufferMutex.Lock()
	defer bufferMutex.Unlock()
	return len(batchBuffer)
}

func GetInsertedCount() int64 {
	return atomic.LoadInt64(&insertedCount)
}

func GetFailedCount() int64 {
	return atomic.LoadInt64(&failedCount)
}

// Conversion helper functions
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