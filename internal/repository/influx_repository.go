package repository

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"solar_project/internal/config"
	"solar_project/internal/domain"
)

// InfluxRepo implements Repository for InfluxDB
type InfluxRepo struct {
	db *config.InfluxDatabase
}

// NewInfluxRepo creates a new InfluxDB repository
func NewInfluxRepo(db *config.InfluxDatabase) *InfluxRepo {
	return &InfluxRepo{db: db}
}

// Insert writes records to InfluxDB with defensive checks
func (r *InfluxRepo) Insert(ctx context.Context, records []domain.InverterData) error {
	// CRITICAL: Check for nil client
	if r.db == nil {
		return fmt.Errorf("InfluxDB database is nil")
	}
	if r.db.Client == nil {
		return fmt.Errorf("InfluxDB client is nil - database not initialized properly")
	}

	if len(records) == 0 {
		return nil
	}

	// Convert to points
	points := make([]*influxdb3.Point, 0, len(records))
	for _, record := range records {
		point := r.recordToPoint(record)
		if point != nil {
			points = append(points, point)
		}
	}

	if len(points) == 0 {
		return fmt.Errorf("no valid points to write")
	}

	// Write with timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// DEFENSIVE: Log before write
	fmt.Printf("📝 Writing %d points to InfluxDB...\n", len(points))

	err := r.db.Client.WritePoints(ctx, points)
	if err != nil {
		// LOG the actual error with details
		return fmt.Errorf("WritePoints failed: %w (points: %d, db: %s)", 
			err, len(points), r.db.Database)
	}

	fmt.Printf("✅ Successfully wrote %d points\n", len(points))
	return nil
}

// recordToPoint converts domain model to InfluxDB point
func (r *InfluxRepo) recordToPoint(record domain.InverterData) *influxdb3.Point {
	tags := map[string]string{
		"device_type": record.DeviceType,
		"device_name": record.DeviceName,
		"device_id":   record.DeviceID,
	}

	if record.SignalStrength != "" {
		tags["signal_strength"] = record.SignalStrength
	}

	fields := map[string]interface{}{
		"serial_no":    record.Data.SerialNo,
		"voltage":      record.Data.Voltage,
		"power":        record.Data.Power,
		"frequency":    record.Data.Frequency,
		"today_energy": record.Data.TodayEnergy,
		"total_energy": record.Data.TotalEnergy,
		"temperature":  record.Data.Temperature,
		"fault_code":   record.Data.FaultCode,
	}

	if record.Data.GridVoltage > 0 {
		fields["grid_voltage"] = record.Data.GridVoltage
	}

	return influxdb3.NewPoint(
		"inverter_data",
		tags,
		fields,
		record.Timestamp,
	)
}

// Query retrieves records from InfluxDB
func (r *InfluxRepo) Query(ctx context.Context, filter domain.QueryFilter) ([]domain.InverterData, error) {
	// CRITICAL: Check for nil client
	if r.db == nil || r.db.Client == nil {
		return nil, fmt.Errorf("InfluxDB client is nil - database not initialized")
	}

	// Build SQL query
	query := "SELECT * FROM inverter_data WHERE 1=1"

	if filter.DeviceID != "" {
		query += fmt.Sprintf(" AND device_id = '%s'", filter.DeviceID)
	}

	if filter.FaultCode != nil {
		if *filter.FaultCode == 0 {
			query += " AND fault_code = 0"
		} else {
			query += " AND fault_code > 0"
		}
	}

	if filter.StartTime != nil {
		query += fmt.Sprintf(" AND time >= '%s'", filter.StartTime.Format(time.RFC3339))
	}

	if filter.EndTime != nil {
		query += fmt.Sprintf(" AND time <= '%s'", filter.EndTime.Format(time.RFC3339))
	}

	query += " ORDER BY time DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	// DEFENSIVE: Log query
	fmt.Printf("🔍 Executing query: %s\n", query)

	// Execute query
	iterator, err := r.db.Client.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w (query: %s)", err, query)
	}

	// Parse results
	var results []domain.InverterData
	for iterator.Next() {
		record := r.pointToRecord(iterator.Value())
		results = append(results, record)
	}

	fmt.Printf("✅ Query returned %d records\n", len(results))
	return results, nil
}

// pointToRecord converts InfluxDB result to domain model
func (r *InfluxRepo) pointToRecord(value map[string]interface{}) domain.InverterData {
	record := domain.InverterData{
		DeviceType:     getStringValue(value, "device_type"),
		DeviceName:     getStringValue(value, "device_name"),
		DeviceID:       getStringValue(value, "device_id"),
		SignalStrength: getStringValue(value, "signal_strength"),
	}

	if ts, ok := value["time"].(time.Time); ok {
		record.Timestamp = ts
	}

	record.Data = domain.InverterDetails{
		SerialNo:    getStringValue(value, "serial_no"),
		Voltage:     getIntValue(value, "voltage"),
		Power:       getIntValue(value, "power"),
		Frequency:   getIntValue(value, "frequency"),
		TodayEnergy: getIntValue(value, "today_energy"),
		TotalEnergy: getIntValue(value, "total_energy"),
		Temperature: getIntValue(value, "temperature"),
		FaultCode:   getIntValue(value, "fault_code"),
		GridVoltage: getFloatValue(value, "grid_voltage"),
	}

	return record
}

// Count returns number of matching records
func (r *InfluxRepo) Count(ctx context.Context, filter domain.QueryFilter) (int64, error) {
	// CRITICAL: Check for nil client
	if r.db == nil || r.db.Client == nil {
		return 0, fmt.Errorf("InfluxDB client is nil - database not initialized")
	}

	query := "SELECT COUNT(*) as count FROM inverter_data WHERE 1=1"

	if filter.DeviceID != "" {
		query += fmt.Sprintf(" AND device_id = '%s'", filter.DeviceID)
	}

	if filter.FaultCode != nil {
		if *filter.FaultCode == 0 {
			query += " AND fault_code = 0"
		} else {
			query += " AND fault_code > 0"
		}
	}

	iterator, err := r.db.Client.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("count query failed: %w", err)
	}

	if iterator.Next() {
		value := iterator.Value()
		// Try multiple type assertions for count
		if count, ok := value["count"].(int64); ok {
			return count, nil
		}
		if count, ok := value["count"].(float64); ok {
			return int64(count), nil
		}
		if count, ok := value["count"].(int); ok {
			return int64(count), nil
		}
	}

	return 0, nil
}

// Type returns database type
func (r *InfluxRepo) Type() string {
	return "influx"
}

// Helper functions with better type handling
func getStringValue(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

func getIntValue(data map[string]interface{}, key string) int {
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
		return 0
	}
}

func getFloatValue(data map[string]interface{}, key string) float64 {
	switch val := data[key].(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0.0
	}
}