package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LogEntry represents a single log line with metadata
type LogEntry struct {
	ID          primitive.ObjectID     `json:"id" bson:"_id,omitempty"`
	ServiceName string                 `json:"service_name" bson:"service_name"`
	Hostname    string                 `json:"hostname" bson:"hostname"`
	FilePath    string                 `json:"file_path" bson:"file_path"`
	Line        string                 `json:"line" bson:"line"`
	Timestamp   time.Time              `json:"timestamp" bson:"timestamp"`
	LineNumber  int64                  `json:"line_number" bson:"line_number"`
	Parsed      map[string]interface{} `json:"parsed,omitempty" bson:"parsed,omitempty"`
}

// LogBatch wraps multiple log entries for efficient transmission
type LogBatch struct {
	ServiceName string     `json:"service_name"`
	Entries     []LogEntry `json:"entries"`
}

// FileState tracks the reading position of a log file
type FileState struct {
	Offset   int64     `json:"offset"`
	Inode    uint64    `json:"inode"`
	LastRead time.Time `json:"last_read"`
}
