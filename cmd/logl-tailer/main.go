package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oicur0t/logl/internal/config"
	"github.com/oicur0t/logl/internal/tailer"
	"github.com/oicur0t/logl/pkg/mtls"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	configPath := flag.String("config", "/etc/logl/tailer.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadTailerConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger, err := initLogger(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting logl-tailer",
		zap.String("service", cfg.ServiceName),
		zap.String("hostname", cfg.Hostname),
		zap.Int("log_files", len(cfg.LogFiles)))

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))
		cancel()

		// Give 30 seconds for graceful shutdown
		time.Sleep(30 * time.Second)
		logger.Error("Forced shutdown after timeout")
		os.Exit(1)
	}()

	// Load mTLS configuration
	tlsConfig, err := mtls.LoadClientTLSConfig(
		cfg.MTLS.CACert,
		cfg.MTLS.ClientCert,
		cfg.MTLS.ClientKey,
		cfg.MTLS.ServerName,
	)
	if err != nil {
		logger.Fatal("Failed to load mTLS config", zap.Error(err))
	}

	// Create HTTP client
	httpClient := tailer.NewClient(
		cfg.Server.URL,
		tlsConfig,
		cfg.Server.Timeout,
		cfg.Server.MaxRetries,
		logger,
	)

	// Create batcher
	batcher := tailer.NewBatcher(
		cfg.ServiceName,
		cfg.Batching.MaxSize,
		cfg.Batching.MaxWait,
		cfg.Batching.QueueSize,
		logger,
		httpClient,
	)

	// Get enabled log files
	var enabledLogFiles []string
	for _, lf := range cfg.LogFiles {
		if lf.Enabled {
			enabledLogFiles = append(enabledLogFiles, lf.Path)
		}
	}

	if len(enabledLogFiles) == 0 {
		logger.Fatal("No enabled log files configured")
	}

	// Create watcher
	watcher := tailer.NewWatcher(
		cfg.ServiceName,
		cfg.Hostname,
		enabledLogFiles,
		cfg.StateFile,
		logger,
		batcher.GetLineChan(),
	)

	// Start batcher in background
	go func() {
		if err := batcher.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("Batcher failed", zap.Error(err))
		}
	}()

	// Start watcher (blocks until context is cancelled)
	if err := watcher.Start(ctx); err != nil && err != context.Canceled {
		logger.Error("Watcher failed", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("Tailer stopped gracefully")
}

// initLogger creates a configured zap logger
func initLogger(level string, format string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	var loggerConfig zap.Config
	if format == "json" {
		loggerConfig = zap.NewProductionConfig()
	} else {
		loggerConfig = zap.NewDevelopmentConfig()
	}

	loggerConfig.Level = zap.NewAtomicLevelAt(zapLevel)

	return loggerConfig.Build()
}
