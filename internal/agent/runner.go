package agent

import "context"

// Event represents a streaming event from an agent runner.
type Event struct {
	Type    string         // "stdout" | "stderr" | "complete" | "error" | "needs_input"
	Content string
	Output  map[string]any // on "complete": structured output parsed from agent
}

// RunOpts configures an agent run.
type RunOpts struct {
	Prompt      string
	WorkDir     string
	MaxTurns    int
	Environment map[string]string
}

// Runner is the interface for pluggable agent runners.
type Runner interface {
	Name() string
	// Run executes the agent in the given sandbox (by ID) and streams events.
	// The channel is closed when the agent completes or errors.
	Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error)
	// Interrupt kills a running agent.
	Interrupt(ctx context.Context, sandboxID string) error
}
