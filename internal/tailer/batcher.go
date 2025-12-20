package tailer

import (
	"context"
	"sync"
	"time"

	"github.com/oicur0t/logl/pkg/models"
	"go.uber.org/zap"
)

// Batcher accumulates log entries and sends them in batches
type Batcher struct {
	serviceName string // Default service name for logging only
	maxSize     int
	maxWait     time.Duration
	logger      *zap.Logger
	sender      BatchSender

	lineChan chan models.LogEntry
	mu       sync.Mutex
	batches  map[string][]models.LogEntry // service name -> entries
}

// BatchSender is an interface for sending log batches
type BatchSender interface {
	SendBatch(ctx context.Context, batch models.LogBatch) error
}

// NewBatcher creates a new log batcher
func NewBatcher(serviceName string, maxSize int, maxWait time.Duration, queueSize int, logger *zap.Logger, sender BatchSender) *Batcher {
	return &Batcher{
		serviceName: serviceName,
		maxSize:     maxSize,
		maxWait:     maxWait,
		logger:      logger,
		sender:      sender,
		lineChan:    make(chan models.LogEntry, queueSize),
		batches:     make(map[string][]models.LogEntry),
	}
}

// GetLineChan returns the channel for receiving log entries
func (b *Batcher) GetLineChan() chan<- models.LogEntry {
	return b.lineChan
}

// Start begins the batching process
func (b *Batcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(b.maxWait)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush remaining entries before exiting
			if err := b.flush(ctx); err != nil {
				b.logger.Error("Failed to flush final batch", zap.Error(err))
			}
			return ctx.Err()

		case entry := <-b.lineChan:
			b.mu.Lock()
			serviceName := entry.ServiceName
			if _, exists := b.batches[serviceName]; !exists {
				b.batches[serviceName] = make([]models.LogEntry, 0, b.maxSize)
			}
			b.batches[serviceName] = append(b.batches[serviceName], entry)
			shouldFlush := len(b.batches[serviceName]) >= b.maxSize
			b.mu.Unlock()

			if shouldFlush {
				if err := b.flushService(ctx, serviceName); err != nil {
					b.logger.Error("Failed to flush batch", zap.Error(err), zap.String("service", serviceName))
				}
				ticker.Reset(b.maxWait)
			}

		case <-ticker.C:
			// Time threshold reached
			if err := b.flush(ctx); err != nil {
				b.logger.Error("Failed to flush batch on timer", zap.Error(err))
			}
		}
	}
}

// flush sends all current batches to the server
func (b *Batcher) flush(ctx context.Context) error {
	b.mu.Lock()
	services := make([]string, 0, len(b.batches))
	for serviceName := range b.batches {
		services = append(services, serviceName)
	}
	b.mu.Unlock()

	for _, serviceName := range services {
		if err := b.flushService(ctx, serviceName); err != nil {
			return err
		}
	}
	return nil
}

// flushService sends the batch for a specific service to the server
func (b *Batcher) flushService(ctx context.Context, serviceName string) error {
	b.mu.Lock()
	batch, exists := b.batches[serviceName]
	if !exists || len(batch) == 0 {
		b.mu.Unlock()
		return nil
	}

	// Create a copy of the batch for sending
	batchToSend := models.LogBatch{
		ServiceName: serviceName,
		Entries:     make([]models.LogEntry, len(batch)),
	}
	copy(batchToSend.Entries, batch)

	// Clear this service's batch
	b.batches[serviceName] = b.batches[serviceName][:0]
	b.mu.Unlock()

	b.logger.Debug("Flushing batch",
		zap.Int("size", len(batchToSend.Entries)),
		zap.String("service", serviceName))

	// Send the batch
	if err := b.sender.SendBatch(ctx, batchToSend); err != nil {
		b.logger.Error("Failed to send batch",
			zap.Error(err),
			zap.Int("size", len(batchToSend.Entries)),
			zap.String("service", serviceName))
		return err
	}

	b.logger.Info("Batch sent successfully",
		zap.Int("size", len(batchToSend.Entries)),
		zap.String("service", serviceName))

	return nil
}
