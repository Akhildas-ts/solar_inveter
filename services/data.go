package services

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	 "solar_project/logger"
	 "solar_project/constants"
	 "github.com/google/uuid"

	"solar_project/models"
	
	"net/http"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"github.com/gin-gonic/gin"
)


var (
	collection     *mongo.Collection
	insertedCount  int64
	failedCount    int64
	batchBuffer    []interface{}
	bufferMutex    sync.Mutex
	batchSize      = 100
	faultStats     = make(map[int]int64)
	faultStatMutex sync.Mutex
	
)

// InitGenerator initializes the data generator with MongoDB collection
func InitGenerator(coll *mongo.Collection) {
	collection = coll
	logger.WriteLog(constants.LOG_LEVEL_INFO, "", "INIT", "Data generator initialized with MongoDB collection")
}


func GenerateHandler(c *gin.Context) {

	var template models.InverterData


	if err := c.ShouldBindJSON(&template); err != nil {
		logger.WriteLog(constants.LOG_LEVEL_ERROR, "", "API", fmt.Sprintf("Bad request: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
//  // Generate unique ID for this request
//  requestID := uuid.New().String()[:16]


	
	go Generate600RecordsPerSecond(template)


	c.JSON(http.StatusOK, gin.H{
		"message": "Data generation started successfully",
		"template_used": template.DeviceName,
		
	})


}

// Generate600RecordsPerSecond generates data at 600 records/second
func Generate600RecordsPerSecond(template models.InverterData) {
	interval := time.Second / 600
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	counter := 0
	startTime := time.Now()
	targetRecords := 36000

	for range ticker.C {
		 // Generate unique ID for this request
 requestID := uuid.New().String()[:16]

 logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "API", fmt.Sprintf("Data generation requested for template: %s", template.DeviceName))
		if counter >= targetRecords {
			elapsed := time.Since(startTime)
			logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "GENERATOR", fmt.Sprintf("Completed! Inserted %d records in %v (%.2f rec/sec)", counter, elapsed, float64(counter)/elapsed.Seconds()))
			fmt.Printf("\n Completed! Inserted %d records in %v (%.2f rec/sec)\n",
				counter, elapsed, float64(counter)/elapsed.Seconds())

			time.Sleep(time.Second)
			FlushBatch(requestID)

			fmt.Printf("\nFinal Stats:\n")
			fmt.Printf("   Success: %d\n", atomic.LoadInt64(&insertedCount))
			fmt.Printf("   Failed: %d\n\n", atomic.LoadInt64(&failedCount))
			logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "GENERATOR", fmt.Sprintf("Final Stats: Success=%d, Failed=%d",
			atomic.LoadInt64(&insertedCount), atomic.LoadInt64(&failedCount)))
			PrintFaultSummary()
			break
		}

		now := time.Now()

		// Generate fault code (90% normal, 10% faults)
		faultCode := generateFaultCode()

		// Update fault statistics
		updateFaultStats(faultCode)

		// Adjust values based on fault code
		voltage := 6289 + rand.Intn(200) - 100
		power := 147417 + rand.Intn(1000)
		temp := 659 + rand.Intn(20) - 10

		// Modify values if there's a fault
		if faultCode > 0 {
			voltage, power, temp = applyFaultEffects(faultCode, voltage, power, temp)
			logger.WriteLog(constants.LOG_LEVEL_DEBUG, requestID, "FAULT",
				fmt.Sprintf("Applied fault code %d: adjusted values", faultCode))
		}

		// clone base template
		data := template
		data.Date = now.Format("02/01/2006")
		data.Time = now.Format("15:04:05")
		data.Timestamp = now
		data.DeviceID = fmt.Sprintf("ESDL%d", rand.Intn(600)+1)
		data.Data.SerialNo = fmt.Sprintf("%d", rand.Intn(600)+1)
		data.Data.S1V = voltage
		data.Data.TotalOutputPower = power
		data.Data.InvTemp = temp
		data.Data.FaultCode = faultCode

		bufferMutex.Lock()
		batchBuffer = append(batchBuffer, data)
		shouldFlush := len(batchBuffer) >= batchSize
		bufferMutex.Unlock()
		if shouldFlush {
			go FlushBatch(requestID)
		}

		counter++
	}
}

// generateFaultCode generates fault codes: 90% normal, 10% faults
func generateFaultCode() int {
	roll := rand.Intn(100)

	if roll < 90 {
		return 0 // Normal operation
	}

	// Distribute faults
	// Critical faults (3, 5, 7, 9, 10) - 3%
	// Warning faults (1, 2, 4, 6, 8) - 7%

	faultRoll := rand.Intn(10)
	if faultRoll < 7 {
		// Warning faults
		warnings := []int{1, 2, 4, 6, 8}
		return warnings[rand.Intn(len(warnings))]
	}
	// Critical faults
	criticals := []int{3, 5, 7, 9, 10}
	return criticals[rand.Intn(len(criticals))]
}

