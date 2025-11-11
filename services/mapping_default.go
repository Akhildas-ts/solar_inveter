package services

import (
	"solar_project/models"
	"time"
)

// if there is no more mapping system is there in the mongodb, so that's time we use the default formate
func GetDefaultMappings() []models.DataSourceMapping {
	now := time.Now()

	return []models.DataSourceMapping{
		{
			SourceID:    "format_2_inverter",
			Description: "Different field names - nested data",
			NestedPath:  "data",
			Mappings:    format2Mappings(),
			Active:      true,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
			CreatedBy:   "system",
		},
		{
			SourceID:    "flat_format_device",
			Description: "Flat structure - no nested data",
			NestedPath:  "",
			Mappings:    flatFormatMappings(),
			Active:      true,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
			CreatedBy:   "system",
		},
		{
			SourceID:    "unit_conversion_device",
			Description: "Different units with auto-conversion",
			NestedPath:  "readings",
			Mappings:    unitConversionMappings(),
			Active:      true,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
			CreatedBy:   "system",
		},
		{
			SourceID:    "current_format",
			Description: "Original format",
			NestedPath:  "data",
			Mappings:    currentFormatMappings(),
			Active:      true,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
			CreatedBy:   "system",
		},
	}
}

func format2Mappings() []models.FieldMapping {
	return []models.FieldMapping{
		{SourceField: "serial_no", StandardField: "serial_no", DataType: "string", DefaultValue: "UNKNOWN"},
		{SourceField: "voltage_input", StandardField: "voltage", DataType: "int", DefaultValue: 0},
		{SourceField: "power_watts", StandardField: "power", DataType: "int", DefaultValue: 0},
		{SourceField: "freq_hz", StandardField: "frequency", DataType: "int", DefaultValue: 0},
		{SourceField: "energy_today_wh", StandardField: "today_energy", DataType: "int", DefaultValue: 0},
		{SourceField: "energy_total_kwh", StandardField: "total_energy", DataType: "int", DefaultValue: 0, Transform: "multiply:1000"},
		{SourceField: "temp_celsius", StandardField: "temperature", DataType: "int", DefaultValue: 0},
		{SourceField: "error_code", StandardField: "fault_code", DataType: "int", DefaultValue: 0},
	}
}

func flatFormatMappings() []models.FieldMapping {
	return []models.FieldMapping{
		{SourceField: "serial_no", StandardField: "serial_no", DataType: "string", DefaultValue: "UNKNOWN"},
		{SourceField: "V", StandardField: "voltage", DataType: "int", DefaultValue: 0},
		{SourceField: "P", StandardField: "power", DataType: "int", DefaultValue: 0},
		{SourceField: "Hz", StandardField: "frequency", DataType: "int", DefaultValue: 0},
		{SourceField: "E_today", StandardField: "today_energy", DataType: "int", DefaultValue: 0},
		{SourceField: "E_total", StandardField: "total_energy", DataType: "int", DefaultValue: 0},
		{SourceField: "temp", StandardField: "temperature", DataType: "int", DefaultValue: 0},
		{SourceField: "status", StandardField: "fault_code", DataType: "int", DefaultValue: 0},
	}
}

func unitConversionMappings() []models.FieldMapping {
	return []models.FieldMapping{
		{SourceField: "voltage_mv", StandardField: "voltage", DataType: "int", DefaultValue: 0, Transform: "divide:10"},
		{SourceField: "power_kw", StandardField: "power", DataType: "float", DefaultValue: 0.0, Transform: "multiply:1000"},
		{SourceField: "frequency_hz", StandardField: "frequency", DataType: "int", DefaultValue: 0},
		{SourceField: "today_kwh", StandardField: "today_energy", DataType: "float", DefaultValue: 0.0, Transform: "multiply:1000"},
		{SourceField: "total_kwh", StandardField: "total_energy", DataType: "float", DefaultValue: 0.0, Transform: "multiply:1000"},
		{SourceField: "temp_f", StandardField: "temperature", DataType: "int", DefaultValue: 0, Transform: "subtract:32"},
		{SourceField: "fault", StandardField: "fault_code", DataType: "int", DefaultValue: 0},
	}
}

func currentFormatMappings() []models.FieldMapping {
	return []models.FieldMapping{
		{SourceField: "serial_no", StandardField: "serial_no", DataType: "string", DefaultValue: "UNKNOWN"},
		{SourceField: "s1v", StandardField: "voltage", DataType: "int", DefaultValue: 0},
		{SourceField: "total_output_power", StandardField: "power", DataType: "int", DefaultValue: 0},
		{SourceField: "f", StandardField: "frequency", DataType: "int", DefaultValue: 0},
		{SourceField: "today_e", StandardField: "today_energy", DataType: "int", DefaultValue: 0},
		{SourceField: "total_e", StandardField: "total_energy", DataType: "int", DefaultValue: 0},
		{SourceField: "inv_temp", StandardField: "temperature", DataType: "int", DefaultValue: 0},
		{SourceField: "fault_code", StandardField: "fault_code", DataType: "int", DefaultValue: 0},
	}
}
