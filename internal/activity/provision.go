package activity

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

var credNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

var reservedEnvVars = map[string]bool{
	"PATH": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true,
	"HOME": true, "USER": true, "SHELL": true,
}

// ProvisionSandbox creates a sandbox and injects team credentials as env vars.
// Returns the sandbox ID.
func (a *Activities) ProvisionSandbox(ctx context.Context, input workflow.StepInput) (string, error) {
	env := make(map[string]string)

	// Resolve team credentials by name, injecting each as an env var
	if a.CredStore != nil && len(input.ResolvedOpts.Credentials) > 0 {
		// Validate all names upfront before making any DB calls.
		for _, credName := range input.ResolvedOpts.Credentials {
			if !credNameRe.MatchString(credName) {
				return "", fmt.Errorf("invalid credential name %q: must match ^[A-Z][A-Z0-9_]*$", credName)
			}
			if reservedEnvVars[credName] {
				return "", fmt.Errorf("credential name %q conflicts with reserved environment variable", credName)
			}
		}
		// Fetch all credentials in a single batch query.
		vals, err := a.CredStore.GetBatch(ctx, input.TeamID, input.ResolvedOpts.Credentials)
		if err != nil {
			return "", fmt.Errorf("resolve credentials: %w", err)
		}
		for k, v := range vals {
			env[k] = v
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
