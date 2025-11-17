// internal/repository/raw_repository.go
package repository

import (
	"context"
	"fmt"
	"time"

	"solar_project/internal/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RawData represents the original unprocessed data
type RawData struct {
	ID        primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	RequestID string                 `bson:"request_id,omitempty" json:"request_id,omitempty"`
	Data      map[string]interface{} `bson:"data" json:"data"`
	Timestamp time.Time              `bson:"timestamp" json:"timestamp"`
	SourceIP  string                 `bson:"source_ip,omitempty" json:"source_ip,omitempty"`
	Processed bool                   `bson:"processed" json:"processed"`
	Error     string                 `bson:"error,omitempty" json:"error,omitempty"`
}

// RawDataRepo handles raw data storage
type RawDataRepo struct {
	collection *mongo.Collection
}

// NewRawDataRepo creates a new raw data repository
func NewRawDataRepo(db *config.MongoDatabase) *RawDataRepo {
	collection := db.Database.Collection("raw_data")

	// Create indexes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "processed", Value: 1}},
		},
		{
			Keys: bson.D{
				{Key: "timestamp", Value: 1},
				{Key: "processed", Value: 1},
			},
		},
		{
			Keys: bson.D{{Key: "request_id", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetSparse(true),
		},
	}

	collection.Indexes().CreateMany(ctx, indexes)

	return &RawDataRepo{collection: collection}
}

// Insert saves raw data
func (r *RawDataRepo) Insert(ctx context.Context, data map[string]interface{}, sourceIP, requestID string) (string, error) {
	rawID := primitive.NewObjectID()
	if requestID == "" {
		requestID = rawID.Hex()
	}

	clonedData := cloneMap(data)
	clonedData["request_id"] = requestID

	rawData := RawData{
		ID:        rawID,
		RequestID: requestID,
		Data:      clonedData,
		Timestamp: time.Now(),
		SourceIP:  sourceIP,
		Processed: false,
	}

	if _, err := r.collection.InsertOne(ctx, rawData); err != nil {
		return "", err
	}

	return rawID.Hex(), nil
}

// MarkProcessed marks a raw data record as processed
func (r *RawDataRepo) MarkProcessed(ctx context.Context, id string) error {
	objectID, err := toObjectID(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"processed": true,
		},
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

// MarkError marks a raw data record with an error
func (r *RawDataRepo) MarkError(ctx context.Context, id string, errorMsg string) error {
	objectID, err := toObjectID(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"processed": false,
			"error":     errorMsg,
		},
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

// GetUnprocessed retrieves unprocessed raw data
func (r *RawDataRepo) GetUnprocessed(ctx context.Context, limit int) ([]RawData, error) {
	filter := bson.M{"processed": false}
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: 1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []RawData
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// Count returns total raw data records
func (r *RawDataRepo) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}

// CountUnprocessed returns count of unprocessed records
func (r *RawDataRepo) CountUnprocessed(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"processed": false})
}

// CountErrors returns count of records with errors
func (r *RawDataRepo) CountErrors(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{
		"error": bson.M{"$ne": ""},
	})
}

// Reprocess allows reprocessing failed records
func (r *RawDataRepo) Reprocess(ctx context.Context, id string) error {
	objectID, err := toObjectID(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"processed": false,
			"error":     "",
		},
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

// GetByTimeRange retrieves raw data within a time range
func (r *RawDataRepo) GetByTimeRange(ctx context.Context, start, end time.Time, limit int) ([]RawData, error) {
	filter := bson.M{
		"timestamp": bson.M{
			"$gte": start,
			"$lte": end,
		},
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []RawData
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// Cleanup removes old processed records (data retention)
func (r *RawDataRepo) Cleanup(ctx context.Context, olderThan time.Time) (int64, error) {
	filter := bson.M{
		"processed": true,
		"timestamp": bson.M{"$lt": olderThan},
	}

	result, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}

func toObjectID(id string) (primitive.ObjectID, error) {
	if id == "" {
		return primitive.NilObjectID, fmt.Errorf("raw data id cannot be empty")
	}

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("invalid raw data id %q: %w", id, err)
	}
	return objectID, nil
}

func cloneMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}

	cloned := make(map[string]interface{}, len(data))
	for k, v := range data {
		if nested, ok := v.(map[string]interface{}); ok {
			cloned[k] = cloneMap(nested)
			continue
		}
		cloned[k] = v
	}

	return cloned
}
