package tailer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nxadm/tail"
	"github.com/oicur0t/logl/pkg/models"
	"go.uber.org/zap"
)

// Watcher tails log files and sends lines to a channel
type Watcher struct {
	serviceNames map[string]string // filepath -> service name mapping
	hostname     string
	logFiles     []string
	stateFile    string
	logger       *zap.Logger
	lineChan     chan<- models.LogEntry
	state        map[string]*models.FileState
	stateMu      sync.RWMutex
}

// NewWatcher creates a new log file watcher
func NewWatcher(serviceNames map[string]string, hostname string, logFiles []string, stateFile string, logger *zap.Logger, lineChan chan<- models.LogEntry) *Watcher {
	return &Watcher{
		serviceNames: serviceNames,
		hostname:     hostname,
		logFiles:     logFiles,
		stateFile:    stateFile,
		logger:       logger,
		lineChan:     lineChan,
		state:        make(map[string]*models.FileState),
	}
}

// Start begins tailing all configured log files
func (w *Watcher) Start(ctx context.Context) error {
	// Load previous state
	if err := w.loadState(); err != nil {
		w.logger.Warn("Failed to load state, starting fresh", zap.Error(err))
	}

	// Start state saver goroutine
	go w.stateSaver(ctx)

	// Start a goroutine for each log file
	var wg sync.WaitGroup
	for _, logFile := range w.logFiles {
		wg.Add(1)
		go func(filepath string) {
			defer wg.Done()
			if err := w.tailFile(ctx, filepath); err != nil {
				w.logger.Error("Error tailing file", zap.String("file", filepath), zap.Error(err))
			}
		}(logFile)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Save state one last time before exiting
	if err := w.saveState(); err != nil {
		w.logger.Error("Failed to save final state", zap.Error(err))
	}

	return nil
}

// tailFile tails a single log file
func (w *Watcher) tailFile(ctx context.Context, filepath string) error {
	w.logger.Info("Starting to tail file", zap.String("file", filepath))

	// Configure tail
	config := tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true, // Use polling for better compatibility
		Location:  &tail.SeekInfo{Offset: 0, Whence: os.SEEK_END},
	}

	// If we have previous state, seek to that position
	w.stateMu.RLock()
	if state, exists := w.state[filepath]; exists {
		config.Location = &tail.SeekInfo{Offset: state.Offset, Whence: os.SEEK_SET}
		w.logger.Info("Resuming from saved position",
			zap.String("file", filepath),
			zap.Int64("offset", state.Offset))
	}
	w.stateMu.RUnlock()

	// Start tailing
	t, err := tail.TailFile(filepath, config)
	if err != nil {
		return fmt.Errorf("failed to tail file %s: %w", filepath, err)
	}
	defer t.Cleanup()

	var lineNumber int64
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Stopping tail of file", zap.String("file", filepath))
			return ctx.Err()

		case line, ok := <-t.Lines:
			if !ok {
				w.logger.Warn("Tail channel closed", zap.String("file", filepath))
				return nil
			}

			if line.Err != nil {
				w.logger.Error("Error reading line", zap.String("file", filepath), zap.Error(line.Err))
				continue
			}

			lineNumber++

			// Create log entry
			entry := models.LogEntry{
				ServiceName: w.serviceNames[filepath],
				Hostname:    w.hostname,
				FilePath:    filepath,
				Line:        line.Text,
				Timestamp:   time.Now(),
				LineNumber:  lineNumber,
			}

			// Send to batch channel (non-blocking with timeout)
			select {
			case w.lineChan <- entry:
				// Successfully sent
			case <-time.After(5 * time.Second):
				w.logger.Warn("Timeout sending line to batcher, dropping line",
					zap.String("file", filepath),
					zap.Int64("line_number", lineNumber))
			case <-ctx.Done():
				return ctx.Err()
			}

			// Update state
			offset, err := t.Tell()
			if err == nil {
				w.updateState(filepath, offset, lineNumber)
			}
		}
	}
}

// updateState updates the in-memory state for a file
func (w *Watcher) updateState(filepath string, offset int64, lineNumber int64) {
	w.stateMu.Lock()
	defer w.stateMu.Unlock()

	w.state[filepath] = &models.FileState{
		Offset:   offset,
		Inode:    0, // tail library doesn't expose inode easily
		LastRead: time.Now(),
	}
}

// stateSaver periodically saves state to disk
func (w *Watcher) stateSaver(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.saveState(); err != nil {
				w.logger.Error("Failed to save state", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// saveState saves the current state to disk
func (w *Watcher) saveState() error {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()

	data, err := json.MarshalIndent(w.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(w.stateFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	w.logger.Debug("State saved", zap.String("state_file", w.stateFile))
	return nil
}

// loadState loads the previous state from disk
func (w *Watcher) loadState() error {
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet, not an error
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	if err := json.Unmarshal(data, &w.state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	w.logger.Info("State loaded", zap.String("state_file", w.stateFile), zap.Int("files", len(w.state)))
	return nil
}
