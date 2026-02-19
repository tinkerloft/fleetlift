package metrics

import (
	"context"
	"time"

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
	m        *Metrics
	outbound interceptor.ActivityOutboundInterceptor
}

// Init stores the outbound interceptor so ExecuteActivity can retrieve activity info.
func (a *activityInterceptor) Init(outbound interceptor.ActivityOutboundInterceptor) error {
	a.outbound = outbound
	return a.ActivityInboundInterceptorBase.Init(outbound)
}

func (a *activityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	name := ""
	if a.outbound != nil {
		info := a.outbound.GetInfo(ctx)
		name = info.ActivityType.Name
	}
	start := time.Now()

	result, err := a.ActivityInboundInterceptorBase.ExecuteActivity(ctx, in)

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
