package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Level represents the configured log level.
type Level string

const (
	LevelOff   Level = "off"
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// ParseLevel converts a string to a Level, defaulting to LevelOff.
func ParseLevel(s string) Level {
	switch Level(strings.ToLower(s)) {
	case LevelDebug:
		return LevelDebug
	case LevelInfo:
		return LevelInfo
	case LevelWarn:
		return LevelWarn
	case LevelError:
		return LevelError
	default:
		return LevelOff
	}
}

func toSlogLevel(l Level) slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelError + 1 // effectively disables all logging
	}
}

// Setup configures the global slog logger. When level is "off", logging is
// discarded. Otherwise, logs are written to ~/.config/jenking/debug.log.
// Returns a cleanup function that closes the log file.
func Setup(level Level) (cleanup func(), err error) {
	if level == LevelOff || level == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return func() {}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	logDir := filepath.Join(home, ".config", "jenking")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(logDir, "debug.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: toSlogLevel(level),
	})
	slog.SetDefault(slog.New(handler))

	slog.Info("logging initialized", "level", level, "file", logPath)

	return func() { f.Close() }, nil
}
