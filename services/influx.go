package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"

	"solar_project/config"
	"solar_project/constants"
	"solar_project/logger"
	"solar_project/models"
)

type InfluxWriter struct {
	client     *influxdb3.Client
	database   string
	writeQueue chan []*influxdb3.Point
	workers    int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	writesSucceeded int64
	writesFailed    int64
	mu              sync.Mutex
}

func NewInfluxWriter() *InfluxWriter {
	ctx, cancel := context.WithCancel(context.Background())

	writer := &InfluxWriter{
		client:     config.GetInfluxClient(),
		database:   config.GetInfluxDatabase(),
		writeQueue: make(chan []*influxdb3.Point, 100),
		workers:    4,
		ctx:        ctx,
		cancel:     cancel,
	}

	for i := 0; i < writer.workers; i++ {
		writer.wg.Add(1)
		go writer.writeWorker(i)
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INFLUX_INIT",
		fmt.Sprintf("✅ Started %d InfluxDB v3 Core write workers", writer.workers))

	return writer
}

func (w *InfluxWriter) writeWorker(workerID int) {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return
		case points, ok := <-w.writeQueue:
			if !ok {
				return
			}

			err := w.writeWithRetry(points, workerID)

			w.mu.Lock()
			if err != nil {
				w.writesFailed += int64(len(points))
			} else {
				w.writesSucceeded += int64(len(points))
			}
			w.mu.Unlock()
		}
	}
}

func (w *InfluxWriter) writeWithRetry(points []*influxdb3.Point, workerID int) error {
	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		// ✅ CRITICAL: Use 30-second timeout for InfluxDB v3 Core
		ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
		
		// ✅ FIXED: Use the correct v3 API method
		err := w.client.WritePoints(ctx, points)
		cancel()

		if err == nil {
			if attempt > 0 {
				logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INFLUX_RETRY",
					fmt.Sprintf("✅ Worker %d: Retry succeeded on attempt %d (%d points)",
						workerID, attempt+1, len(points)))
			}
			return nil
		}

		// Log specific error
		logger.WriteLog(constants.LOG_LEVEL_WARNING, "", "INFLUX_RETRY",
			fmt.Sprintf("⚠️  Worker %d: Attempt %d/%d failed for %d points. Error: %v",
				workerID, attempt+1, maxRetries, len(points), err))

		if attempt < maxRetries-1 {
			delay := baseDelay * time.Duration(1<<attempt)
			time.Sleep(delay)
		}
	}

	logger.WriteLog(constants.LOG_LEVEL_ERROR, "", "INFLUX_WRITE",
		fmt.Sprintf("❌ Worker %d: All %d attempts failed for %d points",
			workerID, maxRetries, len(points)))
	return fmt.Errorf("max retries exceeded")
}

func (w *InfluxWriter) WriteData(data []interface{}, requestID string) error {
	if len(data) == 0 {
		return nil
	}

	if w.client == nil {
		return fmt.Errorf("InfluxDB client not initialized")
	}

	points := make([]*influxdb3.Point, 0, len(data))
	for _, item := range data {
		payload, ok := item.(InverterPayload)
		if !ok {
			logger.WriteLog(constants.LOG_LEVEL_WARNING, requestID, "INFLUX_CONVERT",
				"Failed to convert item to InverterPayload")
			continue
		}

		point := convertToInfluxPoint(payload)
		if point != nil {
			points = append(points, point)
		}
	}

	if len(points) == 0 {
		return nil
	}

	select {
	case w.writeQueue <- points:
		return nil
	case <-time.After(5 * time.Second):
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "INFLUX_QUEUE",
			fmt.Sprintf("❌ Write queue full! Dropping %d points", len(points)))
		return fmt.Errorf("write queue full")
	}
}

func (w *InfluxWriter) Close() {
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INFLUX_SHUTDOWN",
		"🛑 Closing InfluxDB writer...")

	w.cancel()
	close(w.writeQueue)
	w.wg.Wait()

	w.mu.Lock()
	total := w.writesSucceeded + w.writesFailed
	successRate := float64(0)
	if total > 0 {
		successRate = float64(w.writesSucceeded) / float64(total) * 100
	}
	w.mu.Unlock()

	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INFLUX_SHUTDOWN",
		fmt.Sprintf("✅ Writer closed. Success: %d, Failed: %d (%.1f%%)",
			w.writesSucceeded, w.writesFailed, successRate))
}

func (w *InfluxWriter) GetStats() (succeeded, failed int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writesSucceeded, w.writesFailed
}

// ✅ FIXED: Proper point creation for InfluxDB v3 Core
func convertToInfluxPoint(payload InverterPayload) *influxdb3.Point {
	tags := map[string]string{
		"device_type": payload.DeviceType,
		"device_name": payload.DeviceName,
		"device_id":   payload.DeviceID,
	}

	if payload.SignalStrength != "" {
		tags["signal_strength"] = payload.SignalStrength
	}

	fields := map[string]interface{}{
		"serial_no":          payload.Data.SerialNo,
		"voltage":            payload.Data.S1V,
		"total_output_power": payload.Data.TotalOutputPower,
		"temperature":        payload.Data.InvTemp,
		"fault_code":         payload.Data.FaultCode,
	}

	// ✅ Use historical timestamp if provided
	timestamp := time.Now()
	if payload.DeviceTimestamp != nil {
		timestamp = *payload.DeviceTimestamp
	}

	return influxdb3.NewPoint(
		"inverter_data",
		tags,
		fields,
		timestamp,
	)
}

func ConvertToInfluxRecord(data map[string]interface{}) InverterPayload {
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