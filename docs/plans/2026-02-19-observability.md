# Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Prometheus metrics, structured slog logging, a Grafana dashboard, and alerting rules for operational visibility of both the Fleetlift worker and API server.

**Architecture:** A `MetricsInterceptor` implements the Temporal `WorkerInterceptor` interface to auto-instrument every activity (duration histogram + total counter) without touching individual activity files. The worker exposes `/metrics` on `:9090`. The API server (`cmd/server`) exposes `/metrics` on its existing port alongside the REST API, using HTTP request instrumentation middleware. Both binaries use their own `prometheus.Registry` (no shared global). The worker's own log output is migrated from stdlib `log` to `slog` JSON; Temporal's internal logger is wired to a thin slog adapter. Log level is configurable via `LOG_LEVEL` env var (default `info`). Grafana dashboard JSON and Prometheus alerting rules are static deploy artifacts.

**Tech Stack:** `github.com/prometheus/client_golang` v1, `go.temporal.io/sdk/interceptor`, stdlib `log/slog` (Go 1.21+), Grafana dashboard JSON, Prometheus alerting YAML.

**Branch:** `feat/observability`

**Progress:**
- Task 1: Prometheus metric definitions — ✅ Complete
- Task 2: Activity interceptor — ✅ Complete
- Task 3: Wire metrics into worker + API server — ✅ Complete
- Task 4: slog structured logging + Temporal adapter — ✅ Complete
- Task 5: Grafana dashboard + alerting rules + docs — ✅ Complete

---

## Task 1: Prometheus metric definitions

**Files:**
- Create: `internal/metrics/metrics.go`
- Create: `internal/metrics/metrics_test.go`

**Step 1: Add prometheus dependency**

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
```

**Step 2: Write the failing test**

`internal/metrics/metrics_test.go`:
```go
package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/metrics"
)

func TestRegister(t *testing.T) {
	reg := prometheus.NewRegistry()
	require.NoError(t, metrics.Register(reg))

	mfs, err := reg.Gather()
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	assert.True(t, names["fleetlift_activity_duration_seconds"])
	assert.True(t, names["fleetlift_activity_total"])
	assert.True(t, names["fleetlift_prs_created_total"])
	assert.True(t, names["fleetlift_sandbox_provision_seconds"])
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./internal/metrics/... -run TestRegister -v
```
Expected: FAIL (package doesn't exist)

**Step 4: Create `internal/metrics/metrics.go`**

```go
// Package metrics defines Prometheus metrics for the Fleetlift worker.
package metrics

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all registered Prometheus collectors.
type Metrics struct {
	ActivityDuration         *prometheus.HistogramVec
	ActivityTotal            *prometheus.CounterVec
	PRsCreatedTotal          prometheus.Counter
	SandboxProvisionDuration prometheus.Histogram
}

// Register registers all metrics with the given registry and returns the Metrics instance.
func Register(reg prometheus.Registerer) error {
	m := New()
	collectors := []prometheus.Collector{
		m.ActivityDuration,
		m.ActivityTotal,
		m.PRsCreatedTotal,
		m.SandboxProvisionDuration,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// New creates uninitialised metric instances (used internally and by interceptor).
func New() *Metrics {
	return &Metrics{
		ActivityDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "fleetlift_activity_duration_seconds",
				Help:    "Duration of each Temporal activity execution in seconds.",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
			},
			[]string{"activity_name", "result"},
		),
		ActivityTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "fleetlift_activity_total",
				Help: "Total number of Temporal activity executions by name and result.",
			},
			[]string{"activity_name", "result"},
		),
		PRsCreatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "fleetlift_prs_created_total",
			Help: "Total number of pull requests successfully created.",
		}),
		SandboxProvisionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "fleetlift_sandbox_provision_seconds",
			Help:    "Duration of sandbox provisioning in seconds.",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		}),
	}
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/metrics/... -run TestRegister -v
```
Expected: PASS

**Step 6: Commit**

```bash
git add internal/metrics/ go.mod go.sum
git commit -m "feat(metrics): add Prometheus metric definitions"
```

---

## Task 2: Activity interceptor

**Files:**
- Create: `internal/metrics/interceptor.go`
- Modify: `internal/metrics/metrics_test.go` (add interceptor test)

**Step 1: Write the failing test** (add to `metrics_test.go`)

```go
package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/internal"
	"github.com/tinkerloft/fleetlift/internal/metrics"
)

