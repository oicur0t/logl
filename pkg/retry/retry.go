package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Config holds retry configuration
type Config struct {
	MaxRetries  int
	InitialWait time.Duration
	MaxWait     time.Duration
	Multiplier  float64
}

// DefaultConfig returns a sensible default retry configuration
func DefaultConfig() Config {
	return Config{
		MaxRetries:  5,
		InitialWait: 1 * time.Second,
		MaxWait:     60 * time.Second,
		Multiplier:  2.0,
	}
}

// Do executes the given function with exponential backoff retry logic
func Do(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Execute the function
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Don't wait after the last attempt
		if attempt == cfg.MaxRetries {
			break
		}

		// Calculate exponential backoff with jitter
		waitTime := calculateBackoff(attempt, cfg)

		// Wait with context cancellation support
		select {
		case <-time.After(waitTime):
			// Continue to next attempt
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return lastErr
}

// calculateBackoff calculates the backoff duration with exponential backoff and jitter
func calculateBackoff(attempt int, cfg Config) time.Duration {
	// Exponential backoff: initialWait * multiplier^attempt
	backoff := float64(cfg.InitialWait) * math.Pow(cfg.Multiplier, float64(attempt))

	// Cap at max wait time
	if backoff > float64(cfg.MaxWait) {
		backoff = float64(cfg.MaxWait)
	}

	// Add jitter (±25%)
	jitter := backoff * 0.25 * (rand.Float64()*2 - 1)
	backoff += jitter

	// Ensure minimum of initial wait time
	if backoff < float64(cfg.InitialWait) {
		backoff = float64(cfg.InitialWait)
	}

	return time.Duration(backoff)
}
