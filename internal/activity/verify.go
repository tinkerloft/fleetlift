package activity

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/sdk/activity"
)

// VerifyStep runs verification commands in a sandbox after agent execution.
// Returns nil if all verifiers pass, error otherwise.
func (a *Activities) VerifyStep(ctx context.Context, sandboxID string, stepRunID string, verifiers any) error {
	if verifiers == nil {
		return nil
	}

	cmds := parseVerifiers(verifiers)
	if len(cmds) == 0 {
		return nil
	}

	a.updateStepStatus(ctx, stepRunID, "verifying")

	var failures []string
	for _, cmd := range cmds {
		activity.RecordHeartbeat(ctx, "verifying: "+cmd)
		stdout, stderr, err := a.Sandbox.Exec(ctx, sandboxID, cmd, WorkspacePath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("verify %q: %s (stdout: %s, stderr: %s)", cmd, err, stdout, stderr))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("verification failed:\n%s", strings.Join(failures, "\n"))
	}
	return nil
}

// parseVerifiers extracts a list of command strings from a verifiers value,
// which may be a []any (JSON array of strings), a string, or nil.
func parseVerifiers(v any) []string {
	switch val := v.(type) {
	case []any:
		var cmds []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				cmds = append(cmds, s)
			}
		}
		return cmds
	case []string:
		return val
	case string:
		if val != "" {
			return []string{val}
		}
	}
	return nil
}