// applyFaultEffects applies effects to sensor values based on fault code
func applyFaultEffects(faultCode, voltage, power, temp int) (int, int, int) {
	switch faultCode {
	case 1: // Low Energy
		power = power / 2
	case 2: // High Temperature
		temp = temp + 100
	case 3: // Grid Issue
		power = power / 3
	case 4: // Low Voltage
		voltage = voltage / 2
	case 5: // High Voltage
		voltage = voltage * 2
	case 6: // Frequency issue
		power = int(float64(power) * 0.8)
	case 7: // Overload
		power = power * 2
		temp = temp + 50
	case 8: // Communication Error
		// No change to values
	case 9: // Hardware Fault
		power = 0
		voltage = 0
	case 10: // System Shutdown
		power = 0
		voltage = 0
		temp = 0
	}
	return voltage, power, temp
}

// updateFaultStats updates fault statistics - anlayis the fault data inserted for future api's
func updateFaultStats(faultCode int) {
	faultStatMutex.Lock()
	faultStats[faultCode]++
	faultStatMutex.Unlock()
}

// PrintFaultSummary prints fault statistics summary
func PrintFaultSummary() {
	fmt.Println("⚠️  Fault Code Summary:")
	fmt.Println("   =====================")

	faultStatMutex.Lock()
	defer faultStatMutex.Unlock()

	for code := 0; code <= 10; code++ {
		count := faultStats[code]
		if count > 0 {
			info := models.FaultCodes[code]
			fmt.Printf("   [%d] %s: %d occurrences (%s)\n",
				code, info.Name, count, info.Severity)
		}
	}
	fmt.Println()
}

// FlushBatch flushes the batch buffer to database
func FlushBatch(requestID string) {
	bufferMutex.Lock()
	if len(batchBuffer) == 0 {
		logger.WriteLog(constants.LOG_LEVEL_DEBUG, requestID, "FLUSH", "No data to flush — buffer empty")
		bufferMutex.Unlock()
		return
	}

	toInsert := make([]interface{}, len(batchBuffer))
	copy(toInsert, batchBuffer)
	batchBuffer = batchBuffer[:0]
	bufferMutex.Unlock()

	logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "FLUSH",
		fmt.Sprintf("Flushing %d records to MongoDB...", len(toInsert)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := options.InsertMany().SetOrdered(false)
	result, err := collection.InsertMany(ctx, toInsert, opts)

	if err != nil {
		inserted := 0
		if result != nil {
			inserted = len(result.InsertedIDs)
		}
		failed := len(toInsert) - inserted
		atomic.AddInt64(&insertedCount, int64(inserted)) //safely updated counters (no race condition)
		atomic.AddInt64(&failedCount, int64(failed))
		logger.WriteLog(constants.LOG_LEVEL_ERROR,requestID, "FLUSH",
			fmt.Sprintf("Batch insert failed — Inserted: %d, Failed: %d, Error: %v",
				inserted, failed, err))
	} else {
		atomic.AddInt64(&insertedCount, int64(len(toInsert)))
		logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "FLUSH",
			fmt.Sprintf("Batch inserted successfully: %d records", len(toInsert)))
	}
}

// PeriodicBatchFlush periodically flushes batches
func PeriodicBatchFlush() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		FlushBatch("") // Empty requestID for periodic flushes
	}
}

// ReportStats reports statistics periodically
func ReportStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastCount := int64(0)
	lastTime := time.Now()

	for range ticker.C {
		currentCount := atomic.LoadInt64(&insertedCount)
		currentTime := time.Now()

		elapsed := currentTime.Sub(lastTime).Seconds()
		rps := float64(currentCount-lastCount) / elapsed

		// Count recent faults
		faultStatMutex.Lock()
		faultCount := int64(0)
		for code := 1; code <= 10; code++ {
			faultCount += faultStats[code]
		}
		faultStatMutex.Unlock()

		fmt.Printf(" Total=%d | Failed=%d | RPS=%.0f | Faults=%d\n",
			currentCount, atomic.LoadInt64(&failedCount), rps, faultCount)

		lastCount = currentCount
		lastTime = currentTime
	}
}

// GetInsertedCount returns the inserted count
func GetInsertedCount() int64 {
	return atomic.LoadInt64(&insertedCount)
}

// GetFailedCount returns the failed count
func GetFailedCount() int64 {
	return atomic.LoadInt64(&failedCount)
}

// GetBufferSize returns the current buffer size
func GetBufferSize() int {
	bufferMutex.Lock()
	defer bufferMutex.Unlock()
	return len(batchBuffer)
}