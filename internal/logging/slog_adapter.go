// Package logging provides utilities for structured logging.
package logging

import (
	"log/slog"
)

// SlogAdapter adapts a *slog.Logger to satisfy go.temporal.io/sdk/log.Logger.
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a Temporal-compatible logger backed by the given *slog.Logger.
func NewSlogAdapter(l *slog.Logger) *SlogAdapter {
	return &SlogAdapter{logger: l}
}

func (s *SlogAdapter) Debug(msg string, keyvals ...interface{}) {
	s.logger.Debug(msg, toAttrs(keyvals)...)
}

func (s *SlogAdapter) Info(msg string, keyvals ...interface{}) {
	s.logger.Info(msg, toAttrs(keyvals)...)
}

func (s *SlogAdapter) Warn(msg string, keyvals ...interface{}) {
	s.logger.Warn(msg, toAttrs(keyvals)...)
}

func (s *SlogAdapter) Error(msg string, keyvals ...interface{}) {
	s.logger.Error(msg, toAttrs(keyvals)...)
}

// toAttrs converts alternating key-value pairs to slog.Attr args.
func toAttrs(keyvals []interface{}) []any {
	if len(keyvals) == 0 {
		return nil
	}
	attrs := make([]any, 0, len(keyvals))
	for i := 0; i+1 < len(keyvals); i += 2 {
		key, _ := keyvals[i].(string)
		attrs = append(attrs, slog.Any(key, keyvals[i+1]))
	}
	// Handle odd-length keyvals gracefully
	if len(keyvals)%2 != 0 {
		attrs = append(attrs, slog.Any("MISSING_VALUE", keyvals[len(keyvals)-1]))
	}
	return attrs
}
