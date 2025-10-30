package routes

import (
	"net/http"

	"solar_project/config"
	"solar_project/models"
	"solar_project/services"

	"github.com/gin-gonic/gin"
)

// RouteHandler interface for database-specific routes
type RouteHandler interface {
	GetAllData(c *gin.Context)
	GetStats(c *gin.Context)
	
	GetDataByFaultCode(c *gin.Context)
	GetFaultStats(c *gin.Context)
	GetActiveFaults(c *gin.Context)
	GetLatestFaults(c *gin.Context)
}

var handler RouteHandler

// SetupRoutes configures all API routes
func SetupRoutes(r *gin.Engine) {
	// Initialize appropriate handler based on DB type
	switch config.GetDBType() {
	case config.MongoDB:
		handler = NewMongoHandler()
	case config.InfluxDB:
		handler = NewInfluxHandler()
	}

	api := r.Group("/api")
	{
		// Basic routes
		api.GET("/all", handler.GetAllData)
		api.GET("/stats", handler.GetStats)
		api.POST("/data", services.GenerateHandler)

		// Fault detection routes
		faults := api.Group("/faults")
		{
			faults.GET("/list", getFaultCodeList)
			faults.GET("/data", handler.GetDataByFaultCode)
			faults.GET("/stats", handler.GetFaultStats)
			faults.GET("/active", handler.GetActiveFaults)
			faults.GET("/latest", handler.GetLatestFaults)
		}
	}
}

// getFaultCodeList returns all fault code definitions (database-independent)
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

// Helper function to calculate success rate
func calculateSuccessRate(inserted, failed int64) float64 {
	total := inserted + failed
	if total == 0 {
		return 100.0
	}
	return float64(inserted) / float64(total) * 100.0
}
