package routes

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"solar_project/config"
	"solar_project/models"
	"solar_project/services"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoHandler handles MongoDB-specific routes
type MongoHandler struct{}

// NewMongoHandler creates a new MongoDB route handler
func NewMongoHandler() *MongoHandler {
	return &MongoHandler{}
}

// GetStats returns insertion statistics from MongoDB including raw data
func (m *MongoHandler) GetStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetMongoCollection()
	client := config.GetMongoClient()
	dbName := getEnv("DB_NAME", "solar_monitoring")
	rawCollection := client.Database(dbName).Collection("raw_data")

	// Get processed data counts
	totalCount, _ := collection.CountDocuments(ctx, bson.D{})
	faultCount, _ := collection.CountDocuments(ctx, bson.D{
		{Key: "data.fault_code", Value: bson.D{{Key: "$gt", Value: 0}}},
	})
	normalCount, _ := collection.CountDocuments(ctx, bson.D{
		{Key: "data.fault_code", Value: 0},
	})

	// ✅ Get raw data counts
	rawTotalCount, _ := rawCollection.CountDocuments(ctx, bson.D{})
	rawProcessedCount, _ := rawCollection.CountDocuments(ctx, bson.D{
		{Key: "processed", Value: true},
	})
	rawPendingCount, _ := rawCollection.CountDocuments(ctx, bson.D{
		{Key: "processed", Value: false},
	})
	rawErrorCount, _ := rawCollection.CountDocuments(ctx, bson.D{
		{Key: "mapping_error", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$ne", Value: ""}}},
	})

	c.JSON(http.StatusOK, gin.H{
		"database":       "MongoDB",
		"total_records":  totalCount,
		"normal_records": normalCount,
		"fault_records":  faultCount,
		"inserted_count": services.GetInsertedCount(),
		"failed_count":   services.GetFailedCount(),
		"buffer_size":    services.GetBufferSize(),
		"success_rate":   calculateSuccessRate(services.GetInsertedCount(), services.GetFailedCount()),

		// ✅ NEW: Raw data statistics
		"raw_data": gin.H{
			"total":     rawTotalCount,
			"processed": rawProcessedCount,
			"pending":   rawPendingCount,
			"errors":    rawErrorCount,
		},
		"received_count":  services.GetReceivedCount(),
		"processed_count": services.GetProcessedCount(),
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetAllData returns paginated records from MongoDB
func (m *MongoHandler) GetAllData(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	page := getIntParam(c, "page", 1)
	limit := getIntParam(c, "limit", 100)
	skip := (page - 1) * limit

	collection := config.GetMongoCollection()

	// Get total count
	total, _ := collection.CountDocuments(ctx, bson.D{})

	// Get paginated data
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))

	cursor, err := collection.Find(ctx, bson.D{}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data"})
		return
	}
	defer cursor.Close(ctx)

	var results []models.InverterData
	cursor.All(ctx, &results)

	totalPages := (int(total) + limit - 1) / limit

	c.JSON(http.StatusOK, gin.H{
		"count":       len(results),
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,
		"has_next":    page < totalPages,
		"has_prev":    page > 1,
		"data":        results,
	})
}

// GetDataByFaultCode returns data filtered by fault code from MongoDB
func (m *MongoHandler) GetDataByFaultCode(c *gin.Context) {
	codeStr := c.Query("code")
	if codeStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code parameter required"})
		return
	}

	code, err := strconv.Atoi(codeStr)
	if err != nil || code < 0 || code > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fault code (0-10)"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetMongoCollection()
	filter := bson.D{{Key: "data.fault_code", Value: code}}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(50)

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data"})
		return
	}
	defer cursor.Close(ctx)

	var results []models.InverterData
	cursor.All(ctx, &results)

	c.JSON(http.StatusOK, gin.H{
		"fault_code": code,
		"fault_info": models.FaultCodes[code],
		"count":      len(results),
		"data":       results,
	})
}

// GetFaultStats returns aggregated fault statistics from MongoDB
func (m *MongoHandler) GetFaultStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetMongoCollection()

	// Aggregate fault counts
	pipeline := []bson.D{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$data.fault_code"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to aggregate"})
		return
	}
	defer cursor.Close(ctx)

	var results []struct {
		Code  int `bson:"_id"`
		Count int `bson:"count"`
	}
	cursor.All(ctx, &results)

	// Enrich with fault info
	enrichedStats := make([]gin.H, 0)
	for _, stat := range results {
		info := models.FaultCodes[stat.Code]
		enrichedStats = append(enrichedStats, gin.H{
			"code":        stat.Code,
			"name":        info.Name,
			"count":       stat.Count,
			"severity":    info.Severity,
			"description": info.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total_codes": len(results),
		"statistics":  enrichedStats,
	})
}

// GetActiveFaults returns only records with active faults from MongoDB
func (m *MongoHandler) GetActiveFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetMongoCollection()
	filter := bson.D{{Key: "data.fault_code", Value: bson.D{{Key: "$gt", Value: 0}}}}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(100)

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data"})
		return
	}
	defer cursor.Close(ctx)

	var results []models.InverterData
	cursor.All(ctx, &results)

	c.JSON(http.StatusOK, gin.H{
		"count":  len(results),
		"faults": results,
	})
}

// GetLatestFaults returns the latest fault records grouped by code from MongoDB
func (m *MongoHandler) GetLatestFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetMongoCollection()
	filter := bson.D{{Key: "data.fault_code", Value: bson.D{{Key: "$ne", Value: 0}}}}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(50)

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data"})
		return
	}
	defer cursor.Close(ctx)

	var results []models.InverterData
	cursor.All(ctx, &results)

	// Group by fault code
	faultGroups := make(map[int][]models.InverterData)
	for _, result := range results {
		code := result.Data.FaultCode
		faultGroups[code] = append(faultGroups[code], result)
	}

	c.JSON(http.StatusOK, gin.H{
		"total_faults": len(results),
		"by_code":      faultGroups,
	})
}

// Helper function to get integer parameters
func getIntParam(c *gin.Context, key string, defaultValue int) int {
	if value := c.Query(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil && intValue > 0 {
			return intValue
		}
	}
	return defaultValue
}
