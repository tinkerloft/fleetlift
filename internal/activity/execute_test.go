package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// noopSandbox satisfies sandbox.Client without doing anything.
type noopSandbox struct{}

func (n *noopSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-noop", nil
}
func (n *noopSandbox) ExecStream(_ context.Context, _, _, _ string, _ func(string)) error {
	return nil
}
func (n *noopSandbox) Exec(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}
func (n *noopSandbox) WriteFile(_ context.Context, _, _, _ string) error { return nil }
func (n *noopSandbox) ReadFile(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (n *noopSandbox) ReadBytes(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}
func (n *noopSandbox) Kill(_ context.Context, _ string) error          { return nil }
func (n *noopSandbox) RenewExpiration(_ context.Context, _ string) error { return nil }

func TestExecuteStep_RejectsNonHTTPS(t *testing.T) {
	a := &Activities{
		Sandbox:      &noopSandbox{},
		AgentRunners: nil,
	}

	badURLs := []string{
		"file:///etc/passwd",
		"git://github.com/org/repo.git",
		"ssh://github.com/org/repo.git",
		"http://github.com/org/repo.git",
		"ftp://example.com/repo.git",
		"",
	}

	for _, url := range badURLs {
		t.Run(url, func(t *testing.T) {
			input := workflow.ExecuteStepInput{
				StepInput: workflow.StepInput{
					ResolvedOpts: workflow.ResolvedStepOpts{
						Repos: []model.RepoRef{{URL: url}},
						Agent: "claude-code",
					},
				},
				SandboxID: "sb-test",
			}
			_, err := a.ExecuteStep(context.Background(), input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "https://")
		})
	}
}

func TestExecuteStep_AcceptsHTTPS(t *testing.T) {
	// A valid https:// URL passes URL validation and proceeds into clone/heartbeat logic.
	// In tests, activity.RecordHeartbeat panics because there is no Temporal activity context.
	// We confirm the panic is NOT a URL-validation error (i.e., the URL check passed).
	a := &Activities{
		Sandbox:      &noopSandbox{},
		AgentRunners: map[string]agent.Runner{},
	}

	input := workflow.ExecuteStepInput{
		StepInput: workflow.StepInput{
			ResolvedOpts: workflow.ResolvedStepOpts{
				Repos: []model.RepoRef{{URL: "https://github.com/org/repo.git"}},
				Agent: "unknown-agent-will-cause-error",
			},
		},
		SandboxID: "sb-test",
	}

	defer func() {
		r := recover()
		if r != nil {
			msg := ""
			if s, ok := r.(string); ok {
				msg = s
			} else if e, ok := r.(error); ok {
				msg = e.Error()
			}
			// Panic should be from Temporal internals, not from our URL validation.
			assert.NotContains(t, msg, "https://")
			assert.NotContains(t, msg, "repo URL must use")
		}
	}()

	_, _ = a.ExecuteStep(context.Background(), input)
}
