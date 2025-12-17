package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// HTTPServerConfig holds HTTP server settings
type HTTPServerConfig struct {
	ListenAddress   string        `mapstructure:"listen_address"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// MongoDBConfig holds MongoDB connection settings
type MongoDBConfig struct {
	URI                 string `mapstructure:"uri"`
	Database            string `mapstructure:"database"`
	CollectionPrefix    string `mapstructure:"collection_prefix"`
	CertificateKeyFile  string `mapstructure:"certificate_key_file"`
	Timeout             time.Duration `mapstructure:"timeout"`
	MaxPoolSize         int    `mapstructure:"max_pool_size"`
	TTLDays             int    `mapstructure:"ttl_days"`
}

// ServerMTLSConfig holds mTLS configuration for the server
type ServerMTLSConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	CACert     string `mapstructure:"ca_cert"`
	ServerCert string `mapstructure:"server_cert"`
	ServerKey  string `mapstructure:"server_key"`
	ClientAuth string `mapstructure:"client_auth"` // require, request, or none
}

// RateLimitConfig holds rate limiting settings
type RateLimitConfig struct {
	Enabled            bool `mapstructure:"enabled"`
	RequestsPerMinute  int  `mapstructure:"requests_per_minute"`
	Burst              int  `mapstructure:"burst"`
}

// ServerConfig represents the complete server configuration
type ServerConfig struct {
	Server       HTTPServerConfig  `mapstructure:"server"`
	MongoDB      MongoDBConfig     `mapstructure:"mongodb"`
	MTLS         ServerMTLSConfig  `mapstructure:"mtls"`
	RateLimiting RateLimitConfig   `mapstructure:"rate_limiting"`
	LogLevel     string            `mapstructure:"log_level"`
	LogFormat    string            `mapstructure:"log_format"`
}

// LoadServerConfig loads the server configuration from a file
func LoadServerConfig(configPath string) (*ServerConfig, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.AutomaticEnv()

	// Set defaults
	v.SetDefault("server.listen_address", "0.0.0.0:8443")
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "30s")
	v.SetDefault("server.shutdown_timeout", "30s")
	v.SetDefault("mongodb.database", "logl")
	v.SetDefault("mongodb.collection_prefix", "logs_")
	v.SetDefault("mongodb.timeout", "10s")
	v.SetDefault("mongodb.max_pool_size", 100)
	v.SetDefault("mongodb.ttl_days", 30)
	v.SetDefault("mtls.enabled", true)
	v.SetDefault("mtls.client_auth", "require")
	v.SetDefault("rate_limiting.enabled", false)
	v.SetDefault("rate_limiting.requests_per_minute", 1000)
	v.SetDefault("rate_limiting.burst", 100)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "json")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ServerConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if config.MongoDB.URI == "" {
		return nil, fmt.Errorf("mongodb.uri is required")
	}
	if config.MTLS.Enabled {
		if config.MTLS.CACert == "" || config.MTLS.ServerCert == "" || config.MTLS.ServerKey == "" {
			return nil, fmt.Errorf("mTLS certificates are required when mTLS is enabled")
		}
	}

	return &config, nil
}
