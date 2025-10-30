package services

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"

	"solar_project/config"
	"solar_project/constants"
	"solar_project/logger"
)

// InfluxWriter handles InfluxDB write operations
type InfluxWriter struct {
	client   *influxdb3.Client
	database string
}

// NewInfluxWriter creates a new InfluxDB writer
func NewInfluxWriter() *InfluxWriter {
	return &InfluxWriter{
		client:   config.GetInfluxClient(),
		database: config.GetInfluxDatabase(),
	}
}

// WriteData writes data to InfluxDB
func (i *InfluxWriter) WriteData(data []interface{}, requestID string) error {
	if len(data) == 0 {
		return nil
	}

	// Convert interface{} to InfluxDB points
	points := make([]*influxdb3.Point, 0, len(data))
	for _, item := range data {
		if payload, ok := item.(InverterPayload); ok {
			point := ConvertToInfluxPoint(payload)
			points = append(points, point)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// ✅ FIX 1: Try writing WITHOUT WithDatabase option first
	// The database is already set in the client during initialization
	err := i.client.WritePoints(ctx, points)

	if err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "INFLUX_WRITE",
			fmt.Sprintf("❌ Batch insert FAILED — %d records, Error: %v", len(points), err))
		
		// ✅ FIX 2: Provide better error context
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "INFLUX_DEBUG",
			fmt.Sprintf("Database: %s, Client configured: %v", i.database, i.client != nil))
		
		return err
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "INFLUX_WRITE",
		fmt.Sprintf("✅ Batch inserted successfully: %d records to database '%s'", len(points), i.database))
	return nil
}

// ConvertToInfluxPoint converts payload to InfluxDB point
func ConvertToInfluxPoint(payload InverterPayload) *influxdb3.Point {
	return influxdb3.NewPointWithMeasurement("inverter_data").
		SetTag("device_type", payload.DeviceType).
		SetTag("device_name", payload.DeviceName).
		SetTag("device_id", payload.DeviceID).
		SetTag("signal_strength", payload.SignalStrength).
		SetField("serial_no", payload.Data.SerialNo).
		SetField("voltage", int64(payload.Data.S1V)).
		SetField("total_output_power", int64(payload.Data.TotalOutputPower)).
		SetField("temperature", int64(payload.Data.InvTemp)).
		SetField("fault_code", int64(payload.Data.FaultCode)).
		SetField("reading_date", payload.Date).
		SetField("reading_time", payload.Time).
		SetTimestamp(time.Now())
}