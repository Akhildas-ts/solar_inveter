# 🌞 Solar Inverter Data Monitoring System

A high-performance real-time system designed to collect, process, and monitor inverter data efficiently — capable of handling **600+ records/second**, complete with fault detection, batch processing, and performance monitoring.

---

## 🧩 1. Overview & Features

### **System Capabilities**
- Real-time data ingestion and monitoring
- High-throughput handling (600+ records/second)
- Multi-threaded concurrent data processing
- Fault detection and auto-logging system
- RESTful API for data access and analytics
- MongoDB time-series data storage
- Graceful shutdown and fault-tolerant design

### **Key Features**
- 🚀 **Performance**: Optimized connection pooling and batching for speed  
- ⚙️ **Fault Detection**: Detects 11 different inverter fault types in real time  
- 🧠 **Scalable Architecture**: Easily extendable for multiple device types  
- 📊 **Monitoring**: Detailed logs, system metrics, and fault reports  

---

## 🏗️ 2. Architecture

### **System Diagram**
Client (Inverter Simulator)
↓
HTTP POST (JSON)
↓
API Server (Gin)
↓
Fault Detection Engine
↓
MongoDB (Time-Series Collection)
↓
Dashboard / Analytics


### **Directory Structure**

solar_project/
├── cmd/ # Entry point for main applications
├── services/ # Core business logic
├── models/ # Data structures (MongoDB, JSON models)
├── logger/ # Logging setup and utilities
├── constants/ # Constant definitions (fault codes, limits)
├── routes/ # API route definitions
├── config/ # Database & environment configurations
└── main.go # Application bootstrap



### **Technology Stack**
- **Language:** Go (Golang)
- **Framework:** Gin Web Framework
- **Database:** MongoDB (Time-Series)
- **Utilities:** UUID, Sync/Atomic, Log Rotation
- **Monitoring:** Built-in logging & performance metrics

---

## ⚙️ 3. Installation Guide

### **Prerequisites**
- Go 1.20+  
- MongoDB 6.0+  
- Git  

### **Setup Steps**
```bash
# 1. Clone repository
git clone https://github.com/<your_repo>/solar_project.git
cd solar_project

# 2. Install dependencies
go mod tidy

# 3. Configure environment
cp .env.example .env

# 4. Run the application
go run main.go

Environment Variables
Variable	Description
MONGO_URI	MongoDB connection string
PORT	API server port
LOG_PATH	Log file location
BATCH_SIZE	Number of records per DB batch insert
🔌 4. Complete API Documentation
Available Endpoints (8 Total)
Endpoint	Method	Description
/api/ping	GET	Health check
/api/inverter/send	POST	Send inverter data
/api/inverter/all	GET	Get all inverter data
/api/inverter/stats	GET	Get data statistics
/api/faults/list	GET	List all fault codes
/api/faults/data	GET	Get data filtered by fault code
/api/faults/active	GET	Get currently active faults
/api/faults/latest	GET	Get latest fault records
Example Request
curl -X POST http://localhost:8080/api/inverter/send \
-H "Content-Type: application/json" \
-d '{
  "device_type": "inverter",
  "device_name": "INV_001",
  "device_id": "abc-123",
  "date": "2025-10-15",
  "time": "15:30:00",
  "data": {
    "voltage": 230,
    "current": 12.5,
    "power": 2875
  }
}'

Example Response
{
  "status": "success",
  "message": "Data received successfully"
}

⚠️ 5. Fault Detection System
Fault Codes (0–10)
Code	Severity	Description	Recommended Action
0	Low	No Fault	Normal Operation
1	Medium	Low Voltage	Check input supply
2	Medium	Over Voltage	Inspect grid or panels
3	High	Over Temperature	Ensure cooling fans working
4	High	Under Frequency	Verify input frequency
5	Critical	Overload	Disconnect excess load
6	Critical	DC Overcurrent	Check DC input wiring
7	High	AC Overcurrent	Check AC output
8	Medium	Communication Error	Verify data cable or IP
9	High	Sensor Fault	Replace faulty sensor
10	Critical	Inverter Failure	Restart / Replace inverter
Real-Time Processing

Each incoming record is analyzed immediately

Detected faults are tagged and logged

Critical alerts can trigger notifications or dashboard updates

📈 6. Performance & Monitoring
Benchmark Metrics

600+ records/second throughput

<10ms average insert latency

Batched MongoDB inserts for high performance

Optimization Features

Connection pooling with keep-alive

Atomic counters for concurrent writes

Memory-efficient data buffering

Log Structure
[2025-10-15 15:35:01] [INFO] [INSERT] 100 records flushed to MongoDB
[2025-10-15 15:35:02] [DEBUG] [FAULT] Device INV_001 fault code 3 triggered


Log rotation is handled daily with size limits.

👨‍💻 7. Development Guide
Project Structure

services/ → Handles core logic

models/ → Data schemas

routes/ → REST API endpoints

logger/ → Log creation, rotation, formatting

Thread Safety

Uses sync.Mutex and atomic.AddInt64 for safe counters

Batch queue operations protected by locks

Adding New Features

Create new file under services/

Register route in routes/api.go

Add handler logic

Update models if database schema changes

🧰 8. Troubleshooting
Issue	Cause	Solution
MongoDB connection error	Wrong URI	Check .env file
Data not visible	Wrong DB or collection	Verify MongoDB config
High CPU usage	Large batch size	Reduce BATCH_SIZE
Faults not detected	Missing constants	Check constants/fault_codes.go
Debugging

Use logger.DEBUG mode for detailed logs

Use curl tests to verify endpoints

Monitor MongoDB insert rates using mongostat

🧪 9. Usage Examples
Command-Line Example
go run main.go

API Testing (curl)
curl http://localhost:8080/api/inverter/all

Client Simulator

Simulates multiple inverters sending JSON payloads to the API for testing throughput and fault detection.

⚙️ 10. Advanced Features
Graceful Shutdown

Ensures all pending batches are flushed before termination

Closes DB connections and goroutines cleanly

Batch Processing

Aggregates multiple records before MongoDB insertion

Improves performance and reduces write overhead

Memory Management

Lightweight structs

Reusable buffers for JSON encoding

Optimized garbage collection with sync pools

🏁 Conclusion

This Solar Inverter Monitoring System provides a complete, scalable, and optimized solution for handling large volumes of inverter data in real time with intelligent fault detection and monitoring.

Author: Your Name
Version: 1.0
License: MIT
GitHub: https://github.com/yourusername/solar_project

