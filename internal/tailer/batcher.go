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
	serviceName string
	maxSize     int
	maxWait     time.Duration
	logger      *zap.Logger
	sender      BatchSender

	lineChan chan models.LogEntry
	mu       sync.Mutex
	batch    []models.LogEntry
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
		batch:       make([]models.LogEntry, 0, maxSize),
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
			b.batch = append(b.batch, entry)
			shouldFlush := len(b.batch) >= b.maxSize
			b.mu.Unlock()

			if shouldFlush {
				if err := b.flush(ctx); err != nil {
					b.logger.Error("Failed to flush batch", zap.Error(err))
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

// flush sends the current batch to the server
func (b *Batcher) flush(ctx context.Context) error {
	b.mu.Lock()
	if len(b.batch) == 0 {
		b.mu.Unlock()
		return nil
	}

	// Create a copy of the batch for sending
	batchToSend := models.LogBatch{
		ServiceName: b.serviceName,
		Entries:     make([]models.LogEntry, len(b.batch)),
	}
	copy(batchToSend.Entries, b.batch)

	// Clear the current batch
	b.batch = b.batch[:0]
	b.mu.Unlock()

	b.logger.Debug("Flushing batch",
		zap.Int("size", len(batchToSend.Entries)),
		zap.String("service", b.serviceName))

	// Send the batch
	if err := b.sender.SendBatch(ctx, batchToSend); err != nil {
		b.logger.Error("Failed to send batch",
			zap.Error(err),
			zap.Int("size", len(batchToSend.Entries)))
		return err
	}

	b.logger.Info("Batch sent successfully",
		zap.Int("size", len(batchToSend.Entries)),
		zap.String("service", b.serviceName))

	return nil
}
