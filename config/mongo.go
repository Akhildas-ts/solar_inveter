package config

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDatabase implements the Database interface for MongoDB
type MongoDatabase struct {
	client     *mongo.Client
	collection *mongo.Collection
	connected  bool
}

// NewMongoDatabase creates a new MongoDB database instance
func NewMongoDatabase() *MongoDatabase {
	return &MongoDatabase{}
}

// Connect establishes connection to MongoDB
func (m *MongoDatabase) Connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")

	clientOptions := options.Client().
		ApplyURI(mongoURI).
		SetMaxPoolSize(50).
		SetMinPoolSize(10)

	var err error
	m.client, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	err = m.client.Ping(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	dbName := getEnv("DB_NAME", "solar_monitoring")
	collectionName := getEnv("MONGO_COLLECTION", "inverter_data")
	m.collection = m.client.Database(dbName).Collection(collectionName)
	m.connected = true

	fmt.Printf("  URI: %s\n", mongoURI)
	fmt.Printf("  Database: %s\n", dbName)
	fmt.Printf("  Collection: %s\n", collectionName)

	return nil
}

// Close closes the MongoDB connection
func (m *MongoDatabase) Close() {
	if m.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.client.Disconnect(ctx)
		m.connected = false
		fmt.Println("✓ MongoDB connection closed")
	}
}

// GetType returns the database type
func (m *MongoDatabase) GetType() DatabaseType {
	return MongoDB
}

// IsConnected returns connection status
func (m *MongoDatabase) IsConnected() bool {
	return m.connected
}

// GetClient returns the MongoDB client
func (m *MongoDatabase) GetClient() *mongo.Client {
	return m.client
}

// GetCollection returns the MongoDB collection
func (m *MongoDatabase) GetCollection() *mongo.Collection {
	return m.collection
}

// ✅ Helper function to get MongoDB client (works for both data and mappings)
func GetMongoClient() *mongo.Client {
	// Try mappings DB first
	if mongoDB := GetMongoDBForMappings(); mongoDB != nil {
		return mongoDB.GetClient()
	}
	
	// Fallback to activeDB if it's MongoDB
	if activeDB != nil {
		if mdb, ok := activeDB.(*MongoDatabase); ok {
			return mdb.GetClient()
		}
	}
	
	return nil
}

// ✅ Helper function to get MongoDB collection from activeDB only
func GetMongoCollection() *mongo.Collection {
	if activeDB == nil {
		return nil
	}
	if mongoDB, ok := activeDB.(*MongoDatabase); ok {
		return mongoDB.GetCollection()
	}
	return nil
}