func TestInterceptor_RecordsSuccessMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, metrics.RegisterWith(reg, m))

	i := metrics.NewInterceptor(m)

	// Simulate a successful activity execution via the interceptor
	called := false
	fakeNext := &fakeActivityInterceptor{
		fn: func(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
			called = true
			time.Sleep(5 * time.Millisecond) // small delay so duration > 0
			return "ok", nil
		},
	}

	actInterceptor := i.InterceptActivity(context.Background(), fakeNext)
	actInterceptor.Init(&interceptor.ActivityOutboundInterceptorBase{})

	ctx := newFakeActivityContext("TestActivity")
	result, err := actInterceptor.ExecuteActivity(ctx, &interceptor.ExecuteActivityInput{})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "ok", result)

	// Verify counter incremented
	mfs, err := reg.Gather()
	require.NoError(t, err)
	total := findCounter(mfs, "fleetlift_activity_total", "activity_name", "TestActivity", "result", "success")
	assert.Equal(t, float64(1), total)
}

func TestInterceptor_RecordsFailureMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, metrics.RegisterWith(reg, m))

	i := metrics.NewInterceptor(m)
	fakeNext := &fakeActivityInterceptor{
		fn: func(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
			return nil, errors.New("boom")
		},
	}

	actInterceptor := i.InterceptActivity(context.Background(), fakeNext)
	actInterceptor.Init(&interceptor.ActivityOutboundInterceptorBase{})

	ctx := newFakeActivityContext("FailActivity")
	_, err := actInterceptor.ExecuteActivity(ctx, &interceptor.ExecuteActivityInput{})
	require.Error(t, err)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	total := findCounter(mfs, "fleetlift_activity_total", "activity_name", "FailActivity", "result", "failure")
	assert.Equal(t, float64(1), total)
}

// --- helpers ---

type fakeActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	fn func(context.Context, *interceptor.ExecuteActivityInput) (interface{}, error)
}

func (f *fakeActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	return f.fn(ctx, in)
}

func newFakeActivityContext(activityName string) context.Context {
	// inject a fake activity.Info into context using the internal test helper
	return internal.WithActivityInfo(context.Background(), internal.ActivityInfo{
		ActivityType: internal.ActivityType{Name: activityName},
	})
}

func findCounter(mfs []*dto.MetricFamily, name string, labelPairs ...string) float64 {
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchLabels(m, labelPairs) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func matchLabels(m *dto.Metric, pairs []string) bool {
	labels := make(map[string]string)
	for _, lp := range m.GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		if labels[pairs[i]] != pairs[i+1] {
			return false
		}
	}
	return true
}
```

Also update imports in test file to include `dto "github.com/prometheus/client_model/go"`, `"context"`, `"errors"`, `"time"`, `"go.temporal.io/sdk/interceptor"`, `"go.temporal.io/sdk/internal"`.

**Step 2: Add `RegisterWith` to `metrics.go`**

The test uses `metrics.RegisterWith(reg, m)` instead of `metrics.Register(reg)` so it can inject a pre-built `*Metrics`. Add this function to `metrics.go`:

```go
// RegisterWith registers a pre-built Metrics instance with the given registry.
func RegisterWith(reg prometheus.Registerer, m *Metrics) error {
	collectors := []prometheus.Collector{
		m.ActivityDuration,
		m.ActivityTotal,
		m.PRsCreatedTotal,
		m.SandboxProvisionDuration,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./internal/metrics/... -run "TestInterceptor" -v
```
Expected: FAIL (interceptor types don't exist yet)

**Step 4: Create `internal/metrics/interceptor.go`**

```go
package metrics

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"

	internalactivity "github.com/tinkerloft/fleetlift/internal/activity"
)

// Interceptor is a Temporal WorkerInterceptor that records Prometheus metrics for every activity.
type Interceptor struct {
	interceptor.WorkerInterceptorBase
	m *Metrics
}

// NewInterceptor creates a new metrics interceptor using the given Metrics.
func NewInterceptor(m *Metrics) *Interceptor {
	return &Interceptor{m: m}
}

// InterceptActivity wraps each activity execution to record duration and outcome metrics.
func (i *Interceptor) InterceptActivity(ctx context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &activityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
		m:                              i.m,
	}
}

type activityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	m *Metrics
}

