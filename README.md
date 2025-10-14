# 🌞 Solar Monitoring System with Fault Detection

A high-performance Go-based solar inverter monitoring system that generates, stores, and analyzes solar panel data with comprehensive fault detection capabilities.

## 🚀 Features

- **High-Speed Data Generation**: Generates 600 records per second (36,000 records total)
- **Real-time Fault Detection**: Monitors 10 different fault types with severity levels
- **MongoDB Integration**: Efficient batch insertion with automatic buffering
- **RESTful API**: Complete API for data access and fault analysis
- **Web Dashboard**: Interactive web interface for monitoring
- **Performance Metrics**: Real-time statistics and success rate monitoring

## 📊 Fault Detection System

The system monitors **11 fault types** (including normal operation):

| Code | Fault Type | Severity | Description |
|------|------------|----------|-------------|
| 0 | Normal Operation | INFO | System working perfectly |
| 1 | Low Energy Output | WARNING | Energy production below expected |
| 2 | High Temperature | WARNING | Inverter temperature too high |
| 3 | Grid Connection Issue | CRITICAL | Grid synchronization problems |
| 4 | Low Voltage | WARNING | Input voltage below threshold |
| 5 | High Voltage | CRITICAL | Input voltage above threshold |
| 6 | Frequency Out of Range | WARNING | AC frequency deviation |
| 7 | Inverter Overload | CRITICAL | Load exceeding capacity |
| 8 | Communication Error | WARNING | Network communication lost |
| 9 | Hardware Fault | CRITICAL | Internal component failure |
| 10 | System Shutdown | CRITICAL | Emergency shutdown activated |

## 🛠️ Technology Stack

- **Backend**: Go (Gin framework)
- **Database**: MongoDB
- **Frontend**: HTML/CSS/JavaScript
- **Architecture**: Modular design with separate packages

## 📁 Project Structure

```
solar/
├── main.go              # Application entry point
├── config/
│   └── db.go           # Database configuration
├── models/
│   └── inveter.go      # Data models and fault definitions
├── routes/
│   └── api.go          # API route handlers
├── services/
│   └── data.go         # Data generation and processing
├── static/
│   └── index.html      # Web dashboard
├── go.mod              # Go module dependencies
└── README.md           # This file
```

## 🚀 Quick Start

### Prerequisites

- Go 1.19 or higher
- MongoDB 4.4 or higher

### Installation

1. **Clone the repository**
   ```bash
   git clone <your-repo-url>
   cd solar
   ```

2. **Install dependencies**
   ```bash
   go mod tidy
   ```

3. **Start MongoDB**
   ```bash
   # Using Docker
   docker run -d -p 27017:27017 --name mongodb mongo:latest
   
   # Or start your local MongoDB service
   mongod
   ```

4. **Run the application**
   ```bash
   go run main.go
   ```

5. **Access the application**
   - Web Dashboard: http://localhost:8080
   - API Base URL: http://localhost:8080/api

## 📡 API Documentation

### Basic APIs

#### Get All Data
```http
GET /api/all?page=1&limit=100
```
Returns paginated records with metadata.

#### Get Statistics
```http
GET /api/stats
```
Returns system statistics including success rates and fault counts.

### Fault Detection APIs

#### Get Fault Code List
```http
GET /api/faults/list
```
Returns all available fault code definitions.

#### Get Data by Fault Code
```http
GET /api/faults/data?code=3
```
Returns records filtered by specific fault code (0-10).

#### Get Fault Statistics
```http
GET /api/faults/stats
```
Returns aggregated fault statistics from the database.

#### Get Active Faults
```http
GET /api/faults/active
```
Returns only records with active faults (excludes normal operation).

#### Get Latest Faults
```http
GET /api/faults/latest
```
Returns the latest 50 fault records grouped by fault code.

## 🧪 Testing with Postman

### Basic API Tests

1. **Get All Data**
   - URL: `http://localhost:8080/api/all`
   - Method: `GET`
   - Body: None

2. **Get Statistics**
   - URL: `http://localhost:8080/api/stats`
   - Method: `GET`
   - Body: None

### Fault Detection Tests

1. **Get Fault List**
   - URL: `http://localhost:8080/api/faults/list`
   - Method: `GET`
   - Body: None

2. **Get Data by Fault Code**
   - URL: `http://localhost:8080/api/faults/data?code=3`
   - Method: `GET`
   - Body: None
   - Query Parameters: `code` (0-10)

3. **Get Fault Statistics**
   - URL: `http://localhost:8080/api/faults/stats`
   - Method: `GET`
   - Body: None

4. **Get Active Faults**
   - URL: `http://localhost:8080/api/faults/active`
   - Method: `GET`
   - Body: None

5. **Get Latest Faults**
   - URL: `http://localhost:8080/api/faults/latest`
   - Method: `GET`
   - Body: None

## 📈 Performance Features

- **Batch Processing**: Efficient batch insertion (100 records per batch)
- **Concurrent Operations**: Multiple goroutines for data generation and processing
- **Memory Management**: Atomic counters and mutex-protected operations
- **Error Handling**: Comprehensive error tracking and reporting
- **Real-time Monitoring**: Live statistics reporting every 5 seconds

## 🎯 Data Generation

The system generates realistic solar inverter data including:

- **Device Information**: Device type, name, ID
- **Sensor Data**: Voltage, power output, frequency, temperature
- **Energy Metrics**: Today's energy, total energy
- **Fault Detection**: Automatic fault code generation (90% normal, 10% faults)
- **Timestamps**: Precise timing for all records

## 🔧 Configuration

### Database Configuration
- **Host**: localhost:27017
- **Database**: solar_monitoring
- **Collection**: inverter_data
- **Connection Pool**: 10-50 connections

### Performance Settings
- **Generation Rate**: 600 records/second
- **Batch Size**: 100 records
- **Flush Interval**: 200ms
- **Target Records**: 36,000 total

## 📊 Monitoring Dashboard

The web dashboard provides:
- Real-time data visualization
- Fault monitoring and alerts
- System performance metrics
- Interactive charts and graphs

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🆘 Support

For support and questions:
- Create an issue in the repository
- Check the API documentation above
- Review the fault code definitions for troubleshooting

## 🎉 Acknowledgments

- Built with Go and Gin framework
- MongoDB for high-performance data storage
- Responsive web design for monitoring dashboard

---

**Happy Solar Monitoring! 🌞⚡**