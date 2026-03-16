package activity

import (
	"context"
	"fmt"
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

	// MCP sidecar setup (optional — skip if binary path not configured)
	if mcpBinaryPrefix := os.Getenv("FLEETLIFT_MCP_BINARY_PATH"); mcpBinaryPrefix != "" {
		// Detect sandbox architecture to select the correct binary.
		archOut, _, err := a.Sandbox.Exec(ctx, sandboxID, "uname -m", "/")
		if err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("detect sandbox architecture: %w", err)
		}
		archRaw := strings.TrimSpace(archOut)
		var goarch string
		switch archRaw {
		case "x86_64":
			goarch = "amd64"
		case "aarch64":
			goarch = "arm64"
		default:
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("unsupported sandbox architecture %q", archRaw)
		}
		mcpBinaryPath := mcpBinaryPrefix + "-" + goarch

		mcpData, err := os.ReadFile(mcpBinaryPath)
		if err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("MCP binary not found for arch %q: %s — run: make mcp-sidecar: %w", goarch, mcpBinaryPath, err)
		}

		jwtSecret := []byte(os.Getenv("JWT_SECRET"))
		if len(jwtSecret) == 0 {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("JWT_SECRET is required when FLEETLIFT_MCP_BINARY_PATH is set")
		}
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
		// Verify chmod took effect using POSIX-portable test.
		if _, _, err := a.Sandbox.Exec(ctx, sandboxID, "test -x /usr/local/bin/fleetlift-mcp", "/"); err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("MCP binary is not executable after chmod")
		}

		apiURL := os.Getenv("FLEETLIFT_API_URL")
		if apiURL == "" {
			apiURL = "http://host.docker.internal:8080"
		}
		mcpPort := "8081"

		// Pass token exclusively via env var — avoid exposing it in /proc/cmdline.
		startCmd := fmt.Sprintf(
			"FLEETLIFT_MCP_TOKEN=%s nohup /usr/local/bin/fleetlift-mcp --api-url %s --port %s > /tmp/fleetlift-mcp.log 2>&1 &",
			shellquote.Quote(mcpToken),
			shellquote.Quote(apiURL), shellquote.Quote(mcpPort),
		)
		if _, _, err := a.Sandbox.Exec(ctx, sandboxID, startCmd, "/"); err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("start MCP sidecar: %w", err)
		}

		// Health check — retry for up to 5 seconds, respecting context cancellation.
		// Uses perl (available in minimal ubuntu:22.04) instead of curl which may not be installed.
		healthCmd := fmt.Sprintf(
			`perl -e 'use IO::Socket::INET; use IO::Handle; my $s=IO::Socket::INET->new(PeerAddr=>"localhost:%s",Timeout=>1) or exit 1; print $s "GET /health HTTP/1.0\r\nHost: localhost\r\n\r\n"; my $r=join"",$s->getlines; print $r=~/ok/?"ok":"fail"'`,
			shellquote.Quote(mcpPort),
		)
		healthy := false
		for i := 0; i < 10; i++ {
			select {
			case <-ctx.Done():
				_ = a.Sandbox.Kill(ctx, sandboxID)
				return "", fmt.Errorf("context cancelled during MCP health check: %w", ctx.Err())
			default:
			}
			out, _, err := a.Sandbox.Exec(ctx, sandboxID, healthCmd, "/")
			if err == nil && strings.Contains(out, "ok") {
				healthy = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !healthy {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("MCP sidecar health check failed after 5s")
		}

		// Inject MCP port and token into sandbox env so the agent runner and test steps can use them.
		profileCmd := fmt.Sprintf(
			"printf 'export FLEETLIFT_MCP_PORT=%s\nexport FLEETLIFT_MCP_TOKEN=%s\n' >> /etc/profile.d/fleetlift-mcp.sh",
			shellquote.Quote(mcpPort), shellquote.Quote(mcpToken),
		)
		if _, _, err := a.Sandbox.Exec(ctx, sandboxID, profileCmd, "/"); err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("persist MCP env in sandbox: %w", err)
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
