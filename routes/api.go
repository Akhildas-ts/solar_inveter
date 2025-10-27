package routes

import (
	"net/http"

	"solar_project/models"
	"solar_project/services"

	"github.com/gin-gonic/gin"
)

// SetupRoutes configures all API routes
func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		// api.GET("/all", getAllData)  // Temporarily disabled - needs InfluxDB 3.0 query implementation
		api.GET("/stats", getStats)
		api.POST("/data", services.GenerateHandler)

		faults := api.Group("/faults")
		{
			faults.GET("/list", getFaultCodeList)
			// Temporarily disabled - needs InfluxDB 3.0 query implementation
			// faults.GET("/data", getDataByFaultCode)
			// faults.GET("/stats", getFaultStats)
			// faults.GET("/active", getActiveFaults)
			// faults.GET("/latest", getLatestFaults)
		}
	}
}

// getStats returns insertion statistics
func getStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message":        "Solar Monitoring System with InfluxDB 3.0",
		"inserted_count": services.GetInsertedCount(),
		"failed_count":   services.GetFailedCount(),
		"buffer_size":    services.GetBufferSize(),
		"status":         "running",
		"database":       "InfluxDB 3.0",
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