func (a *activityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	info := activity.GetInfo(ctx)
	name := info.ActivityType.Name
	start := time.Now()

	result, err := a.Next.ExecuteActivity(ctx, in)

	duration := time.Since(start).Seconds()
	outcome := "success"
	if err != nil {
		outcome = "failure"
	}

	a.m.ActivityDuration.WithLabelValues(name, outcome).Observe(duration)
	a.m.ActivityTotal.WithLabelValues(name, outcome).Inc()

	// Per-metric tracking for specific activities
	if err == nil {
		switch name {
		case internalactivity.ActivityProvisionSandbox, internalactivity.ActivityProvisionAgentSandbox:
			a.m.SandboxProvisionDuration.Observe(duration)
		case internalactivity.ActivityCreatePullRequest:
			a.m.PRsCreatedTotal.Inc()
		}
	}

	return result, err
}
```

**Step 5: Run tests**

```bash
go test ./internal/metrics/... -v
```
Expected: all PASS

**Step 6: Commit**

```bash
git add internal/metrics/interceptor.go internal/metrics/metrics.go internal/metrics/metrics_test.go
git commit -m "feat(metrics): add Temporal activity interceptor for automatic instrumentation"
```

---

## Task 3: Wire metrics into worker + API server

**Files:**
- Modify: `cmd/worker/main.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `cmd/server/main.go`

### 3a: Worker metrics

**Step 1: Update `cmd/worker/main.go`**

Add imports:
```go
import (
    "net/http"
    // ... existing imports ...
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "go.temporal.io/sdk/interceptor"
    "github.com/tinkerloft/fleetlift/internal/metrics"
)
```

In `main()`, before connecting to Temporal, add metric setup:

```go
// Set up Prometheus metrics
reg := prometheus.NewRegistry()
reg.MustRegister(prometheus.NewGoCollector())
reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
m := metrics.New()
if err := metrics.RegisterWith(reg, m); err != nil {
    log.Fatalf("Failed to register metrics: %v", err)
}

// Expose /metrics on a dedicated port
metricsAddr := getEnvOrDefault("METRICS_ADDR", ":9090")
go func() {
    mux := http.NewServeMux()
    mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
    log.Printf("Metrics server listening on %s/metrics", metricsAddr)
    if err := http.ListenAndServe(metricsAddr, mux); err != nil {
        log.Printf("Metrics server error: %v", err)
    }
}()
```

Change `worker.New(...)` to pass the interceptor:

```go
w := worker.New(c, internalclient.TaskQueue, worker.Options{
    Interceptors: []interceptor.WorkerInterceptor{metrics.NewInterceptor(m)},
})
```

**Step 2: Build to verify**

```bash
go build ./cmd/worker/...
```
Expected: no errors

**Step 3: Commit**

```bash
git add cmd/worker/main.go
git commit -m "feat(worker): expose Prometheus /metrics on :9090 and wire activity interceptor"
```

### 3b: API server metrics

The API server embeds `/metrics` in its existing chi router. `server.New` gains an optional `prometheus.Gatherer` parameter — pass `nil` to use `prometheus.DefaultGatherer` (keeps tests unchanged).

**Step 1: Write the failing test** (add to `server_test.go`)

```go
func TestMetricsEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	s := server.New(&mockClient{}, nil, reg)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}
```

Add `"github.com/prometheus/client_golang/prometheus"` to test imports.

**Step 2: Run to verify failure**

```bash
go test ./internal/server/... -run TestMetricsEndpoint -v
```
Expected: FAIL (server.New takes 2 args)

**Step 3: Update `internal/server/server.go`**

Add field and update `New`:
```go
import (
    // ... existing ...
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
    router   chi.Router
    client   TemporalClient
    staticFS fs.FS
    gatherer prometheus.Gatherer // nil → prometheus.DefaultGatherer
}

func New(client TemporalClient, staticFS fs.FS, gatherer prometheus.Gatherer) *Server {
    s := &Server{client: client, staticFS: staticFS, gatherer: gatherer}
    s.router = s.buildRouter()
    return s
}
```

Add `/metrics` route in `buildRouter()`, before the static handler:
```go
// Metrics endpoint
g := s.gatherer
if g == nil {
    g = prometheus.DefaultGatherer
}
r.Get("/metrics", promhttp.HandlerFor(g, promhttp.HandlerOpts{}).ServeHTTP)
```

**Step 4: Fix all existing `server.New(...)` calls to pass a third arg**

In `internal/server/server_test.go`: replace every `server.New(mc, nil)` and `server.New(&mockClient{}, nil)` with `server.New(mc, nil, nil)` / `server.New(&mockClient{}, nil, nil)`.

