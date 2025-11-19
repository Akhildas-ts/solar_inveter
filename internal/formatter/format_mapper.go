// internal/formatter/format_mapper.go
// Maps incoming data to the exact expected format without mutating raw data.
package formatter

import (
	"fmt"
	"time"
)

// ExpectedInverterData - Your exact expected output format.
type ExpectedInverterData struct {
	DeviceType     string             `bson:"device_type" json:"device_type"`
	DeviceName     string             `bson:"device_name" json:"device_name"`
	DeviceID       string             `bson:"device_id" json:"device_id"`
	Date           string             `bson:"date" json:"date"`
	Time           string             `bson:"time" json:"time"`
	TimeZone       string             `bson:"time_zone,omitempty" json:"time_zone,omitempty"`
	Latitude       string             `bson:"latitude,omitempty" json:"latitude,omitempty"`
	Longitude      string             `bson:"longitude,omitempty" json:"longitude,omitempty"`
	SoftwareVer    string             `bson:"software_ver,omitempty" json:"software_ver,omitempty"`
	SignalStrength string             `bson:"signal_strength,omitempty" json:"signal_strength,omitempty"`
	LogInterval    string             `bson:"loginterval,omitempty" json:"loginterval,omitempty"`
	BatteryStatus  string             `bson:"battery_status,omitempty" json:"battery_status,omitempty"`
	Valid          bool               `bson:"valid" json:"valid"`
	Data           ExpectedDataObject `bson:"data" json:"data"`
	RequestID      string             `bson:"request_id,omitempty" json:"request_id,omitempty"`
	RawID          string             `bson:"raw_id,omitempty" json:"raw_id,omitempty"`
	Timestamp      time.Time          `bson:"timestamp" json:"timestamp"`
}

// ExpectedDataObject - Nested data format.
type ExpectedDataObject struct {
	SlaveID   string `bson:"slave_id,omitempty" json:"slave_id,omitempty"`
	SerialNo  string `bson:"serial_no" json:"serial_no"`
	ModelName string `bson:"model_name,omitempty" json:"model_name,omitempty"`

	TotalOutputPower float64 `bson:"total_output_power" json:"total_output_power"`
	TotalE           float64 `bson:"total_e" json:"total_e"`
	TodayE           float64 `bson:"today_e" json:"today_e"`

	PV1Voltage float64 `bson:"pv1_voltage,omitempty" json:"pv1_voltage,omitempty"`
	PV1Current float64 `bson:"pv1_current,omitempty" json:"pv1_current,omitempty"`
	PV2Voltage float64 `bson:"pv2_voltage,omitempty" json:"pv2_voltage,omitempty"`
	PV2Current float64 `bson:"pv2_current,omitempty" json:"pv2_current,omitempty"`
	PV3Voltage float64 `bson:"pv3_voltage,omitempty" json:"pv3_voltage,omitempty"`
	PV3Current float64 `bson:"pv3_current,omitempty" json:"pv3_current,omitempty"`
	PV4Voltage float64 `bson:"pv4_voltage,omitempty" json:"pv4_voltage,omitempty"`
	PV4Current float64 `bson:"pv4_current,omitempty" json:"pv4_current,omitempty"`

	GridVoltageR float64 `bson:"grid_voltage_r,omitempty" json:"grid_voltage_r,omitempty"`
	GridVoltageS float64 `bson:"grid_voltage_s,omitempty" json:"grid_voltage_s,omitempty"`
	GridVoltageT float64 `bson:"grid_voltage_t,omitempty" json:"grid_voltage_t,omitempty"`
	GridCurrentR float64 `bson:"grid_current_r,omitempty" json:"grid_current_r,omitempty"`
	GridCurrentS float64 `bson:"grid_current_s,omitempty" json:"grid_current_s,omitempty"`
	GridCurrentT float64 `bson:"grid_current_t,omitempty" json:"grid_current_t,omitempty"`

	InverterTemp float64 `bson:"inverter_temp" json:"inverter_temp"`
	Frequency    float64 `bson:"frequency" json:"frequency"`

	Alarm1 int `bson:"alarm_1,omitempty" json:"alarm_1,omitempty"`
	Alarm2 int `bson:"alarm_2,omitempty" json:"alarm_2,omitempty"`
	Alarm3 int `bson:"alarm_3,omitempty" json:"alarm_3,omitempty"`
}

// FormatMapper converts raw incoming data into the expected structure.
type FormatMapper struct {
	fieldMappings map[string]map[string]string
}

// NewFormatMapper creates a new mapper instance.
func NewFormatMapper() *FormatMapper {
	fm := &FormatMapper{
		fieldMappings: make(map[string]map[string]string),
	}
	fm.initializeMappings()
	return fm
}

func (fm *FormatMapper) initializeMappings() {
	fm.fieldMappings = map[string]map[string]string{
		"Inv": {
			"slid":  "slave_id",
			"sno":   "serial_no",
			"model": "model_name",
			"p":     "total_output_power",
			"e":     "today_e",
			"te":    "total_e",
			"pv1v":  "pv1_voltage",
			"pv1c":  "pv1_current",
			"pv2v":  "pv2_voltage",
			"pv2c":  "pv2_current",
			"pv3v":  "pv3_voltage",
			"pv3c":  "pv3_current",
			"pv4v":  "pv4_voltage",
			"pv4c":  "pv4_current",
			"gvr":   "grid_voltage_r",
			"gvs":   "grid_voltage_s",
			"gvt":   "grid_voltage_t",
			"gcr":   "grid_current_r",
			"gcs":   "grid_current_s",
			"gct":   "grid_current_t",
			"itmp":  "inverter_temp",
			"fr":    "frequency",
			"al1":   "alarm_1",
			"al2":   "alarm_2",
			"al3":   "alarm_3",
		},
		"current_format": {
			"serial_no":          "serial_no",
			"s1v":                "pv1_voltage",
			"total_output_power": "total_output_power",
			"f":                  "frequency",
			"today_e":            "today_e",
			"total_e":            "total_e",
			"inv_temp":           "inverter_temp",
			"fault_code":         "fault_code",
		},
	}
}

