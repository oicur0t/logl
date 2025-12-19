package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/oicur0t/logl/pkg/models"
	"go.uber.org/zap"
)

// Handler handles HTTP requests
type Handler struct {
	storage *Storage
	parser  *LogParser
	logger  *zap.Logger
}

// NewHandler creates a new HTTP handler
func NewHandler(storage *Storage, parser *LogParser, logger *zap.Logger) *Handler {
	return &Handler{
		storage: storage,
		parser:  parser,
		logger:  logger,
	}
}

// IngestLogs handles log ingestion requests
func (h *Handler) IngestLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode the request body
	var batch models.LogBatch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate batch
	if batch.ServiceName == "" {
		http.Error(w, "service_name is required", http.StatusBadRequest)
		return
	}

	if len(batch.Entries) == 0 {
		http.Error(w, "entries cannot be empty", http.StatusBadRequest)
		return
	}

	h.logger.Debug("Received batch",
		zap.String("service", batch.ServiceName),
		zap.Int("entries", len(batch.Entries)))

	// Parse JSON logs if enabled
	for i := range batch.Entries {
		h.parser.ParseLogEntry(&batch.Entries[i])
	}

	// Insert into MongoDB
	if err := h.storage.InsertBatch(r.Context(), batch); err != nil {
		h.logger.Error("Failed to insert batch", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "success",
		"received": len(batch.Entries),
	})
}

// Health handles health check requests
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}