```bash
# Count occurrences to patch
grep -n "server\.New(" internal/server/server_test.go
```

Use search-and-replace to update all of them.

**Step 5: Update `cmd/server/main.go`**

```go
import (
    // ... existing ...
    "github.com/prometheus/client_golang/prometheus"
)

// In main(), before server.New:
reg := prometheus.NewRegistry()
reg.MustRegister(prometheus.NewGoCollector())
reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

s := server.New(c, webFS, reg)
```

**Step 6: Run all tests**

```bash
go test ./internal/server/... -v
```
Expected: all PASS including `TestMetricsEndpoint`

**Step 7: Build both binaries**

```bash
go build ./cmd/worker/... && go build ./cmd/server/...
```
Expected: no errors

**Step 8: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go cmd/server/main.go
git commit -m "feat(server): expose /metrics endpoint with per-binary Prometheus registry"
```

---

## Task 4: slog structured logging + Temporal adapter

**Files:**
- Create: `internal/logging/slog_adapter.go`
- Create: `internal/logging/slog_adapter_test.go`
- Modify: `cmd/worker/main.go`

**Step 1: Write the failing test**

`internal/logging/slog_adapter_test.go`:
```go
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
	// If keyvals has odd length, adapter must not panic
	var buf bytes.Buffer
	sl := slog.New(slog.NewJSONHandler(&buf, nil))
	adapter := logging.NewSlogAdapter(sl)
	assert.NotPanics(t, func() { adapter.Info("odd", "key") })
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/logging/... -run TestSlogAdapter -v
```
Expected: FAIL (package doesn't exist)

**Step 3: Create `internal/logging/slog_adapter.go`**

```go
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
```

**Step 4: Run tests**

```bash
go test ./internal/logging/... -v
```
Expected: all PASS

**Step 5: Wire slog into `cmd/worker/main.go`**

Add imports:
```go
import (
    "log/slog"
    "os"
    // ... existing ...
    "go.temporal.io/sdk/client"
    "github.com/tinkerloft/fleetlift/internal/logging"
)
```

At the very top of `main()`, before any other setup:
```go
// Structured JSON logging
sl := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
slog.SetDefault(sl)
temporalLogger := logging.NewSlogAdapter(sl)
```

Replace all `log.Printf(...)` / `log.Fatalf(...)` in `main()` with `slog.Info(...)` / `slog.Error(...)` etc.

Replace `client.Dial(client.Options{HostPort: temporalAddr})` with:
```go
c, err := client.Dial(client.Options{
    HostPort: temporalAddr,
    Logger:   temporalLogger,
})
```

**Step 6: Build to verify**

```bash
go build ./cmd/worker/...
```
Expected: no errors

**Step 7: Run all tests**

```bash
go test ./...
```
Expected: all PASS

**Step 8: Commit**

```bash
git add internal/logging/ cmd/worker/main.go
git commit -m "feat(logging): add slog adapter for Temporal logger and use slog in worker"
```

---

## Task 5: Grafana dashboard + alerting rules + docs

**Files:**
- Create: `deploy/grafana/fleetlift-dashboard.json`
- Create: `deploy/prometheus/alerts.yaml`
- Modify: `docs/plans/IMPLEMENTATION_PLAN.md`
- Modify: `docs/plans/2026-02-19-observability.md` (this file — mark complete)

**Step 1: Create `deploy/prometheus/alerts.yaml`**

```yaml
groups:
  - name: fleetlift
    interval: 60s
    rules:
      - alert: HighActivityFailureRate
        expr: |
          sum(rate(fleetlift_activity_total{result="failure"}[5m])) /
          sum(rate(fleetlift_activity_total[5m])) > 0.1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High Fleetlift activity failure rate"
          description: "More than 10% of activities are failing over the last 5 minutes."

      - alert: SandboxProvisioningSlow
        expr: |
          histogram_quantile(0.95, rate(fleetlift_sandbox_provision_seconds_bucket[10m])) > 120
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Slow sandbox provisioning"
          description: "p95 sandbox provision time exceeds 2 minutes."

      - alert: NoActivitiesRunning
        expr: |
          sum(rate(fleetlift_activity_total[10m])) == 0
        for: 30m
        labels:
          severity: info
        annotations:
          summary: "No Fleetlift activity in 30 minutes"
          description: "No activities have executed in the past 30 minutes — worker may be idle or down."
