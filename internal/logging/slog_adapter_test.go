package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/logging"
)

func TestSlogAdapter_Info(t *testing.T) {
	var buf bytes.Buffer
	sl := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	adapter := logging.NewSlogAdapter(sl)

	adapter.Info("hello world", "key", "value", "count", 42)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "hello world", entry["msg"])
	assert.Equal(t, "value", entry["key"])
	assert.Equal(t, float64(42), entry["count"])
	assert.Equal(t, "INFO", entry["level"])
}

func TestSlogAdapter_Error(t *testing.T) {
	var buf bytes.Buffer
	sl := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	adapter := logging.NewSlogAdapter(sl)

	adapter.Error("something failed", "error", "boom")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "ERROR", entry["level"])
}

func TestSlogAdapter_OddKeyvals(t *testing.T) {
	var buf bytes.Buffer
	sl := slog.New(slog.NewJSONHandler(&buf, nil))
	adapter := logging.NewSlogAdapter(sl)
	assert.NotPanics(t, func() { adapter.Info("odd", "key") })
}
