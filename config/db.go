package config

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	client     *mongo.Client
	collection *mongo.Collection
)

// InitDB initializes the MongoDB connection
func InitDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().
		ApplyURI("mongodb://localhost:27017").
		SetMaxPoolSize(50).
		SetMinPoolSize(10)

	var err error
	client, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		return err
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		return err
	}

	collection = client.Database("solar_monitoring").Collection("inverter_data")

	return nil
}

// GetClient returns the MongoDB client
func GetClient() *mongo.Client {
	return client
}

// GetCollection returns the inverter data collection
func GetCollection() *mongo.Collection {
	return collection
}

// CloseDB closes the MongoDB connection
func CloseDB() {
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client.Disconnect(ctx)
	}
}