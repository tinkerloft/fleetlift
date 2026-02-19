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
