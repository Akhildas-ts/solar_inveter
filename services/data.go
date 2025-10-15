package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"solar_project/logger"
	"solar_project/constants"
	"solar_project/models"
	"github.com/google/uuid"

	"net/http"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"github.com/gin-gonic/gin"
)

var (
	collection     *mongo.Collection
	insertedCount  int64
	failedCount    int64
	batchBuffer    []interface{}
	bufferMutex    sync.Mutex
	flushMutex     sync.Mutex  // ← NEW: Prevent concurrent flushes
	batchSize      = 100
	faultStats     = make(map[int]int64)
	faultStatMutex sync.Mutex
	isShuttingDown atomic.Bool  // ← NEW: Graceful shutdown flag
)

// InitGenerator initializes the Mongo collection reference
func InitGenerator(coll *mongo.Collection) {
	collection = coll
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT", "Data receiver initialized with MongoDB collection")
}

// InverterPayload matches the client's JSON structure
type InverterPayload struct {
	DeviceType     string             `json:"device_type"`
	DeviceName     string             `json:"device_name"`
	DeviceID       string             `json:"device_id"`
	Date           string             `json:"date"`
	Time           string             `json:"time"`
	SignalStrength string             `json:"signal_strength"`
	Data           models.InverterDetails `json:"data"`
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

	// Convert to full model
	record := models.InverterData{
		DeviceType:     payload.DeviceType,
		DeviceName:     payload.DeviceName,
		DeviceID:       payload.DeviceID,
		Date:           payload.Date,
		Time:           payload.Time,
		Timestamp:      time.Now(), //client time_ date
		SignalStrength: payload.SignalStrength,
		Data:           payload.Data,
	}

	// Add to buffer (thread-safe)
	bufferMutex.Lock()
	batchBuffer = append(batchBuffer, record)
	currentBufferSize := len(batchBuffer)
	shouldFlush := len(batchBuffer) >= batchSize
	bufferMutex.Unlock()

	// Update fault stats
	if record.Data.FaultCode > 0 {
		updateFaultStats(record.Data.FaultCode)
		// logger.WriteLog(constants.LOG_LEVEL_DEBUG, requestID, "FAULT",
		// 	fmt.Sprintf("Received fault code %d for device %s",
		// 		record.Data.FaultCode, record.DeviceID))
	}

	// Flush batch if buffer full (synchronously to prevent race)
	if shouldFlush {
		FlushBatch(requestID)  // ← CHANGED: Synchronous call
	}

	// Respond quickly
	c.JSON(http.StatusOK, gin.H{
		"status":      "received",
		"request_id":  requestID,
		"buffer_size": currentBufferSize,
	})
}

// FlushBatch writes buffered data to MongoDB (thread-safe)
func FlushBatch(requestID string) {
	// Prevent concurrent flushes
	flushMutex.Lock()
	defer flushMutex.Unlock()

	bufferMutex.Lock()
	if len(batchBuffer) == 0 {
		bufferMutex.Unlock()
		return
	}
	toInsert := make([]interface{}, len(batchBuffer))
	copy(toInsert, batchBuffer)
	batchBuffer = batchBuffer[:0]  // Clear buffer
	bufferMutex.Unlock()

	// logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "FLUSH",
	// 	fmt.Sprintf("Flushing %d records to MongoDB...", len(toInsert)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)  // ← Increased timeout
	defer cancel()

	opts := options.InsertMany().SetOrdered(false)
	result, err := collection.InsertMany(ctx, toInsert, opts)

	if err != nil {
		inserted := 0
		if result != nil {
			inserted = len(result.InsertedIDs)
		}
		failed := len(toInsert) - inserted
		atomic.AddInt64(&insertedCount, int64(inserted))
		atomic.AddInt64(&failedCount, int64(failed))
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "FLUSH",
			fmt.Sprintf("Batch insert failed — Inserted: %d, Failed: %d, Error: %v",
				inserted, failed, err))
	} else {
		atomic.AddInt64(&insertedCount, int64(len(toInsert)))
		logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "FLUSH",
			fmt.Sprintf("Batch inserted successfully: %d records", len(toInsert)))
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
	
	// Give in-flight requests time to complete
	time.Sleep(500 * time.Millisecond)
	
	// Final flush
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

// GetInsertedCount returns the inserted count
func GetInsertedCount() int64 {
	return atomic.LoadInt64(&insertedCount)
}

// GetFailedCount returns the failed count
func GetFailedCount() int64 {
	return atomic.LoadInt64(&failedCount)
}