package models

import "time"

// InverterData represents the main inverter data structure
type InverterData struct {
	DeviceType     string     `json:"device_type" bson:"device_type"`
	DeviceName     string     `json:"device_name" bson:"device_name"`
	DeviceID       string     `json:"device_id" bson:"device_id"`
	Date           string     `json:"date" bson:"date"`
	Time           string     `json:"time" bson:"time"`
	SignalStrength string     `json:"signal_strength" bson:"signal_strength"`
	Data           SensorData `json:"data" bson:"data"`
	Timestamp      time.Time  `json:"timestamp" bson:"timestamp"`
}

// SensorData represents the sensor readings
type SensorData struct {
	SerialNo         string `json:"serial_no" bson:"serial_no"`
	S1V              int    `json:"s1v" bson:"s1v"`
	TotalOutputPower int    `json:"total_output_power" bson:"total_output_power"`
	F                int    `json:"f" bson:"f"`
	TodayE           int    `json:"today_e" bson:"today_e"`
	TotalE           int    `json:"total_e" bson:"total_e"`
	InvTemp          int    `json:"inv_temp" bson:"inv_temp"`
	FaultCode        int    `json:"fault_code" bson:"fault_code"`
}

// FaultInfo represents fault code information
type FaultInfo struct {
	Code        int    `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // INFO, WARNING, CRITICAL
	Action      string `json:"action"`
}

// FaultCodes contains all fault code definitions
var FaultCodes = map[int]FaultInfo{
	0: {
		Code:        0,
		Name:        "Normal Operation",
		Description: "Solar system working perfectly",
		Severity:    "INFO",
		Action:      "No action needed",
	},
	1: {
		Code:        1,
		Name:        "Low Energy Output",
		Description: "Energy production below expected level",
		Severity:    "WARNING",
		Action:      "Check panel cleanliness and weather conditions",
	},
	2: {
		Code:        2,
		Name:        "High Temperature",
		Description: "Inverter temperature exceeding normal range",
		Severity:    "WARNING",
		Action:      "Check ventilation and cooling system",
	},
	3: {
		Code:        3,
		Name:        "Grid Connection Issue",
		Description: "Problem with grid connection or synchronization",
		Severity:    "CRITICAL",
		Action:      "Check grid connection and contact utility provider",
	},
	4: {
		Code:        4,
		Name:        "Low Voltage",
		Description: "Input voltage below minimum threshold",
		Severity:    "WARNING",
		Action:      "Check panel connections and wiring",
	},
	5: {
		Code:        5,
		Name:        "High Voltage",
		Description: "Input voltage above maximum threshold",
		Severity:    "CRITICAL",
		Action:      "Disconnect immediately and contact technician",
	},
	6: {
		Code:        6,
		Name:        "Frequency Out of Range",
		Description: "AC frequency deviation from normal range",
		Severity:    "WARNING",
		Action:      "Monitor grid frequency and check inverter settings",
	},
	7: {
		Code:        7,
		Name:        "Inverter Overload",
		Description: "Current load exceeding inverter capacity",
		Severity:    "CRITICAL",
		Action:      "Reduce load or upgrade inverter capacity",
	},
	8: {
		Code:        8,
		Name:        "Communication Error",
		Description: "Lost communication with monitoring system",
		Severity:    "WARNING",
		Action:      "Check network connection and restart monitoring",
	},
	9: {
		Code:        9,
		Name:        "Hardware Fault",
		Description: "Internal hardware component failure detected",
		Severity:    "CRITICAL",
		Action:      "Immediate service required - contact technician",
	},
	10: {
		Code:        10,
		Name:        "System Shutdown",
		Description: "Emergency system shutdown activated",
		Severity:    "CRITICAL",
		Action:      "Check system safety and contact support immediately",
	},
}