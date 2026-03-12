package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/workflow"
)

func TestProvisionSandbox_RejectsInvalidCredentialName(t *testing.T) {
	a := &Activities{
		Sandbox:   &noopSandbox{},
		CredStore: &stubCredStore{},
	}

	invalidNames := []string{
		"lower_case",
		"123START",
		"HAS SPACE",
		"HAS-DASH",
		"has.dot",
		"",
		"A" + string(make([]byte, 64)), // 65 chars total — too long
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			input := workflow.StepInput{
				TeamID: "team-1",
				ResolvedOpts: workflow.ResolvedStepOpts{
					Credentials: []string{name},
					Agent:       "claude-code",
				},
			}
			_, err := a.ProvisionSandbox(context.Background(), input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid credential name")
		})
	}
}

func TestProvisionSandbox_RejectsReservedEnvVar(t *testing.T) {
	a := &Activities{
		Sandbox:   &noopSandbox{},
		CredStore: &stubCredStore{},
	}

	reserved := []string{"PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "HOME", "USER", "SHELL"}
	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			input := workflow.StepInput{
				TeamID: "team-1",
				ResolvedOpts: workflow.ResolvedStepOpts{
					Credentials: []string{name},
					Agent:       "claude-code",
				},
			}
			_, err := a.ProvisionSandbox(context.Background(), input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "reserved environment variable")
		})
	}
}

func TestProvisionSandbox_AcceptsValidCredentialName(t *testing.T) {
	a := &Activities{
		Sandbox:   &noopSandbox{},
		CredStore: &stubCredStore{val: "secret"},
	}

	validNames := []string{
		"API_KEY",
		"MY_TOKEN",
		"A",
		"GITHUB_TOKEN",
		"X123",
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			input := workflow.StepInput{
				TeamID: "team-1",
				ResolvedOpts: workflow.ResolvedStepOpts{
					Credentials: []string{name},
					Agent:       "claude-code",
				},
			}
			_, err := a.ProvisionSandbox(context.Background(), input)
			// No validation error; may succeed or fail for other reasons (e.g. DB).
			if err != nil {
				assert.NotContains(t, err.Error(), "invalid credential name")
				assert.NotContains(t, err.Error(), "reserved environment variable")
			}
		})
	}
}

// stubCredStore returns a fixed value for any credential lookup.
type stubCredStore struct {
	val string
}

func (s *stubCredStore) Get(_ context.Context, _, _ string) (string, error) {
	return s.val, nil
}
