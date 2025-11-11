package routes

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"solar_project/config"
	"solar_project/models"
	"solar_project/services"

	"github.com/gin-gonic/gin"
)

// InfluxHandler handles LOCAL InfluxDB v3-specific routes
type InfluxHandler struct {
	client   interface{}
	database string
}

// NewInfluxHandler creates a new LOCAL InfluxDB v3 route handler
func NewInfluxHandler() *InfluxHandler {
	return &InfluxHandler{
		client:   config.GetInfluxClient(),
		database: config.GetInfluxDatabase(),
	}
}

// GetStats returns insertion statistics from LOCAL InfluxDB v3
func (h *InfluxHandler) GetStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()

	// ✅ InfluxDB v3 uses SQL queries!
	totalQuery := fmt.Sprintf(`
		SELECT COUNT(*) as count
		FROM inverter_data
	`)

	iterator, err := client.Query(ctx, totalQuery)
	totalCount := int64(0)
	if err == nil {
		for iterator.Next() {
			if val := iterator.Value(); val != nil {
				if count, ok := val["count"].(int64); ok {
					totalCount = count
				}
			}
		}
	}

	// ✅ Query fault count with SQL
	faultQuery := fmt.Sprintf(`
		SELECT COUNT(*) as count
		FROM inverter_data 
		WHERE fault_code > 0
	`)

	iterator2, err := client.Query(ctx, faultQuery)
	faultCount := int64(0)
	if err == nil {
		for iterator2.Next() {
			if val := iterator2.Value(); val != nil {
				if count, ok := val["count"].(int64); ok {
					faultCount = count
				}
			}
		}
	}

	normalCount := totalCount - faultCount

	c.JSON(http.StatusOK, gin.H{
		"database":       "InfluxDB v3 (Local)",
		"db_name":        h.database,
		"mode":           "LOCAL DOCKER",
		"total_records":  totalCount,
		"normal_records": normalCount,
		"fault_records":  faultCount,
		"inserted_count": services.GetInsertedCount(),
		"failed_count":   services.GetFailedCount(),
		"buffer_size":    services.GetBufferSize(),
		"success_rate":   calculateSuccessRate(services.GetInsertedCount(), services.GetFailedCount()),
	})
}

// GetAllData returns recent records from LOCAL InfluxDB v3
func (h *InfluxHandler) GetAllData(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit := getIntParam(c, "limit", 100)

	client := config.GetInfluxClient()
	
	// ✅ InfluxDB v3 SQL query
	query := fmt.Sprintf(`
		SELECT *
		FROM inverter_data
		ORDER BY time DESC
		LIMIT %d
	`, limit)

	iterator, err := client.Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "Failed to fetch data",
			"database": h.database,
			"detail":   err.Error(),
		})
		return
	}

	var results []map[string]interface{}
	for iterator.Next() {
		results = append(results, iterator.Value())
	}

	c.JSON(http.StatusOK, gin.H{
		"database": h.database,
		"mode":     "LOCAL",
		"count":    len(results),
		"data":     results,
	})
}

// GetDataByFaultCode returns data filtered by fault code from LOCAL InfluxDB v3
func (h *InfluxHandler) GetDataByFaultCode(c *gin.Context) {
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

	client := config.GetInfluxClient()
	
	// ✅ SQL query with WHERE clause
	query := fmt.Sprintf(`
		SELECT *
		FROM inverter_data
		WHERE fault_code = %d
		ORDER BY time DESC
		LIMIT 50
	`, code)

	iterator, err := client.Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "Failed to fetch data",
			"database": h.database,
		})
		return
	}

	var results []map[string]interface{}
	for iterator.Next() {
		results = append(results, iterator.Value())
	}

	c.JSON(http.StatusOK, gin.H{
		"database":   h.database,
		"mode":       "LOCAL",
		"fault_code": code,
		"fault_info": models.FaultCodes[code],
		"count":      len(results),
		"data":       results,
	})
}

// GetFaultStats returns aggregated fault statistics from LOCAL InfluxDB v3
func (h *InfluxHandler) GetFaultStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()
	
	// ✅ SQL GROUP BY query
	query := `
		SELECT fault_code, COUNT(*) as count
		FROM inverter_data
		GROUP BY fault_code
		ORDER BY fault_code
	`

	iterator, err := client.Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "Failed to aggregate",
			"database": h.database,
		})
		return
	}

	enrichedStats := make([]gin.H, 0)
	for iterator.Next() {
		val := iterator.Value()
		if faultCode, ok := val["fault_code"].(int64); ok {
			if count, ok := val["count"].(int64); ok {
				info := models.FaultCodes[int(faultCode)]
				enrichedStats = append(enrichedStats, gin.H{
					"code":        faultCode,
					"name":        info.Name,
					"count":       count,
					"severity":    info.Severity,
					"description": info.Description,
				})
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"database":    h.database,
		"mode":        "LOCAL",
		"total_codes": len(enrichedStats),
		"statistics":  enrichedStats,
	})
}

// GetActiveFaults returns only records with active faults from LOCAL InfluxDB v3
func (h *InfluxHandler) GetActiveFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()
	
	// ✅ SQL query with WHERE
	query := `
		SELECT *
		FROM inverter_data
		WHERE fault_code > 0
		ORDER BY time DESC
		LIMIT 100
	`

	iterator, err := client.Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "Failed to fetch data",
			"database": h.database,
		})
		return
	}

	var results []map[string]interface{}
	for iterator.Next() {
		results = append(results, iterator.Value())
	}

	c.JSON(http.StatusOK, gin.H{
		"database": h.database,
		"mode":     "LOCAL",
		"count":    len(results),
		"faults":   results,
	})
}

// GetLatestFaults returns the latest fault records from LOCAL InfluxDB v3
func (h *InfluxHandler) GetLatestFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()
	
	// ✅ SQL query
	query := `
		SELECT *
		FROM inverter_data
		WHERE fault_code != 0
		ORDER BY time DESC
		LIMIT 50
	`

	iterator, err := client.Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "Failed to fetch data",
			"database": h.database,
		})
		return
	}

	// Group by fault code
	faultGroups := make(map[int][]map[string]interface{})
	for iterator.Next() {
		val := iterator.Value()
		if faultCode, ok := val["fault_code"].(int64); ok {
			code := int(faultCode)
			faultGroups[code] = append(faultGroups[code], val)
		}
	}

	totalFaults := 0
	for _, faults := range faultGroups {
		totalFaults += len(faults)
	}

	c.JSON(http.StatusOK, gin.H{
		"database":     h.database,
		"mode":         "LOCAL",
		"total_faults": totalFaults,
		"by_code":      faultGroups,
	})
}