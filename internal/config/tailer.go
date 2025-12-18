package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

// LogFileConfig represents a single log file to tail
type LogFileConfig struct {
	Path    string `mapstructure:"path"`
	Enabled bool   `mapstructure:"enabled"`
}

// UpstreamServerConfig holds server connection settings
type UpstreamServerConfig struct {
	URL          string        `mapstructure:"url"`
	Timeout      time.Duration `mapstructure:"timeout"`
	MaxRetries   int           `mapstructure:"max_retries"`
	RetryBackoff time.Duration `mapstructure:"retry_backoff"`
}

// BatchingConfig holds batching configuration
type BatchingConfig struct {
	MaxSize   int           `mapstructure:"max_size"`
	MaxWait   time.Duration `mapstructure:"max_wait"`
	QueueSize int           `mapstructure:"queue_size"`
}

// MTLSConfig holds mTLS configuration
type MTLSConfig struct {
	CACert     string `mapstructure:"ca_cert"`
	ClientCert string `mapstructure:"client_cert"`
	ClientKey  string `mapstructure:"client_key"`
	ServerName string `mapstructure:"server_name"`
}

// TailerConfig represents the complete tailer configuration
type TailerConfig struct {
	ServiceName string                `mapstructure:"service_name"`
	Hostname    string                `mapstructure:"hostname"`
	LogFiles    []LogFileConfig       `mapstructure:"log_files"`
	Server      UpstreamServerConfig  `mapstructure:"server"`
	Batching    BatchingConfig        `mapstructure:"batching"`
	MTLS        MTLSConfig            `mapstructure:"mtls"`
	StateFile   string                `mapstructure:"state_file"`
	LogLevel    string                `mapstructure:"log_level"`
	LogFormat   string                `mapstructure:"log_format"`
}

// LoadTailerConfig loads the tailer configuration from a file
func LoadTailerConfig(configPath string) (*TailerConfig, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.AutomaticEnv()

	// Set defaults
	v.SetDefault("hostname", getHostname())
	v.SetDefault("server.timeout", "30s")
	v.SetDefault("server.max_retries", 5)
	v.SetDefault("server.retry_backoff", "1s")
	v.SetDefault("batching.max_size", 100)
	v.SetDefault("batching.max_wait", "5s")
	v.SetDefault("batching.queue_size", 1000)
	v.SetDefault("state_file", "/var/lib/logl/tailer-state.json")
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "json")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config TailerConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if config.ServiceName == "" {
		return nil, fmt.Errorf("service_name is required")
	}
	if config.Server.URL == "" {
		return nil, fmt.Errorf("server.url is required")
	}
	if len(config.LogFiles) == 0 {
		return nil, fmt.Errorf("at least one log file must be configured")
	}

	return &config, nil
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
