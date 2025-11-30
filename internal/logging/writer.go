package logging

import (
	"log/slog"
	"strings"
)

// Writer is an io.Writer implementation that forwards command output to slog.
type Writer struct {
	logger *slog.Logger
}

// NewWriter constructs a Writer bound to the provided logger.
func NewWriter(logger *slog.Logger) *Writer {
	return &Writer{logger: logger}
}

// Write logs the given bytes as a single line at info level.
func (w *Writer) Write(p []byte) (int, error) {
	if w.logger != nil {
		line := strings.TrimRight(string(p), "\n")
		if line != "" {
			w.logger.Info("command output", "line", line)
		}
	}
	return len(p), nil
}
