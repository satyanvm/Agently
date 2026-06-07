package platform

import (
	"log/slog"
	"os"
)

// Logger is a minimal structured logger interface; what code depends on, so the
// backing implementation (slog here) can be swapped. Mirrors
// packages/core/src/platform/logger.ts.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	// Child returns a logger that merges args into every line.
	Child(args ...any) Logger
}

type slogLogger struct{ l *slog.Logger }

func (s slogLogger) Debug(msg string, args ...any) { s.l.Debug(msg, args...) }
func (s slogLogger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s slogLogger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s slogLogger) Error(msg string, args ...any) { s.l.Error(msg, args...) }
func (s slogLogger) Child(args ...any) Logger      { return slogLogger{l: s.l.With(args...)} }

// NewLogger builds a logger. Pretty (text) output in dev, JSON in production,
// matching the TS logger's environment behavior.
func NewLogger(pretty bool, base ...any) Logger {
	level := slog.LevelInfo
	if pretty {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if pretty {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	l := slog.New(h)
	if len(base) > 0 {
		l = l.With(base...)
	}
	return slogLogger{l: l}
}
