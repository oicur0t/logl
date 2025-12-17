package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oicur0t/logl/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// Storage handles MongoDB operations
type Storage struct {
	client           *mongo.Client
	database         *mongo.Database
	collectionPrefix string
	logger           *zap.Logger
	ttlDays          int
}

// NewStorage creates a new MongoDB storage instance
func NewStorage(uri, database, collectionPrefix, certKeyFile string, maxPoolSize, ttlDays int, logger *zap.Logger) (*Storage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build connection options
	clientOpts := options.Client().ApplyURI(uri)

	// Set max pool size
	clientOpts.SetMaxPoolSize(uint64(maxPoolSize))

	// If certificate key file is provided, use X.509 authentication
	if certKeyFile != "" {
		// Update URI to include certificate path
		if strings.Contains(uri, "?") {
			uri = uri + "&tlsCertificateKeyFile=" + certKeyFile
		} else {
			uri = uri + "?tlsCertificateKeyFile=" + certKeyFile
		}

		// Add authentication mechanism
		clientOpts.SetAuth(options.Credential{
			AuthMechanism: "MONGODB-X509",
		})

		// Reapply URI with certificate
		clientOpts.ApplyURI(uri)
	}

	// Connect to MongoDB
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	logger.Info("Connected to MongoDB",
		zap.String("database", database),
		zap.Int("max_pool_size", maxPoolSize))

	return &Storage{
		client:           client,
		database:         client.Database(database),
		collectionPrefix: collectionPrefix,
		logger:           logger,
		ttlDays:          ttlDays,
	}, nil
}

// InsertBatch inserts a batch of log entries into MongoDB
func (s *Storage) InsertBatch(ctx context.Context, batch models.LogBatch) error {
	if len(batch.Entries) == 0 {
		return nil
	}

	// Get or create collection for this service
	collName := s.sanitizeCollectionName(batch.ServiceName)
	collection := s.database.Collection(collName)

	// Ensure indexes exist
	if err := s.ensureIndexes(ctx, collection); err != nil {
		s.logger.Error("Failed to ensure indexes", zap.Error(err), zap.String("collection", collName))
		// Don't fail the insert if index creation fails
	}

	// Convert to interface slice for bulk insert
	docs := make([]interface{}, len(batch.Entries))
	for i, entry := range batch.Entries {
		docs[i] = entry
	}

	// Bulk insert
	result, err := collection.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	if err != nil {
		// Check if it's a duplicate key error (which is fine for idempotency)
		if mongo.IsDuplicateKeyError(err) {
			s.logger.Warn("Duplicate key error, some documents already exist",
				zap.String("collection", collName),
				zap.Int("batch_size", len(batch.Entries)))
			return nil
		}
		return fmt.Errorf("failed to insert batch: %w", err)
	}

	s.logger.Info("Batch inserted",
		zap.String("collection", collName),
		zap.Int("inserted", len(result.InsertedIDs)),
		zap.String("service", batch.ServiceName))

	return nil
}

// ensureIndexes creates necessary indexes on a collection
func (s *Storage) ensureIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexModels := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
			Options: options.Index().SetName("timestamp_desc"),
		},
		{
			Keys: bson.D{
				{Key: "hostname", Value: 1},
				{Key: "timestamp", Value: -1},
			},
			Options: options.Index().SetName("hostname_timestamp"),
		},
	}

	// Add TTL index if configured
	if s.ttlDays > 0 {
		ttlSeconds := int32(s.ttlDays * 24 * 60 * 60)
		indexModels = append(indexModels, mongo.IndexModel{
			Keys: bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().
				SetName("ttl_index").
				SetExpireAfterSeconds(ttlSeconds),
		})
	}

	// Create indexes (this is idempotent)
	_, err := collection.Indexes().CreateMany(ctx, indexModels)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	return nil
}

// sanitizeCollectionName creates a valid collection name from service name
func (s *Storage) sanitizeCollectionName(serviceName string) string {
	// Convert to lowercase
	name := strings.ToLower(serviceName)

	// Replace invalid characters with underscore
	reg := regexp.MustCompile(`[^a-z0-9_]`)
	name = reg.ReplaceAllString(name, "_")

	// Add prefix
	return s.collectionPrefix + name
}

// Close closes the MongoDB connection
func (s *Storage) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}
