// Package logging provides helpers for structured, colorized logging across the application.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
)

// Level represents a structured log level used by codexctl.
type Level slog.Level

const (
	// LevelDebug represents the debug logging level.
	LevelDebug Level = Level(slog.LevelDebug)
	// LevelInfo represents the informational logging level.
	LevelInfo Level = Level(slog.LevelInfo)
	// LevelWarn represents the warning logging level.
	LevelWarn Level = Level(slog.LevelWarn)
	// LevelError represents the error logging level.
	LevelError Level = Level(slog.LevelError)
)

// ParseLevel converts a textual log level into a Level value.
func ParseLevel(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// NewLogger constructs a slog.Logger configured with a tint handler and level.
func NewLogger(w io.Writer, level Level) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	handler := tint.NewHandler(w, &tint.Options{
		Level: slog.Level(level),
	})

	return slog.New(handler)
}
