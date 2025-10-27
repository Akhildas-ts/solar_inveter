package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"net/http"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"solar_project/constants"
	"solar_project/logger"
	"solar_project/models"
)

var (
	client         *influxdb3.Client
	insertedCount  int64
	failedCount    int64
	batchBuffer    []*influxdb3.Point
	bufferMutex    sync.Mutex
	flushMutex     sync.Mutex
	batchSize      = 100
	faultStats     = make(map[int]int64)
	faultStatMutex sync.Mutex
	isShuttingDown atomic.Bool
)

// InitGenerator initializes the InfluxDB client reference
func InitGenerator(cli *influxdb3.Client) {
	client = cli
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT", 
		"Data receiver initialized with InfluxDB 3.0 client")
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

	// Create InfluxDB 3.0 point
	point := influxdb3.NewPointWithMeasurement("inverter_data").
		SetTag("device_type", payload.DeviceType).
		SetTag("device_name", payload.DeviceName).
		SetTag("device_id", payload.DeviceID).
		SetTag("signal_strength", payload.SignalStrength).
		SetField("serial_no", payload.Data.SerialNo).
		SetField("voltage", int64(payload.Data.S1V)).
		SetField("total_output_power", int64(payload.Data.TotalOutputPower)).
		SetField("temperature", int64(payload.Data.InvTemp)).
		SetField("fault_code", int64(payload.Data.FaultCode)).
		SetField("reading_date", payload.Date).      // ✅ Fixed: renamed from "date"
		SetField("reading_time", payload.Time).      // ✅ Fixed: renamed from "time"
		SetTimestamp(time.Now())

	// Add to buffer (thread-safe)
	bufferMutex.Lock()
	batchBuffer = append(batchBuffer, point)
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

// FlushBatch writes buffered data to InfluxDB (thread-safe)
func FlushBatch(requestID string) {
	flushMutex.Lock()
	defer flushMutex.Unlock()

	bufferMutex.Lock()
	if len(batchBuffer) == 0 {
		bufferMutex.Unlock()
		return
	}
	toInsert := make([]*influxdb3.Point, len(batchBuffer))
	copy(toInsert, batchBuffer)
	batchBuffer = batchBuffer[:0]
	bufferMutex.Unlock()

	logger.WriteLog(constants.LOG_LEVEL_DEBUG, requestID, "FLUSH",
		fmt.Sprintf("Attempting to write %d points to InfluxDB", len(toInsert)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write points to InfluxDB 3.0 with database option
	err := client.WritePoints(ctx, toInsert, influxdb3.WithDatabase("solar_monitoring"))

	if err != nil {
		atomic.AddInt64(&failedCount, int64(len(toInsert)))
		fmt.Printf("❌ WRITE ERROR: %v\n", err) // Print error to console
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "FLUSH",
			fmt.Sprintf("❌ Batch insert FAILED — %d records, Error: %v", len(toInsert), err))
	} else {
		atomic.AddInt64(&insertedCount, int64(len(toInsert)))
		logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "FLUSH",
			fmt.Sprintf("✅ Batch inserted successfully: %d records", len(toInsert)))
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

// GetInsertedCount returns the inserted count
func GetInsertedCount() int64 {
	return atomic.LoadInt64(&insertedCount)
}

// GetFailedCount returns the failed count
func GetFailedCount() int64 {
	return atomic.LoadInt64(&failedCount)
}