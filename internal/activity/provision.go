package activity

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
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

	// Inject agent-specific env vars (e.g. Claude auth keys)
	if runner, ok := a.AgentRunners[input.ResolvedOpts.Agent]; ok {
		for k, v := range runner.SandboxEnv() {
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

	// Ensure /workspace exists — the execd fails with a misleading bash error when
	// the cwd doesn't exist. Ubuntu (used for the shell agent) doesn't pre-create it.
	if _, _, err := a.Sandbox.Exec(ctx, sandboxID, "mkdir -p /workspace", "/"); err != nil {
		_ = a.Sandbox.Kill(ctx, sandboxID) // best-effort cleanup to avoid leaking the sandbox
		return "", fmt.Errorf("create workspace: %w", err)
	}

	// MCP sidecar setup (optional — skip if binary not available)
	if mcpBinaryPath := os.Getenv("FLEETLIFT_MCP_BINARY_PATH"); mcpBinaryPath != "" {
		if mcpData, err := os.ReadFile(mcpBinaryPath); err == nil {
			jwtSecret := []byte(os.Getenv("JWT_SECRET"))
			mcpToken, err := auth.IssueMCPToken(jwtSecret, input.TeamID, input.RunID)
			if err != nil {
				_ = a.Sandbox.Kill(ctx, sandboxID)
				return "", fmt.Errorf("issue MCP token: %w", err)
			}

			if err := a.Sandbox.WriteBytes(ctx, sandboxID, "/usr/local/bin/fleetlift-mcp", mcpData); err != nil {
				_ = a.Sandbox.Kill(ctx, sandboxID)
				return "", fmt.Errorf("upload MCP binary: %w", err)
			}
			if _, _, err := a.Sandbox.Exec(ctx, sandboxID, "chmod +x /usr/local/bin/fleetlift-mcp", "/"); err != nil {
				_ = a.Sandbox.Kill(ctx, sandboxID)
				return "", fmt.Errorf("chmod MCP binary: %w", err)
			}

			apiURL := os.Getenv("FLEETLIFT_API_URL")
			if apiURL == "" {
				apiURL = "http://host.docker.internal:8080"
			}
			mcpPort := "8081"
			startCmd := fmt.Sprintf(
				"FLEETLIFT_MCP_TOKEN=%s FLEETLIFT_MCP_PORT=%s nohup /usr/local/bin/fleetlift-mcp --api-url %s --token %s --port %s > /tmp/fleetlift-mcp.log 2>&1 &",
				shellquote.Quote(mcpToken), mcpPort,
				shellquote.Quote(apiURL), shellquote.Quote(mcpToken), mcpPort,
			)
			if _, _, err := a.Sandbox.Exec(ctx, sandboxID, startCmd, "/"); err != nil {
				slog.Warn("failed to start MCP sidecar", "error", err, "sandbox_id", sandboxID)
			} else {
				// Health check — retry for up to 5 seconds
				healthy := false
				for i := 0; i < 10; i++ {
					stdout, _, err := a.Sandbox.Exec(ctx, sandboxID,
						"curl -sf http://localhost:"+mcpPort+"/health", "/")
					if err == nil && strings.Contains(stdout, "ok") {
						healthy = true
						break
					}
					time.Sleep(500 * time.Millisecond)
				}
				if !healthy {
					slog.Warn("MCP sidecar health check failed after 5s", "sandbox_id", sandboxID)
				}
			}
		}
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
	case "shell":
		if img := os.Getenv("SHELL_IMAGE"); img != "" {
			return img
		}
		return "ubuntu:22.04"
	default: // claude-code
		if img := os.Getenv("AGENT_IMAGE"); img != "" {
			return img
		}
		return "claude-code:latest"
	}
}
