package config

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Database interface for operations
type Database interface {
	Close() error
	GetType() string
}

// MongoDatabase wraps MongoDB client
type MongoDatabase struct {
	Client     *mongo.Client
	Database   *mongo.Database
	Collection *mongo.Collection
}

// InfluxDatabase wraps InfluxDB v3 client
type InfluxDatabase struct {
	Client   *influxdb3.Client
	Database string
}

// InitDatabase creates appropriate database connection
func InitDatabase(cfg *Config) (Database, error) {
	switch cfg.DBType {
	case "mongo":
		return initMongo(cfg)
	case "influx":
		return initInflux(cfg)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.DBType)
	}
}

// MongoDB initialization
func initMongo(cfg *Config) (*MongoDatabase, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOpts := options.Client().
		ApplyURI(cfg.MongoURI).
		SetMaxPoolSize(50).
		SetMinPoolSize(10)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect failed: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping failed: %w", err)
	}

	database := client.Database(cfg.MongoDB)
	collection := database.Collection(cfg.MongoCollection)

	// Create indexes
	if err := createMongoIndexes(ctx, collection); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	fmt.Printf("✓ MongoDB connected: %s/%s\n", cfg.MongoDB, cfg.MongoCollection)

	return &MongoDatabase{
		Client:     client,
		Database:   database,
		Collection: collection,
	}, nil
}

func createMongoIndexes(ctx context.Context, col *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys: map[string]interface{}{"timestamp": -1},
		},
		{
			Keys: map[string]interface{}{"device_id": 1},
		},
		{
			Keys: map[string]interface{}{"data.fault_code": 1},
		},
	}

	_, err := col.Indexes().CreateMany(ctx, indexes)
	return err
}

func (m *MongoDatabase) Close() error {
	if m.Client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.Client.Disconnect(ctx)
	}
	return nil
}

func (m *MongoDatabase) GetType() string {
	return "mongo"
}

// InfluxDB initialization
func initInflux(cfg *Config) (*InfluxDatabase, error) {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     cfg.InfluxURL,
		Token:    cfg.InfluxToken,
		Database: cfg.InfluxDatabase,
		WriteOptions: &influxdb3.WriteOptions{
			DefaultTags: map[string]string{},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("influx init failed: %w", err)
	}

	fmt.Printf("✓ InfluxDB connected: %s\n", cfg.InfluxDatabase)

	return &InfluxDatabase{
		Client:   client,
		Database: cfg.InfluxDatabase,
	}, nil
}

func (i *InfluxDatabase) Close() error {
	if i.Client != nil {
		i.Client.Close()
	}
	return nil
}

func (i *InfluxDatabase) GetType() string {
	return "influx"
}