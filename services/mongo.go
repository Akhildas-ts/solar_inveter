package services

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"solar_project/config"
	"solar_project/constants"
	"solar_project/logger"
	"solar_project/models"
)

// MongoWriter handles MongoDB write operations
type MongoWriter struct {
	collection *mongo.Collection
}

// NewMongoWriter creates a new MongoDB writer
func NewMongoWriter() *MongoWriter {
	return &MongoWriter{
		collection: config.GetMongoCollection(),
	}
}

// WriteData writes data to MongoDB
func (m *MongoWriter) WriteData(data []interface{}, requestID string) error {
	if len(data) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := options.InsertMany().SetOrdered(false)
	result, err := m.collection.InsertMany(ctx, data, opts)

	if err != nil {
		inserted := 0
		if result != nil {
			inserted = len(result.InsertedIDs)
		}
		logger.WriteLog(constants.LOG_LEVEL_ERROR, requestID, "MONGO_WRITE",
			fmt.Sprintf("Batch insert failed — Inserted: %d/%d, Error: %v",
				inserted, len(data), err))
		return err
	}

	logger.WriteLog(constants.LOG_LEVEL_INFO, requestID, "MONGO_WRITE",
		fmt.Sprintf("✅ Batch inserted successfully: %d records", len(data)))
	return nil
}

// ConvertToMongoRecord converts payload to MongoDB record
func ConvertToMongoRecord(payload InverterPayload) interface{} {
	return models.InverterData{
		DeviceType:     payload.DeviceType,
		DeviceName:     payload.DeviceName,
		DeviceID:       payload.DeviceID,
		Date:           payload.Date,
		Time:           payload.Time,
		Timestamp:      time.Now(),
		SignalStrength: payload.SignalStrength,
		Data:           payload.Data,
	}
}
