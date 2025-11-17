package repository

import (
	"context"

	"solar_project/internal/config"
	"solar_project/internal/domain"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoRepo implements Repository for MongoDB
type MongoRepo struct {
	db *config.MongoDatabase
}

// NewMongoRepo creates a new MongoDB repository
func NewMongoRepo(db *config.MongoDatabase) *MongoRepo {
	return &MongoRepo{db: db}
}

// Insert writes records to MongoDB
func (r *MongoRepo) Insert(ctx context.Context, records []domain.InverterData) error {
	if len(records) == 0 {
		return nil
	}

	// Convert to interface slice
	docs := make([]interface{}, len(records))
	for i, record := range records {
		docs[i] = record
	}

	opts := options.InsertMany().SetOrdered(false)
	_, err := r.db.Collection.InsertMany(ctx, docs, opts)

	return err
}

// Query retrieves records based on filter
func (r *MongoRepo) Query(ctx context.Context, filter domain.QueryFilter) ([]domain.InverterData, error) {
	// Build query
	query := bson.M{}

	if filter.DeviceID != "" {
		query["device_id"] = filter.DeviceID
	}

	if filter.FaultCode != nil {
		if *filter.FaultCode == 0 {
			query["data.fault_code"] = 0
		} else {
			query["data.fault_code"] = bson.M{"$gt": 0}
		}
	}

	if filter.StartTime != nil {
		query["timestamp"] = bson.M{"$gte": *filter.StartTime}
	}

	if filter.EndTime != nil {
		if _, exists := query["timestamp"]; exists {
			query["timestamp"].(bson.M)["$lte"] = *filter.EndTime
		} else {
			query["timestamp"] = bson.M{"$lte": *filter.EndTime}
		}
	}

	// Build options
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}})

	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}

	if filter.Offset > 0 {
		opts.SetSkip(int64(filter.Offset))
	}

	// Execute query
	cursor, err := r.db.Collection.Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []domain.InverterData
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// Count returns number of records matching filter
func (r *MongoRepo) Count(ctx context.Context, filter domain.QueryFilter) (int64, error) {
	query := bson.M{}

	if filter.DeviceID != "" {
		query["device_id"] = filter.DeviceID
	}

	if filter.FaultCode != nil {
		if *filter.FaultCode == 0 {
			query["data.fault_code"] = 0
		} else {
			query["data.fault_code"] = bson.M{"$gt": 0}
		}
	}

	if filter.StartTime != nil {
		query["timestamp"] = bson.M{"$gte": *filter.StartTime}
	}

	if filter.EndTime != nil {
		if _, exists := query["timestamp"]; exists {
			query["timestamp"].(bson.M)["$lte"] = *filter.EndTime
		} else {
			query["timestamp"] = bson.M{"$lte": *filter.EndTime}
		}
	}

	return r.db.Collection.CountDocuments(ctx, query)
}

// Type returns database type
func (r *MongoRepo) Type() string {
	return "mongo"
}
