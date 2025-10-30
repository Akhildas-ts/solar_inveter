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

// GenerateHandler receives single inverter data per request
func GenerateHandler(c *gin.Context) {
	if isShuttingDown.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
		return
	}

	requestID := uuid.New().String()[:16]

	var payload InverterPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "API",
			fmt.Sprintf("Bad request: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert based on DB type
	var record interface{}
	switch config.GetDBType() {
	case config.MongoDB:
		record = ConvertToMongoRecord(payload)
	case config.InfluxDB:
		record = payload // Will be converted to point during write
	}

	// Add to buffer (thread-safe)
	bufferMutex.Lock()
	batchBuffer = append(batchBuffer, record)
	currentBufferSize := len(batchBuffer)
	shouldFlush := len(batchBuffer) >= batchSize
	bufferMutex.Unlock()

	// Update fault stats
	if payload.Data.FaultCode > 0 {
		updateFaultStats(payload.Data.FaultCode)
	}

	// Flush batch if buffer full
	if shouldFlush {
		FlushBatch(requestID)
	}

	// Respond quickly
	c.JSON(http.StatusOK, gin.H{
		"status":      "received",
		"request_id":  requestID,
		"buffer_size": currentBufferSize,
	})
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

		fmt.Printf(" Total=%d | Failed=%d | RPS=%.0f | Faults=%d | Buffer=%d\n",
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

// Helper for metrics
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