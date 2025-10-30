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

// InfluxHandler handles InfluxDB-specific routes
type InfluxHandler struct {
	client   interface{}
	database string
}

// NewInfluxHandler creates a new InfluxDB route handler
func NewInfluxHandler() *InfluxHandler {
	return &InfluxHandler{
		client:   config.GetInfluxClient(),
		database: config.GetInfluxDatabase(),
	}
}

// GetStats returns insertion statistics from InfluxDB
func (h *InfluxHandler) GetStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := config.GetInfluxClient()

	// Query total count
	totalQuery := fmt.Sprintf(`
		SELECT COUNT(voltage) 
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

	// Query fault count
	faultQuery := fmt.Sprintf(`
		SELECT COUNT(voltage) 
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
		"database":       "InfluxDB",
		"db_name":        h.database, // ✅ NOW USING database variable
		"total_records":  totalCount,
		"normal_records": normalCount,
		"fault_records":  faultCount,
		"inserted_count": services.GetInsertedCount(),
		"failed_count":   services.GetFailedCount(),
		"buffer_size":    services.GetBufferSize(),
		"success_rate":   calculateSuccessRate(services.GetInsertedCount(), services.GetFailedCount()),
	})
}

// GetAllData returns recent records from InfluxDB
func (h *InfluxHandler) GetAllData(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit := getIntParam(c, "limit", 100)

	client := config.GetInfluxClient()
	// ✅ NOW USING database variable in query context
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
		})
		return
	}

	var results []map[string]interface{}
	for iterator.Next() {
		results = append(results, iterator.Value())
	}

	c.JSON(http.StatusOK, gin.H{
		"database": h.database, // ✅ USING database variable
		"count":    len(results),
		"data":     results,
	})
}

// GetDataByFaultCode returns data filtered by fault code from InfluxDB
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
			"database": h.database, // ✅ USING database variable
		})
		return
	}

	var results []map[string]interface{}
	for iterator.Next() {
		results = append(results, iterator.Value())
	}

	c.JSON(http.StatusOK, gin.H{
		"database":   h.database, // ✅ USING database variable
		"fault_code": code,
		"fault_info": models.FaultCodes[code],
		"count":      len(results),
		"data":       results,
	})
}

// GetFaultStats returns aggregated fault statistics from InfluxDB
func (h *InfluxHandler) GetFaultStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()
	query := `
		SELECT fault_code, COUNT(voltage) as count
		FROM inverter_data
		GROUP BY fault_code
		ORDER BY fault_code
	`

	iterator, err := client.Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "Failed to aggregate",
			"database": h.database, // ✅ USING database variable
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
		"database":    h.database, // ✅ USING database variable
		"total_codes": len(enrichedStats),
		"statistics":  enrichedStats,
	})
}

// GetActiveFaults returns only records with active faults from InfluxDB
func (h *InfluxHandler) GetActiveFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()
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
			"database": h.database, // ✅ USING database variable
		})
		return
	}

	var results []map[string]interface{}
	for iterator.Next() {
		results = append(results, iterator.Value())
	}

	c.JSON(http.StatusOK, gin.H{
		"database": h.database, // ✅ USING database variable
		"count":    len(results),
		"faults":   results,
	})
}

// GetLatestFaults returns the latest fault records from InfluxDB
func (h *InfluxHandler) GetLatestFaults(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := config.GetInfluxClient()
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
			"database": h.database, // ✅ USING database variable
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
		"database":     h.database, // ✅ USING database variable
		"total_faults": totalFaults,
		"by_code":      faultGroups,
	})
}