```

**Step 2: Create `deploy/grafana/fleetlift-dashboard.json`**

```json
{
  "title": "Fleetlift Worker",
  "uid": "fleetlift-worker",
  "schemaVersion": 38,
  "refresh": "30s",
  "panels": [
    {
      "id": 1,
      "title": "Activity Rate (req/s)",
      "type": "timeseries",
      "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8},
      "targets": [
        {
          "expr": "sum(rate(fleetlift_activity_total[1m])) by (activity_name)",
          "legendFormat": "{{activity_name}}"
        }
      ]
    },
    {
      "id": 2,
      "title": "Activity Success Rate",
      "type": "stat",
      "gridPos": {"x": 12, "y": 0, "w": 12, "h": 8},
      "targets": [
        {
          "expr": "sum(rate(fleetlift_activity_total{result=\"success\"}[5m])) / sum(rate(fleetlift_activity_total[5m]))",
          "legendFormat": "Success Rate"
        }
      ],
      "options": {"reduceOptions": {"calcs": ["lastNotNull"]}, "orientation": "auto"},
      "fieldConfig": {"defaults": {"unit": "percentunit", "thresholds": {"steps": [
        {"value": null, "color": "red"},
        {"value": 0.9, "color": "yellow"},
        {"value": 0.99, "color": "green"}
      ]}}}
    },
    {
      "id": 3,
      "title": "Activity Duration p95 (seconds)",
      "type": "timeseries",
      "gridPos": {"x": 0, "y": 8, "w": 12, "h": 8},
      "targets": [
        {
          "expr": "histogram_quantile(0.95, sum(rate(fleetlift_activity_duration_seconds_bucket[5m])) by (le, activity_name))",
          "legendFormat": "p95 {{activity_name}}"
        }
      ]
    },
    {
      "id": 4,
      "title": "Sandbox Provision Duration p95 (seconds)",
      "type": "stat",
      "gridPos": {"x": 12, "y": 8, "w": 12, "h": 8},
      "targets": [
        {
          "expr": "histogram_quantile(0.95, rate(fleetlift_sandbox_provision_seconds_bucket[10m]))",
          "legendFormat": "p95"
        }
      ],
      "fieldConfig": {"defaults": {"unit": "s"}}
    },
    {
      "id": 5,
      "title": "PRs Created (total)",
      "type": "stat",
      "gridPos": {"x": 0, "y": 16, "w": 8, "h": 6},
      "targets": [{"expr": "fleetlift_prs_created_total", "legendFormat": "PRs Created"}],
      "fieldConfig": {"defaults": {"unit": "short"}}
    },
    {
      "id": 6,
      "title": "Go Goroutines",
      "type": "timeseries",
      "gridPos": {"x": 8, "y": 16, "w": 16, "h": 6},
      "targets": [{"expr": "go_goroutines", "legendFormat": "goroutines"}]
    }
  ]
}
```

**Step 3: Update `docs/plans/IMPLEMENTATION_PLAN.md`**

Find the Phase 7 section and tick all checkboxes. Find the summary table row for Phase 7 and change `⬜ Not started` to `✅ Complete`.

**Step 4: Update this plan file** — mark all tasks complete in the Progress section at the top.

**Step 5: Run lint and tests**

```bash
make lint
go test ./...
```
Expected: all PASS

**Step 6: Commit**

```bash
git add deploy/ docs/plans/IMPLEMENTATION_PLAN.md docs/plans/2026-02-19-observability.md
git commit -m "feat(observability): add Grafana dashboard, Prometheus alerting rules, and mark Phase 7 complete"
```

---

## Quick Reference

| Component | Location | Purpose |
|-----------|----------|---------|
| Metric definitions | `internal/metrics/metrics.go` | All Prometheus metric instances |
| Activity interceptor | `internal/metrics/interceptor.go` | Auto-instrument every Temporal activity |
| slog Temporal adapter | `internal/logging/slog_adapter.go` | Bridge slog → Temporal logger interface |
| Worker metrics | `cmd/worker/main.go` → `:9090/metrics` | Prometheus scrape target (activity + process) |
| Server metrics | `internal/server/server.go` → `/metrics` | Prometheus scrape target (process + Go) |
| Grafana dashboard | `deploy/grafana/fleetlift-dashboard.json` | Import via Grafana UI |
| Alerting rules | `deploy/prometheus/alerts.yaml` | Load via Prometheus Operator or `rule_files:` |

| Env var | Default | Purpose |
|---------|---------|---------|
| `METRICS_ADDR` | `:9090` | Address the worker exposes `/metrics` on (separate port) |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
