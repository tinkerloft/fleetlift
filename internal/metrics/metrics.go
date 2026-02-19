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