// MapToExpectedFormat transforms raw data into ExpectedInverterData.
func (fm *FormatMapper) MapToExpectedFormat(rawData map[string]interface{}) (*ExpectedInverterData, error) {
	expected := &ExpectedInverterData{
		Timestamp: time.Now(),
		Valid:     true,
	}

	expected.DeviceType = getStringValue(rawData, "device_type", "")
	expected.DeviceName = getStringValue(rawData, "device_name", "")
	expected.DeviceID = getStringValue(rawData, "device_id", "")
	expected.Date = getStringValue(rawData, "date", "")
	expected.Time = getStringValue(rawData, "time", "")
	expected.TimeZone = getStringValue(rawData, "time_zone", "")
	expected.Latitude = getStringValue(rawData, "latitude", "0")
	expected.Longitude = getStringValue(rawData, "longitude", "0")
	expected.SoftwareVer = getStringValue(rawData, "software_ver", "")
	expected.SignalStrength = getStringValue(rawData, "signal_strength", "")
	expected.LogInterval = getStringValue(rawData, "loginterval", "")
	expected.BatteryStatus = getStringValue(rawData, "battery_status", "0")
	expected.RequestID = getStringValue(rawData, "request_id", "")

	dataObj := getNestedObject(rawData, "data")
	if dataObj == nil {
		return expected, fmt.Errorf("no data object found in payload")
	}

	deviceType := expected.DeviceType
	mapping, exists := fm.fieldMappings[deviceType]
	if !exists {
		mapping = fm.fieldMappings["Inv"]
	}

	expected.Data = fm.mapDataFields(dataObj, mapping)

	return expected, nil
}

func (fm *FormatMapper) mapDataFields(sourceData map[string]interface{}, mapping map[string]string) ExpectedDataObject {
	target := ExpectedDataObject{}

	for sourceField, targetField := range mapping {
		sourceValue := sourceData[sourceField]
		if sourceValue == nil {
			continue
		}

		switch targetField {
		case "slave_id", "serial_no", "model_name":
			if str, ok := sourceValue.(string); ok {
				switch targetField {
				case "slave_id":
					target.SlaveID = str
				case "serial_no":
					target.SerialNo = str
				case "model_name":
					target.ModelName = str
				}
			}
		case "pv1_voltage", "pv1_current", "pv2_voltage", "pv2_current",
			"pv3_voltage", "pv3_current", "pv4_voltage", "pv4_current",
			"grid_voltage_r", "grid_voltage_s", "grid_voltage_t",
			"grid_current_r", "grid_current_s", "grid_current_t",
			"inverter_temp", "frequency", "total_output_power", "total_e", "today_e":
			if val, ok := toFloat64(sourceValue); ok {
				assignFloatField(&target, targetField, val)
			}
		case "alarm_1", "alarm_2", "alarm_3":
			if val, ok := toInt(sourceValue); ok {
				assignIntField(&target, targetField, val)
			}
		}
	}

	return target
}

func getStringValue(data map[string]interface{}, key, def string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return def
}

func getNestedObject(data map[string]interface{}, key string) map[string]interface{} {
	if val, ok := data[key]; ok {
		if nested, ok := val.(map[string]interface{}); ok {
			return nested
		}
	}
	return nil
}

func toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		var parsed float64
		if _, err := fmt.Sscan(v, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func toInt(val interface{}) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		var parsed int
		if _, err := fmt.Sscan(v, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func assignFloatField(target *ExpectedDataObject, field string, value float64) {
	switch field {
	case "total_output_power":
		target.TotalOutputPower = value
	case "total_e":
		target.TotalE = value
	case "today_e":
		target.TodayE = value
	case "pv1_voltage":
		target.PV1Voltage = value
	case "pv1_current":
		target.PV1Current = value
	case "pv2_voltage":
		target.PV2Voltage = value
	case "pv2_current":
		target.PV2Current = value
	case "pv3_voltage":
		target.PV3Voltage = value
	case "pv3_current":
		target.PV3Current = value
	case "pv4_voltage":
		target.PV4Voltage = value
	case "pv4_current":
		target.PV4Current = value
	case "grid_voltage_r":
		target.GridVoltageR = value
	case "grid_voltage_s":
		target.GridVoltageS = value
	case "grid_voltage_t":
		target.GridVoltageT = value
	case "grid_current_r":
		target.GridCurrentR = value
	case "grid_current_s":
		target.GridCurrentS = value
	case "grid_current_t":
		target.GridCurrentT = value
	case "inverter_temp":
		target.InverterTemp = value
	case "frequency":
		target.Frequency = value
	}
}

func assignIntField(target *ExpectedDataObject, field string, value int) {
	switch field {
	case "alarm_1":
		target.Alarm1 = value
	case "alarm_2":
		target.Alarm2 = value
	case "alarm_3":
		target.Alarm3 = value
	}
}
