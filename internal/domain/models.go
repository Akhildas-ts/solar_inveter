// internal/domain/models.go
// UPDATED to handle FoxESS scaled data

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
type InverterDetails struct {
    SlaveID        string  `json:"slave_id"`
    SerialNo       string  `json:"serial_no"`
    ModelName      string  `json:"model_name"`

    TotalOutputPower float64 `json:"total_output_power"`
    TotalEnergy      float64 `json:"total_e"`
    TodayEnergy      float64 `json:"today_e"`

    PV1Voltage float64 `json:"pv1_voltage"`
    PV1Current float64 `json:"pv1_current"`
    PV2Voltage float64 `json:"pv2_voltage"`
    PV2Current float64 `json:"pv2_current"`
    PV3Voltage float64 `json:"pv3_voltage"`
    PV3Current float64 `json:"pv3_current"`
    PV4Voltage float64 `json:"pv4_voltage"`
    PV4Current float64 `json:"pv4_current"`

    GridVoltageR float64 `json:"grid_voltage_r"`
    GridVoltageS float64 `json:"grid_voltage_s"`
    GridVoltageT float64 `json:"grid_voltage_t"`

    GridCurrentR float64 `json:"grid_current_r"`
    GridCurrentS float64 `json:"grid_current_s"`
    GridCurrentT float64 `json:"grid_current_t"`

    InverterTemp float64 `json:"inverter_temp"`
    Frequency    float64 `json:"frequency"`

    Alarm1 int `json:"alarm_1"`
    Alarm2 int `json:"alarm_2"`
    Alarm3 int `json:"alarm_3"`
}

// // InverterDetails contains sensor readings (SCALED VALUES)
// type InverterDetails struct {
// 	// Identification
// 	SerialNo string `json:"serial_no" bson:"serial_no"`
// 	SlaveID  string `json:"slave_id,omitempty" bson:"slave_id,omitempty"`
// 	Model    string `json:"model,omitempty" bson:"model,omitempty"`

// 	// Power & Energy (SCALED)
// 	Power       int `json:"power" bson:"power"`               // Watts
// 	TodayEnergy int `json:"today_energy" bson:"today_energy"` // Wh
// 	TotalEnergy int `json:"total_energy" bson:"total_energy"` // Wh

// 	// PV Strings (SCALED)
// 	PV1Voltage int `json:"pv1_voltage,omitempty" bson:"pv1_voltage,omitempty"` // 0.01V precision
// 	PV1Current int `json:"pv1_current,omitempty" bson:"pv1_current,omitempty"` // 0.01A precision
// 	PV2Voltage int `json:"pv2_voltage,omitempty" bson:"pv2_voltage,omitempty"`
// 	PV2Current int `json:"pv2_current,omitempty" bson:"pv2_current,omitempty"`
// 	PV3Voltage int `json:"pv3_voltage,omitempty" bson:"pv3_voltage,omitempty"`
// 	PV3Current int `json:"pv3_current,omitempty" bson:"pv3_current,omitempty"`
// 	PV4Voltage int `json:"pv4_voltage,omitempty" bson:"pv4_voltage,omitempty"`
// 	PV4Current int `json:"pv4_current,omitempty" bson:"pv4_current,omitempty"`

// 	// Grid (SCALED)
// 	GridVoltageR int `json:"grid_voltage_r,omitempty" bson:"grid_voltage_r,omitempty"` // 0.01V
// 	GridVoltageS int `json:"grid_voltage_s,omitempty" bson:"grid_voltage_s,omitempty"`
// 	GridVoltageT int `json:"grid_voltage_t,omitempty" bson:"grid_voltage_t,omitempty"`
// 	GridCurrentR int `json:"grid_current_r,omitempty" bson:"grid_current_r,omitempty"` // 0.001A
// 	GridCurrentS int `json:"grid_current_s,omitempty" bson:"grid_current_s,omitempty"`
// 	GridCurrentT int `json:"grid_current_t,omitempty" bson:"grid_current_t,omitempty"`

// 	// System (SCALED)
// 	Temperature int `json:"temperature" bson:"temperature"` // °C
// 	Frequency   int `json:"frequency" bson:"frequency"`     // Hz (0.001 precision)

// 	// Legacy fields (for backward compatibility)
// 	Voltage     int     `json:"voltage,omitempty" bson:"voltage,omitempty"`
// 	FaultCode   int     `json:"fault_code,omitempty" bson:"fault_code,omitempty"`
// 	GridVoltage float64 `json:"grid_voltage,omitempty" bson:"grid_voltage,omitempty"`

// 	// Alarms
// 	Alarm1 int `json:"alarm1,omitempty" bson:"alarm1,omitempty"`
// 	Alarm2 int `json:"alarm2,omitempty" bson:"alarm2,omitempty"`
// 	Alarm3 int `json:"alarm3,omitempty" bson:"alarm3,omitempty"`
// }

// ... rest of the file remains the same (FieldMapping, DataSourceMapping, etc.)

type FieldMapping struct {
	SourceField   string      `json:"source_field" bson:"source_field"`
	StandardField string      `json:"standard_field" bson:"standard_field"`
	DataType      string      `json:"data_type" bson:"data_type"`
	Transform     string      `json:"transform" bson:"transform"`
	DefaultValue  interface{} `json:"default_value" bson:"default_value"`
	Required      bool        `json:"required" bson:"required"`
}

type DataSourceMapping struct {
	SourceID    string         `json:"source_id" bson:"source_id"`
	Description string         `json:"description" bson:"description"`
	NestedPath  string         `json:"nested_path" bson:"nested_path"`
	Mappings    []FieldMapping `json:"mappings" bson:"mappings"`
	Active      bool           `json:"active" bson:"active"`
	CreatedAt   time.Time      `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" bson:"updated_at"`
}

type QueryFilter struct {
	DeviceID  string
	FaultCode *int
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

type Stats struct {
	TotalRecords  int64   `json:"total_records"`
	NormalRecords int64   `json:"normal_records"`
	FaultRecords  int64   `json:"fault_records"`
	InsertedCount int64   `json:"inserted_count"`
	FailedCount   int64   `json:"failed_count"`
	BufferSize    int     `json:"buffer_size"`
	SuccessRate   float64 `json:"success_rate"`
	DatabaseType  string  `json:"database_type"`
}

type FaultInfo struct {
	Code        int    `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
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
