package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

func TestRunAgenticTransformation_PromptPassedDirectly(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Title:  "Fix the bug",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git"},
		},
		Execution: protocol.ManifestExecution{
			Type:   "agentic",
			Prompt: "Fix the bug in main.go",
		},
	}

	output, err := p.runAgenticTransformation(context.Background(), manifest)
	require.NoError(t, err)
	assert.Empty(t, output) // mock returns empty

	calls := exec.getCalls()
	require.Len(t, calls, 1)

	call := calls[0]
	assert.Equal(t, "claude", call.Name)

	// C1 fix verification: prompt is passed directly, no base64
	assert.Equal(t, "-p", call.Args[0])
	assert.Contains(t, call.Args[1], "Fix the bug in main.go")
	assert.NotContains(t, strings.Join(call.Args, " "), "base64")
	assert.NotContains(t, strings.Join(call.Args, " "), "$(echo")
}

func TestRunDeterministicTransformation(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Execution: protocol.ManifestExecution{
			Type:    "deterministic",
			Command: []string{"mvn", "rewrite:run"},
			Args:    []string{"-DactiveRecipes=org.openrewrite"},
			Env:     map[string]string{"JAVA_HOME": "/usr/lib/jvm"},
		},
	}

	_, err := p.runDeterministicTransformation(context.Background(), manifest)
	require.NoError(t, err)

	calls := exec.getCalls()
	require.Len(t, calls, 1)

	call := calls[0]
	assert.Equal(t, "mvn", call.Name)
	assert.Contains(t, call.Args, "rewrite:run")
	assert.Contains(t, call.Args, "-DactiveRecipes=org.openrewrite")
}

func TestRunDeterministicTransformation_EmptyCommand(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Execution: protocol.ManifestExecution{Type: "deterministic", Command: nil},
	}

	_, err := p.runDeterministicTransformation(context.Background(), manifest)
	assert.ErrorContains(t, err, "deterministic execution requires command")
}

func TestBuildTransformPrompt(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Title: "Migrate logging",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc"},
		},
		Execution: protocol.ManifestExecution{
			Prompt: "Migrate to slog",
		},
		Verifiers: []protocol.ManifestVerifier{
			{Name: "build", Command: []string{"go", "build", "./..."}},
		},
	}

	prompt := p.buildTransformPrompt(manifest)
	assert.Contains(t, prompt, "Migrate logging")
	assert.Contains(t, prompt, "Migrate to slog")
	assert.Contains(t, prompt, "go build ./...")
}

func TestBuildTransformPrompt_ForEachTargets(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Title: "Classify",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc"},
		},
		Execution: protocol.ManifestExecution{Prompt: "Classify endpoints"},
		ForEach: []protocol.ForEachTarget{
			{Name: "users-api", Context: "GET /users"},
			{Name: "orders-api", Context: "GET /orders"},
		},
	}

	prompt := p.buildTransformPrompt(manifest)
	assert.Contains(t, prompt, "users-api")
	assert.Contains(t, prompt, "orders-api")
	assert.Contains(t, prompt, "GET /users")
}

func TestBuildTransformPrompt_ReportMode(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Title: "Audit",
		Mode:  "report",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc"},
		},
		Execution: protocol.ManifestExecution{Prompt: "Audit security"},
	}

	prompt := p.buildTransformPrompt(manifest)
	assert.Contains(t, prompt, "REPORT.md")
}

func TestBuildSteeringPrompt_Truncation(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Title:     "Task",
		Execution: protocol.ManifestExecution{Prompt: "Fix bug"},
	}

	longOutput := strings.Repeat("x", MaxSteeringContextChars+1000)
	prompt := p.buildSteeringPrompt(manifest, "fix tests too", 1, longOutput)

	assert.Contains(t, prompt, "fix tests too")
	assert.Contains(t, prompt, "[truncated]")
	// The prompt should not contain the full long output
	assert.Less(t, len(prompt), len(longOutput))
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"CUSTOM_VAR=hello",
		"GITHUB_TOKEN=secret",
		"ANTHROPIC_API_KEY=sk-xxx",
	}

	filtered := filterEnv(env, map[string]bool{"GITHUB_TOKEN": true})
	assert.Len(t, filtered, 4)
	assert.NotContains(t, filtered, "GITHUB_TOKEN=secret")
	assert.Contains(t, filtered, "CUSTOM_VAR=hello")
}

func TestFilterEnv_CaseInsensitive(t *testing.T) {
	env := []string{"github_token=secret", "FOO=bar"}
	filtered := filterEnv(env, map[string]bool{"GITHUB_TOKEN": true})
	assert.Len(t, filtered, 1)
	assert.Equal(t, "FOO=bar", filtered[0])
}

func TestRunDeterministicTransformation_BlockedEnvVars(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	var capturedEnv []string
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		capturedEnv = opts.Env
		return &CommandResult{ExitCode: 0}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Execution: protocol.ManifestExecution{
			Type:    "deterministic",
			Command: []string{"mvn", "test"},
			Env: map[string]string{
				"JAVA_HOME":  "/usr/lib/jvm",
				"PATH":       "/malicious/path",
				"LD_PRELOAD": "/evil.so",
			},
		},
	}

	_, err := p.runDeterministicTransformation(context.Background(), manifest)
	require.NoError(t, err)

	// JAVA_HOME should be present, PATH and LD_PRELOAD should be blocked
	envStr := strings.Join(capturedEnv, "\n")
	assert.Contains(t, envStr, "JAVA_HOME=/usr/lib/jvm")
	assert.NotContains(t, envStr, "PATH=/malicious/path")
	assert.NotContains(t, envStr, "LD_PRELOAD=/evil.so")
}

func TestRunTransformation_Dispatch(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	tests := []struct {
		execType    string
		expectName  string
		needCommand bool
	}{
		{"agentic", "claude", false},
		{"deterministic", "mvn", true},
		{"", "claude", false}, // default
	}

	for _, tt := range tests {
		exec.mu.Lock()
		exec.calls = nil
		exec.mu.Unlock()

		manifest := &protocol.TaskManifest{
			Title: "Task",
			Repositories: []protocol.ManifestRepo{
				{Name: "svc"},
			},
			Execution: protocol.ManifestExecution{
				Type:   tt.execType,
				Prompt: "do stuff",
			},
		}
		if tt.needCommand {
			manifest.Execution.Command = []string{tt.expectName, "run"}
		}

		_, _ = p.runTransformation(context.Background(), manifest)

		calls := exec.getCalls()
		if len(calls) > 0 {
			assert.Equal(t, tt.expectName, calls[0].Name, "for exec type %q", tt.execType)
		}
	}
}
