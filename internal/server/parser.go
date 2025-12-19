package server

import (
	"encoding/json"

	"github.com/oicur0t/logl/internal/config"
	"github.com/oicur0t/logl/pkg/models"
	"go.uber.org/zap"
)

// LogParser handles parsing of log entries
type LogParser struct {
	config config.JSONParsingConfig
	logger *zap.Logger
}

// NewLogParser creates a new log parser
func NewLogParser(config config.JSONParsingConfig, logger *zap.Logger) *LogParser {
	return &LogParser{
		config: config,
		logger: logger,
	}
}

// ParseLogEntry attempts to parse a log entry's line as JSON
// If parsing succeeds, it populates the Parsed field
// If parsing fails or is disabled, the entry is left unchanged
func (p *LogParser) ParseLogEntry(entry *models.LogEntry) {
	if !p.config.Enabled {
		return
	}

	// Try to parse the line as JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(entry.Line), &parsed); err != nil {
		// Not valid JSON - this is fine, just skip parsing
		// Don't log errors as many logs won't be JSON (nginx, etc.)
		return
	}

	// Successfully parsed - store the parsed data
	entry.Parsed = parsed
}
