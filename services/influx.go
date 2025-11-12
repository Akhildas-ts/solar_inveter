package services

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"

	"solar_project/config"
	"solar_project/constants"
	"solar_project/logger"
	"solar_project/models"
)

// InfluxWriter implements DataWriter interface for InfluxDB 3
type InfluxWriter struct {
	client   *influxdb3.Client
	database string
}

// NewInfluxWriter creates a new InfluxDB 3 writer
func NewInfluxWriter() *InfluxWriter {
	return &InfluxWriter{
		client:   config.GetInfluxClient(),
		database: config.GetInfluxDatabase(),
	}
}

// WriteData writes data to InfluxDB 3 using the official client
func (w *InfluxWriter) WriteData(data []interface{}, requestID string) error {
	if len(data) == 0 {
		return nil
	}

	if w.client == nil {
		err := fmt.Errorf("InfluxDB client not initialized")
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "INFLUX_WRITE",
			fmt.Sprintf("❌ %v", err))
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Convert data to InfluxDB 3 points
	var points []*influxdb3.Point
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
		logger.WriteLog(constants.LOG_LEVEL_WARNING, requestID, "INFLUX_WRITE",
			"No valid points to write")
		return nil
	}
	logger.WriteLog(constants.LOG_LEVEL_DEBUG, requestID, "INFLUX_CHECK",
		fmt.Sprintf("Attempting to write %d points to %s", len(points), w.database))

	// Debug logging
	logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "INFLUX_DEBUG",
		fmt.Sprintf("Database: %s, Points: %d/%d, Client: %v",
			w.database, len(points), len(data), w.client != nil))

	// // ✅ OPTION 1: Try WritePoints first
	err := w.client.WritePoints(ctx, points)

	// ✅ OPTION 2: If WritePoints doesn't exist, try Write
	// err := w.client.Write(ctx, points...)

	// ✅ OPTION 3: If both fail, try WriteData with database option
	// err := w.client.WritePoints(ctx, points)

	if err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "INFLUX_WRITE",
			fmt.Sprintf("❌ Batch insert FAILED — %d records, Error: %v", len(points), err))
		return err
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "INFLUX_WRITE",
		fmt.Sprintf("✅ Batch inserted successfully: %d records", len(points)))

	return nil
}

// convertToInfluxPoint converts InverterPayload to InfluxDB 3 Point
func convertToInfluxPoint(payload InverterPayload) *influxdb3.Point {
	// ✅ Prepare all tags (including optional ones)
	tags := map[string]string{
		"device_type": payload.DeviceType,
		"device_name": payload.DeviceName,
		"device_id":   payload.DeviceID,
	}

	// Add optional signal_strength tag
	if payload.SignalStrength != "" {
		tags["signal_strength"] = payload.SignalStrength
	}

	// ✅ Prepare all fields (including optional ones)
	fields := map[string]interface{}{
		"serial_no":          payload.Data.SerialNo,
		"voltage":            payload.Data.S1V,
		"total_output_power": payload.Data.TotalOutputPower,
		"temperature":        payload.Data.InvTemp,
		"fault_code":         payload.Data.FaultCode,
	}

	// Add optional date/time fields
	if payload.Date != "" {
		fields["reading_date"] = payload.Date
	}
	if payload.Time != "" {
		fields["reading_time"] = payload.Time
	}

	// ✅ Create point with ALL tags and fields in one call
	point := influxdb3.NewPoint(
		"inverter_data",
		tags,
		fields,
		time.Now(),
	)

	return point
}

// ConvertToInfluxRecord converts standardized data to InverterPayload
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
