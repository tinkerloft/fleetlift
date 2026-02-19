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
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"

	"github.com/tinkerloft/fleetlift/internal/metrics"
)

func TestRegister(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, metrics.RegisterWith(reg, m))

	// Seed vec metrics so they appear in Gather()
	m.ActivityDuration.WithLabelValues("seed", "success").Observe(0)
	m.ActivityTotal.WithLabelValues("seed", "success").Add(0)

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

func TestInterceptor_RecordsSuccessMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, metrics.RegisterWith(reg, m))

	i := metrics.NewInterceptor(m)

	called := false
	fakeNext := &fakeActivityInterceptor{
		fn: func(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
			called = true
			time.Sleep(5 * time.Millisecond)
			return "ok", nil
		},
	}

	actInterceptor := i.InterceptActivity(context.Background(), fakeNext)
	require.NoError(t, actInterceptor.Init(&fakeActivityOutbound{activityName: "TestActivity"}))

	result, err := actInterceptor.ExecuteActivity(context.Background(), &interceptor.ExecuteActivityInput{})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "ok", result)

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
	require.NoError(t, actInterceptor.Init(&fakeActivityOutbound{activityName: "FailActivity"}))

	_, err := actInterceptor.ExecuteActivity(context.Background(), &interceptor.ExecuteActivityInput{})
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

// Init overrides the base to avoid propagating to a nil Next.
func (f *fakeActivityInterceptor) Init(_ interceptor.ActivityOutboundInterceptor) error {
	return nil
}

func (f *fakeActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	return f.fn(ctx, in)
}

// fakeActivityOutbound is a mock ActivityOutboundInterceptor that returns a fixed activity name.
type fakeActivityOutbound struct {
	interceptor.ActivityOutboundInterceptorBase
	activityName string
}

func (f *fakeActivityOutbound) GetInfo(_ context.Context) activity.Info {
	return activity.Info{
		ActivityType: activity.Type{Name: f.activityName},
	}
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
