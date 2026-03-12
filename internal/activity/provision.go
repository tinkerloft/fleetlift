package activity

import (
	"context"
	"fmt"
	"os"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// ProvisionSandbox creates a sandbox and injects team credentials as env vars.
// Returns the sandbox ID.
func (a *Activities) ProvisionSandbox(ctx context.Context, input workflow.StepInput) (string, error) {
	env := make(map[string]string)

	// Resolve team credentials by name, injecting each as an env var
	if a.CredStore != nil && len(input.ResolvedOpts.Credentials) > 0 {
		for _, credName := range input.ResolvedOpts.Credentials {
			val, err := a.CredStore.Get(ctx, input.TeamID, credName)
			if err != nil {
				return "", fmt.Errorf("resolve credential %s: %w", credName, err)
			}
			env[credName] = val
		}
	}

	// Inject git identity from worker env
	if email := os.Getenv("GIT_USER_EMAIL"); email != "" {
		env["GIT_USER_EMAIL"] = email
	} else {
		env["GIT_USER_EMAIL"] = DefaultGitEmail
	}
	if name := os.Getenv("GIT_USER_NAME"); name != "" {
		env["GIT_USER_NAME"] = name
	} else {
		env["GIT_USER_NAME"] = DefaultGitName
	}

	sandboxID, err := a.Sandbox.Create(ctx, sandbox.CreateOpts{
		Image:       agentImage(input.ResolvedOpts.Agent),
		Env:         env,
		TimeoutMins: 120,
	})
	if err != nil {
		return "", fmt.Errorf("create sandbox: %w", err)
	}
	return sandboxID, nil
}

func agentImage(agentName string) string {
	switch agentName {
	case "codex":
		if img := os.Getenv("CODEX_IMAGE"); img != "" {
			return img
		}
		return "codex:latest"
	default: // claude-code
		if img := os.Getenv("AGENT_IMAGE"); img != "" {
			return img
		}
		return "claude-code:latest"
	}
}
