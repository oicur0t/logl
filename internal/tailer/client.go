package tailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/oicur0t/logl/pkg/models"
	"github.com/oicur0t/logl/pkg/retry"
	"go.uber.org/zap"
)

// Client sends log batches to the server via HTTP
type Client struct {
	serverURL   string
	httpClient  *http.Client
	logger      *zap.Logger
	retryConfig retry.Config
	circuitBreaker *CircuitBreaker
}

// CircuitBreaker prevents overwhelming a failing server
type CircuitBreaker struct {
	failures    int
	lastFailure time.Time
	threshold   int
	timeout     time.Duration
	mu          sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
	}
}

// isOpen checks if the circuit breaker is open (blocking requests)
func (cb *CircuitBreaker) isOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.failures >= cb.threshold && time.Since(cb.lastFailure) < cb.timeout {
		return true
	}

	// Reset if timeout has passed
	if time.Since(cb.lastFailure) >= cb.timeout {
		cb.failures = 0
	}

	return false
}

// recordSuccess resets the circuit breaker
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
}

// recordFailure increments the failure count
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = time.Now()
}

// NewClient creates a new HTTP client with mTLS
func NewClient(serverURL string, tlsConfig *tls.Config, timeout time.Duration, maxRetries int, logger *zap.Logger) *Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     tlsConfig,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: timeout,
	}

	return &Client{
		serverURL:  serverURL,
		httpClient: httpClient,
		logger:     logger,
		retryConfig: retry.Config{
			MaxRetries:  maxRetries,
			InitialWait: 1 * time.Second,
			MaxWait:     60 * time.Second,
			Multiplier:  2.0,
		},
		circuitBreaker: NewCircuitBreaker(5, 60*time.Second),
	}
}

// SendBatch sends a log batch to the server with retry logic
func (c *Client) SendBatch(ctx context.Context, batch models.LogBatch) error {
	// Check circuit breaker
	if c.circuitBreaker.isOpen() {
		return fmt.Errorf("circuit breaker is open, server may be down")
	}

	var lastErr error
	err := retry.Do(ctx, c.retryConfig, func() error {
		lastErr = c.sendRequest(ctx, batch)
		return lastErr
	})

	if err != nil {
		c.circuitBreaker.recordFailure()
		return err
	}

	c.circuitBreaker.recordSuccess()
	return nil
}

// sendRequest makes a single HTTP request to send the batch
func (c *Client) sendRequest(ctx context.Context, batch models.LogBatch) error {
	// Marshal batch to JSON
	jsonData, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("Request failed", zap.Error(err))
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 500 {
		// Server error - retry
		return fmt.Errorf("server error: %d", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		// Client error - don't retry
		c.logger.Error("Client error, not retrying",
			zap.Int("status_code", resp.StatusCode),
			zap.Int("batch_size", len(batch.Entries)))
		return nil // Don't retry 4xx errors
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	c.logger.Debug("Batch sent successfully",
		zap.Int("status_code", resp.StatusCode),
		zap.Int("batch_size", len(batch.Entries)))

	return nil
}
