package repository

import (
	"context"
	"fmt"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
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
		"serial_no":          record.Data.SerialNo,
		"slave_id":           record.Data.SlaveID,
		"model_name":         record.Data.ModelName,
		"total_output_power": record.Data.TotalOutputPower,
		"total_e":            record.Data.TotalEnergy,
		"today_e":            record.Data.TodayEnergy,
		"pv1_voltage":        record.Data.PV1Voltage,
		"pv1_current":        record.Data.PV1Current,
		"pv2_voltage":        record.Data.PV2Voltage,
		"pv2_current":        record.Data.PV2Current,
		"pv3_voltage":        record.Data.PV3Voltage,
		"pv3_current":        record.Data.PV3Current,
		"pv4_voltage":        record.Data.PV4Voltage,
		"pv4_current":        record.Data.PV4Current,
		"grid_voltage_r":     record.Data.GridVoltageR,
		"grid_voltage_s":     record.Data.GridVoltageS,
		"grid_voltage_t":     record.Data.GridVoltageT,
		"grid_current_r":     record.Data.GridCurrentR,
		"grid_current_s":     record.Data.GridCurrentS,
		"grid_current_t":     record.Data.GridCurrentT,
		"inverter_temp":      record.Data.InverterTemp,
		"frequency":          record.Data.Frequency,
		"alarm_1":            record.Data.Alarm1,
		"alarm_2":            record.Data.Alarm2,
		"alarm_3":            record.Data.Alarm3,
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
		SlaveID:          getStringValue(value, "slave_id"),
		SerialNo:         getStringValue(value, "serial_no"),
		ModelName:        getStringValue(value, "model_name"),
		TotalOutputPower: getFloatValue(value, "total_output_power"),
		TotalEnergy:      getFloatValue(value, "total_e"),
		TodayEnergy:      getFloatValue(value, "today_e"),
		PV1Voltage:       getFloatValue(value, "pv1_voltage"),
		PV1Current:       getFloatValue(value, "pv1_current"),
		PV2Voltage:       getFloatValue(value, "pv2_voltage"),
		PV2Current:       getFloatValue(value, "pv2_current"),
		PV3Voltage:       getFloatValue(value, "pv3_voltage"),
		PV3Current:       getFloatValue(value, "pv3_current"),
		PV4Voltage:       getFloatValue(value, "pv4_voltage"),
		PV4Current:       getFloatValue(value, "pv4_current"),
		GridVoltageR:     getFloatValue(value, "grid_voltage_r"),
		GridVoltageS:     getFloatValue(value, "grid_voltage_s"),
		GridVoltageT:     getFloatValue(value, "grid_voltage_t"),
		GridCurrentR:     getFloatValue(value, "grid_current_r"),
		GridCurrentS:     getFloatValue(value, "grid_current_s"),
		GridCurrentT:     getFloatValue(value, "grid_current_t"),
		InverterTemp:     getFloatValue(value, "inverter_temp"),
		Frequency:        getFloatValue(value, "frequency"),
		Alarm1:           getIntValue(value, "alarm_1"),
		Alarm2:           getIntValue(value, "alarm_2"),
		Alarm3:           getIntValue(value, "alarm_3"),
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
