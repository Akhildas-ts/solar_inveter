#!/bin/bash
# Cleanup script to reset mappings and restart server

echo "🧹 Cleaning up old mappings and data..."

# Delete old mapping configuration
mongosh inverter_db --eval 'db.mappings.deleteMany({ source_id: "Inv" })'

echo "✅ Old mappings deleted"
echo ""
echo "📝 Next steps:"
echo "1. Restart your server (it will auto-seed the new correct mapping)"
echo "2. Run your mock client again"
echo "3. Check MongoDB - data should now have correct field names and values!"
echo ""
echo "Expected MongoDB structure after fix:"
echo "  data.slave_id: \"1\" (not slaveid)"
echo "  data.serial_no: \"SN10000001\" (not serialno)"  
echo "  data.total_output_power: 5234.5 (not totaloutputpower: 0)"
echo "  data.pv1_voltage: 23000 (not pv1voltage: 0)"
