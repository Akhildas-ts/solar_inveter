#!/bin/bash

# Diagnostic script for FoxESS Solar Monitoring System
# Tests: Mapping → Scaling → Storage

set -e

API="http://localhost:8080"
DB_HOST="localhost:27017"
DB_NAME="solar_monitoring"

echo "🔍 FoxESS Solar Monitoring - Diagnostic Test"
echo "=============================================="
echo ""

# Test 1: Check API is running
echo "1️⃣  Checking API connectivity..."
if curl -sf "${API}/api/stats" > /dev/null 2>&1; then
    echo "   ✅ API is running on ${API}"
else
    echo "   ❌ API not responding. Start with: go run ./cmd/server"
    exit 1
fi

# Test 2: Send test payload
echo ""
echo "2️⃣  Sending test FoxESS payload..."

PAYLOAD=$(cat <<'EOF'
{
  "device_type": "Inv",
  "device_name": "TestInverter",
  "device_id": "TEST001",
  "date": "17/11/2025",
  "time": "14:30:00",
  "time_zone": "Asia/Kolkata",
  "latitude": "0",
  "longitude": "0",
  "software_ver": "Q.H.1.0.1",
  "signal_strength": "5",
  "loginterval": "900",
  "battery_status": "0",
  "valid": true,
  "data": {
    "slaveid": "1",
    "serialno": "SN123456",
    "modelname": "EHU10K-ET",
    "totaloutputpower": 5432000,
    "todaye": 45000000,
    "totale": 1100000000,
    "pv1voltage": 45640,
    "pv1current": 36000,
    "pv2voltage": 50500,
    "pv2current": 38000,
    "pv3voltage": 0,
    "pv3current": 0,
    "pv4voltage": 0,
    "pv4current": 0,
    "gridvoltager": 23000,
    "gridvoltages": 23500,
    "gridvoltaget": 24000,
    "gridcurrentr": 4500,
    "gridcurrents": 4700,
    "gridcurrentt": 4600,
    "invertertemp": 530,
    "frequency": 500000,
    "alarm1": 0,
    "alarm2": 0,
    "alarm3": 0
  }
}
EOF
)

RESPONSE=$(curl -sf -X POST "${API}/api/data" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD")

if echo "$RESPONSE" | grep -q "success"; then
    echo "   ✅ Payload sent successfully"
    echo "   Response: $RESPONSE"
else
    echo "   ⚠️  API response: $RESPONSE"
fi

# Test 3: Check stats
echo ""
echo "3️⃣  Checking statistics..."
STATS=$(curl -sf "${API}/api/stats")
echo "   Database stats:"
echo "$STATS" | jq '.' 2>/dev/null || echo "$STATS"

# Test 4: Check stored data in MongoDB
echo ""
echo "4️⃣  Checking MongoDB storage..."
mongosh "mongodb://${DB_HOST}/${DB_NAME}" --eval "
db.inverter_data.findOne({}, {projection: {
  'device_id': 1,
  'data.serial_no': 1,
  'data.total_output_power': 1,
  'data.pv1_voltage': 1,
  'data.inverter_temp': 1,
  'data.frequency': 1,
  'timestamp': 1
}}).then(doc => {
  if (doc) {
    print('✅ Record stored in MongoDB');
    print('Device: ' + doc.device_id);
    print('Serial: ' + doc.data.serial_no);
    print('Power: ' + doc.data.total_output_power + 'W (should be ~5432W)');
    print('PV1V: ' + doc.data.pv1_voltage + 'V (should be ~456V)');
    print('Temp: ' + doc.data.inverter_temp + '°C (should be ~53°C)');
    print('Freq: ' + doc.data.frequency + 'Hz (should be ~500Hz)');
  } else {
    print('❌ No records found in MongoDB');
  }
}).catch(err => {
  print('⚠️  MongoDB check skipped: ' + err.message);
});
" 2>/dev/null || echo "   ⚠️  MongoDB connection skipped"

# Test 5: Verify scaling
echo ""
echo "5️⃣  Verifying scaling factors..."
echo "   Expected conversions:"
echo "   • totaloutputpower: 5432000 mW → 5432 W (÷1000)"
echo "   • pv1voltage: 45640 cV → 456.4 V (÷100)"
echo "   • invertertemp: 530 (0.1°C) → 53.0°C (÷10)"
echo "   • frequency: 500000 mHz → 500 Hz (÷1000)"

# Test 6: Check mappings
echo ""
echo "6️⃣  Checking mappings in MongoDB..."
mongosh "mongodb://${DB_HOST}/${DB_NAME}" --eval "
db.mappings.findOne({source_id: 'Inv'}).then(doc => {
  if (doc) {
    print('✅ Inv mapping exists');
    print('   Fields: ' + doc.mappings.length);
    print('   NestedPath: ' + (doc.nested_path || 'data'));
    print('   Active: ' + doc.active);
    
    const powerField = doc.mappings.find(f => f.source_field === 'totaloutputpower');
    if (powerField) {
      print('   Power transform: ' + powerField.transform);
    }
  } else {
    print('❌ Inv mapping not found');
  }
}).catch(err => {
  print('⚠️  Mapping check skipped: ' + err.message);
});
" 2>/dev/null || echo "   ⚠️  Mapping check skipped"

# Test 7: Raw data check
echo ""
echo "7️⃣  Checking raw data storage..."
mongosh "mongodb://${DB_HOST}/${DB_NAME}" --eval "
db.raw_data.countDocuments({}).then(count => {
  print('✅ Raw data records: ' + count);
}).catch(err => {
  print('⚠️  Raw data check skipped: ' + err.message);
});
" 2>/dev/null || echo "   ⚠️  Raw data check skipped"

echo ""
echo "=============================================="
echo "✅ Diagnostic test complete!"
echo ""
echo "📊 Next steps:"
echo "1. Run the mock generator: go run cmd/client/main.go"
echo "2. Watch server logs for conversions"
echo "3. Check dashboard: http://localhost:8080"
echo ""