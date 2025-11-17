package domain

import "time"

// InverterData represents solar inverter record
type InverterData struct {
	DeviceType     string          `json:"device_type" bson:"device_type"`
	DeviceName     string          `json:"device_name" bson:"device_name"`
	DeviceID       string          `json:"device_id" bson:"device_id"`
	SignalStrength string          `json:"signal_strength" bson:"signal_strength"`
	Timestamp      time.Time       `json:"timestamp" bson:"timestamp"`
	Data           InverterDetails `json:"data" bson:"data"`
}

// InverterDetails contains sensor readings
type InverterDetails struct {
	SerialNo         string  `json:"serial_no" bson:"serial_no"`
	Voltage          int     `json:"voltage" bson:"voltage"`                     // s1_v
	Power            int     `json:"power" bson:"power"`                         // total_output_power
	Frequency        int     `json:"frequency" bson:"frequency"`                 // f
	TodayEnergy      int     `json:"today_energy" bson:"today_energy"`           // today_e
	TotalEnergy      int     `json:"total_energy" bson:"total_energy"`           // total_e
	Temperature      int     `json:"temperature" bson:"temperature"`             // inv_temp
	FaultCode        int     `json:"fault_code" bson:"fault_code"`
	GridVoltage      float64 `json:"grid_voltage,omitempty" bson:"grid_voltage,omitempty"`
}

// Mapping models
type FieldMapping struct {
	SourceField   string      `json:"source_field" bson:"source_field"`
	StandardField string      `json:"standard_field" bson:"standard_field"`
	DataType      string      `json:"data_type" bson:"data_type"`         // int, float, string, bool
	Transform     string      `json:"transform" bson:"transform"`         // multiply:10, divide:100, etc.
	DefaultValue  interface{} `json:"default_value" bson:"default_value"`
	Required      bool        `json:"required" bson:"required"`
}

type DataSourceMapping struct {
	SourceID    string         `json:"source_id" bson:"source_id"`
	Description string         `json:"description" bson:"description"`
	NestedPath  string         `json:"nested_path" bson:"nested_path"` // e.g., "data" for nested
	Mappings    []FieldMapping `json:"mappings" bson:"mappings"`
	Active      bool           `json:"active" bson:"active"`
	CreatedAt   time.Time      `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" bson:"updated_at"`
}

// Query filters
type QueryFilter struct {
	DeviceID  string
	FaultCode *int
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// Statistics
type Stats struct {
	TotalRecords   int64   `json:"total_records"`
	NormalRecords  int64   `json:"normal_records"`
	FaultRecords   int64   `json:"fault_records"`
	InsertedCount  int64   `json:"inserted_count"`
	FailedCount    int64   `json:"failed_count"`
	BufferSize     int     `json:"buffer_size"`
	SuccessRate    float64 `json:"success_rate"`
	DatabaseType   string  `json:"database_type"`
}

// Fault definitions
type FaultInfo struct {
	Code        int    `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // INFO, WARNING, CRITICAL
	Action      string `json:"action"`
}

var FaultCodes = map[int]FaultInfo{
	0:  {0, "Normal Operation", "System working properly", "INFO", "No action needed"},
	1:  {1, "Low Energy Output", "Energy below expected level", "WARNING", "Check panels"},
	2:  {2, "High Temperature", "Temperature exceeding normal range", "WARNING", "Check cooling"},
	3:  {3, "Grid Connection Issue", "Problem with grid connection", "CRITICAL", "Check grid connection"},
	4:  {4, "Low Voltage", "Voltage below minimum threshold", "WARNING", "Check connections"},
	5:  {5, "High Voltage", "Voltage above maximum threshold", "CRITICAL", "Disconnect immediately"},
	6:  {6, "Frequency Out of Range", "AC frequency deviation", "WARNING", "Monitor grid frequency"},
	7:  {7, "Inverter Overload", "Load exceeding capacity", "CRITICAL", "Reduce load"},
	8:  {8, "Communication Error", "Lost communication", "WARNING", "Check network"},
	9:  {9, "Hardware Fault", "Component failure detected", "CRITICAL", "Contact technician"},
	10: {10, "System Shutdown", "Emergency shutdown activated", "CRITICAL", "Contact support"},
}