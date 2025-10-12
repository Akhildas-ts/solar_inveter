package routes

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"solar_project/config"
	"solar_project/models"
	"solar_project/services"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SetupRoutes configures all API routes
func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		// Basic routes
		api.GET("/all", getAllData)
		api.GET("/stats", getStats)

		// Fault detection routes
		faults := api.Group("/faults")
		{
			faults.GET("/list", getFaultCodeList)
			faults.GET("/data", getDataByFaultCode)
			faults.GET("/stats", getFaultStats)
			faults.GET("/active", getActiveFaults)
			faults.GET("/latest", getLatestFaults)
		}
	}
}

// getStats returns insertion statistics
func getStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := config.GetCollection()
	count, _ := collection.CountDocuments(ctx, bson.D{})

	c.JSON(http.StatusOK, gin.H{
		"inserted_count":    services.GetInsertedCount(),
		"failed_count":      services.GetFailedCount(),
		"db_document_count": count,
		"buffer_size":       services.GetBufferSize(),
	})
}

// getAllData returns the latest 100 records
func getAllData(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetCollection()
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(100)
	cursor, err := collection.Find(ctx, bson.D{}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data"})
		return
	}
	defer cursor.Close(ctx)

	var results []models.InverterData
	cursor.All(ctx, &results)

	c.JSON(http.StatusOK, gin.H{
		"count": len(results),
		"data":  results,
	})
}

// getFaultCodeList returns all fault code definitions
func getFaultCodeList(c *gin.Context) {
	faultList := make([]models.FaultInfo, 0, len(models.FaultCodes))
	for _, info := range models.FaultCodes {
		faultList = append(faultList, info)
	}

	c.JSON(http.StatusOK, gin.H{
		"count":  len(faultList),
		"faults": faultList,
	})
}

// getDataByFaultCode returns data filtered by fault code
func getDataByFaultCode(c *gin.Context) {
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

	collection := config.GetCollection()
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

// getFaultStats returns aggregated fault statistics
func getFaultStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetCollection()

	// Aggregate fault counts from database
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

// getActiveFaults returns only records with active faults
func getActiveFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetCollection()

	// Get only non-zero fault codes
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

// getLatestFaults returns the latest 50 fault records grouped by code
func getLatestFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := config.GetCollection()

	// Get latest 50 records with faults
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
