package reqlog

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// FileLogger writes request/response logs as JSON files to a directory.
type FileLogger struct {
	dir string
}

// NewFileLogger creates a FileLogger and ensures the directory exists.
func NewFileLogger(dir string) (*FileLogger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}
	return &FileLogger{dir: dir}, nil
}

// Log writes a Record as a JSON file. Failures are logged but do not block.
func (f *FileLogger) Log(r Record) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		slog.Warn("Failed to marshal request log", "request_id", r.RequestID, "error", err)
		return
	}

	route := strings.Trim(r.Route, "/")
	filename := route + "_" + r.Timestamp.Format("0102-150405.000") + "_" + r.RequestID + ".json"
	path := filepath.Join(f.dir, filename)

	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("Failed to write request log", "path", path, "error", err)
	}
}

// Close is a no-op for file-based logging.
func (f *FileLogger) Close() error { return nil }
