package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/oicur0t/logl/internal/config"
	"github.com/oicur0t/logl/internal/server"
	"github.com/oicur0t/logl/pkg/mtls"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	configPath := flag.String("config", "/etc/logl/server.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadServerConfig(*configPath)
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

	logger.Info("Starting logl-server",
		zap.String("listen", cfg.Server.ListenAddress),
		zap.String("database", cfg.MongoDB.Database))

	// Create MongoDB storage
	storage, err := server.NewStorage(
		cfg.MongoDB.URI,
		cfg.MongoDB.Database,
		cfg.MongoDB.CollectionPrefix,
		cfg.MongoDB.CertificateKeyFile,
		cfg.MongoDB.MaxPoolSize,
		cfg.MongoDB.TTLDays,
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to create storage", zap.Error(err))
	}

	// Create handler
	handler := server.NewHandler(storage, logger)

	// Create HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs/ingest", handler.IngestLogs)
	mux.HandleFunc("/v1/health", handler.Health)

	// Apply middleware
	var httpHandler http.Handler = mux
	httpHandler = server.RecoveryMiddleware(logger)(httpHandler)
	httpHandler = server.LoggingMiddleware(logger)(httpHandler)

	if cfg.MTLS.Enabled {
		httpHandler = server.MTLSMiddleware(logger)(httpHandler)
	}

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         cfg.Server.ListenAddress,
		Handler:      httpHandler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Load TLS configuration if mTLS is enabled
	if cfg.MTLS.Enabled {
		requireClientCert := cfg.MTLS.ClientAuth == "require"
		tlsConfig, err := mtls.LoadServerTLSConfig(
			cfg.MTLS.CACert,
			cfg.MTLS.ServerCert,
			cfg.MTLS.ServerKey,
			requireClientCert,
		)
		if err != nil {
			logger.Fatal("Failed to load TLS config", zap.Error(err))
		}
		httpServer.TLSConfig = tlsConfig
	}

	// Start server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server starting", zap.String("addr", cfg.Server.ListenAddress))

		if cfg.MTLS.Enabled {
			serverErrors <- httpServer.ListenAndServeTLS("", "") // Certs loaded via TLSConfig
		} else {
			serverErrors <- httpServer.ListenAndServe()
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErrors:
		logger.Fatal("Server error", zap.Error(err))

	case sig := <-sigChan:
		logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))

		// Graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("Server shutdown error", zap.Error(err))
			httpServer.Close()
		}

		// Close MongoDB connection
		if err := storage.Close(ctx); err != nil {
			logger.Error("Failed to close MongoDB connection", zap.Error(err))
		}

		logger.Info("Server stopped gracefully")
	}
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
