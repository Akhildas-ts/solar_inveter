// Add these to routes/raw_data.go or create a new file

package routes

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"solar_project/config"
)

// Add this to SetupRawDataRoutes function
func SetupRawDataRoutes(r *gin.Engine) {
	api := r.Group("/api/raw")
	{
		
		api.GET("/unknown", getRawDataUnknown) // ✅ NEW
		api.GET("/attention", getRawDataNeedingAttention) // ✅ NEW
		api.GET("/stats", getRawDataStats)
		
	}
}

// ✅ NEW: Get all data with unknown sources (no mapping found)
func getRawDataUnknown(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit := getIntParam(c, "limit", 100)

	client := config.GetMongoClient()
	dbName := getEnv("DB_NAME", "solar_monitoring")
	rawCollection := client.Database(dbName).Collection("raw_data")

	filter := bson.M{"unknown_source": true}
	opts := options.Find().
		SetSort(bson.D{{Key: "received_at", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := rawCollection.Find(ctx, filter, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch unknown source data"})
		return
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	cursor.All(ctx, &results)

	// Get count
	total, _ := rawCollection.CountDocuments(ctx, filter)

	c.JSON(http.StatusOK, gin.H{
		"count":   len(results),
		"total":   total,
		"data":    results,
		"message": "These records need mapping configuration",
		"action":  "Create a mapping via POST /api/mappings",
	})
}

// ✅ NEW: Get all data needing attention (errors or unknown)
func getRawDataNeedingAttention(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit := getIntParam(c, "limit", 100)

	client := config.GetMongoClient()
	dbName := getEnv("DB_NAME", "solar_monitoring")
	rawCollection := client.Database(dbName).Collection("raw_data")

	filter := bson.M{"needs_attention": true}
	opts := options.Find().
		SetSort(bson.D{{Key: "received_at", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := rawCollection.Find(ctx, filter, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data"})
		return
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	cursor.All(ctx, &results)

	// Group by error type
	unknownSources := 0
	mappingErrors := 0
	
	for _, result := range results {
		if unknown, ok := result["unknown_source"].(bool); ok && unknown {
			unknownSources++
		} else if mappingErr, ok := result["mapping_error"].(string); ok && mappingErr != "" {
			mappingErrors++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"count":          len(results),
		"unknown_source": unknownSources,
		"mapping_error":  mappingErrors,
		"data":           results,
	})
}

// Updated stats to include unknown sources
func getRawDataStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetMongoClient()
	dbName := getEnv("DB_NAME", "solar_monitoring")
	rawCollection := client.Database(dbName).Collection("raw_data")

	total, _ := rawCollection.CountDocuments(ctx, bson.D{})
	processed, _ := rawCollection.CountDocuments(ctx, bson.M{"processed": true})
	pending, _ := rawCollection.CountDocuments(ctx, bson.M{"processed": false})
	errors, _ := rawCollection.CountDocuments(ctx, bson.M{
		"mapping_error": bson.M{"$exists": true, "$ne": ""},
	})
	unknown, _ := rawCollection.CountDocuments(ctx, bson.M{"unknown_source": true}) // ✅ NEW
	needsAttention, _ := rawCollection.CountDocuments(ctx, bson.M{"needs_attention": true}) // ✅ NEW

	// Get oldest and newest records
	var oldest, newest map[string]interface{}
	
	optsOldest := options.FindOne().SetSort(bson.D{{Key: "received_at", Value: 1}})
	rawCollection.FindOne(ctx, bson.D{}, optsOldest).Decode(&oldest)
	
	optsNewest := options.FindOne().SetSort(bson.D{{Key: "received_at", Value: -1}})
	rawCollection.FindOne(ctx, bson.D{}, optsNewest).Decode(&newest)

	successRate := float64(0)
	if total > 0 {
		successRate = float64(processed) / float64(total) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"total_records":      total,
		"processed_records":  processed,
		"pending_records":    pending,
		"error_records":      errors,
		"unknown_sources":    unknown, // ✅ NEW
		"needs_attention":    needsAttention, // ✅ NEW
		"success_rate":       successRate,
		"oldest_record":      oldest,
		"newest_record":      newest,
	})
}