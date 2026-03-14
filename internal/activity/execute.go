package activity

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// ExecuteStep is the core long-running activity. It:
// 1. Clones all repos into the sandbox
// 2. Runs the agent with streaming output
// 3. Heartbeats Temporal as events arrive
// 4. Writes log lines to DB
// 5. Extracts diff and structured output on completion
func (a *Activities) ExecuteStep(ctx context.Context, input workflow.ExecuteStepInput) (*model.StepOutput, error) {
	sb := a.Sandbox
	stepInput := input.StepInput

	// Log buffer is active for the entire step (clone + agent phases) so
	// clone progress and errors appear in the UI log stream immediately.
	buf := newLogBuffer(a, stepInput.StepRunID, "stdout", LogFlushThreshold)
	var seq int64

	// logLine writes a single line to the step log with the given stream and flushes immediately.
	logLine := func(stream, content string) {
		if content == "" {
			return
		}
		_ = batchInsertLogs(ctx, a, stepInput.StepRunID, []logLine{{Seq: seq, Stream: stream, Content: content}})
		seq++
	}

	// Validate all repo URLs before any Temporal-specific calls.
	for _, repo := range stepInput.ResolvedOpts.Repos {
		if !strings.HasPrefix(repo.URL, "https://") {
			return nil, fmt.Errorf("repo URL must use https:// scheme, got: %q", repo.URL)
		}
	}

	// Show a retry banner so users can tell when Temporal is re-running the activity.
	if attempt := activity.GetInfo(ctx).Attempt; attempt > 1 {
		logLine("stderr", fmt.Sprintf("--- retry attempt %d ---", attempt))
	}

	// 1. Clone repos
	for _, repo := range stepInput.ResolvedOpts.Repos {
		repoDir := "/workspace/" + repoName(repo)

		// If a prior step in the same sandbox already cloned this repo, reuse it.
		// This allows downstream steps to declare repositories for workdir purposes
		// without triggering a redundant clone.
		if head, _, _ := sb.Exec(ctx, input.SandboxID, "cat "+shellquote.Quote(repoDir+"/.git/HEAD"), "/"); head != "" {
			logLine("stdout", "Using existing clone at "+repoDir)
			continue
		}

		cloneCmd := fmt.Sprintf("git clone --depth %s", DefaultCloneDepth)
		if repo.Branch != "" {
			cloneCmd += fmt.Sprintf(" --branch %s", shellquote.Quote(repo.Branch))
		}
		cloneCmd += fmt.Sprintf(" %s %s", shellquote.Quote(repo.URL), shellquote.Quote(repoDir))
		activity.RecordHeartbeat(ctx, "cloning "+repoName(repo))
		a.updateStepStatus(ctx, stepInput.StepRunID, model.StepStatusCloning)
		logLine("stdout", "Cloning "+repo.URL+"…")

		// Remove any leftover directory from a previous attempt (e.g. sandbox reuse on retry).
		if _, _, err := sb.Exec(ctx, input.SandboxID, "rm -rf "+shellquote.Quote(repoDir), "/"); err != nil {
			return nil, fmt.Errorf("clean repo dir %s: %w", repoDir, err)
		}

		if _, stderr, err := sb.Exec(ctx, input.SandboxID, cloneCmd, "/"); err != nil {
			msg := fmt.Sprintf("clone failed: %v", err)
			logLine("stderr", msg)
			return nil, fmt.Errorf("clone %s: %w", repo.URL, err)
		} else if gitFailed(stderr) {
			msg := "clone failed: " + strings.TrimSpace(stderr)
			logLine("stderr", msg)
			return nil, fmt.Errorf("clone %s: %s", repo.URL, strings.TrimSpace(stderr))
		} else if stderr != "" {
			logLine("stdout", strings.TrimSpace(stderr))
		}

		// Verify the clone actually created a .git directory. ExecStream may not
		// flush all stderr before execution_complete, so gitFailed can miss auth
		// errors that appear after the initial "Cloning into..." message.
		if head, _, _ := sb.Exec(ctx, input.SandboxID, "cat "+shellquote.Quote(repoDir+"/.git/HEAD"), "/"); head == "" {
			msg := "clone failed: repository not cloned (check GITHUB_TOKEN credential is configured if the repo is private)"
			logLine("stderr", msg)
			return nil, fmt.Errorf("clone %s: no .git directory after clone", repo.URL)
		}

		// Fetch and checkout a specific ref (e.g. "refs/pull/19/head" for PRs).
		// Use `git -C <dir>` to explicitly set the working directory (more reliable than cwd).
		if repo.Ref != "" {
			logLine("stdout", "Fetching ref "+repo.Ref+"…")
			fetchCmd := fmt.Sprintf("git -C %s fetch origin %s", shellquote.Quote(repoDir), shellquote.Quote(repo.Ref))
			if _, stderr, err := sb.Exec(ctx, input.SandboxID, fetchCmd, "/"); err != nil {
				msg := fmt.Sprintf("fetch failed: %v", err)
				logLine("stderr", msg)
				return nil, fmt.Errorf("fetch ref %s: %w", repo.Ref, err)
			} else if gitFailed(stderr) {
				msg := "fetch failed: " + strings.TrimSpace(stderr)
				logLine("stderr", msg)
				return nil, fmt.Errorf("fetch ref %s: %s", repo.Ref, strings.TrimSpace(stderr))
			}
			checkoutCmd := fmt.Sprintf("git -C %s checkout FETCH_HEAD", shellquote.Quote(repoDir))
			if _, stderr, err := sb.Exec(ctx, input.SandboxID, checkoutCmd, "/"); err != nil {
				msg := fmt.Sprintf("checkout failed: %v", err)
				logLine("stderr", msg)
				return nil, fmt.Errorf("checkout ref %s: %w", repo.Ref, err)
			} else if gitFailed(stderr) {
				msg := "checkout failed: " + strings.TrimSpace(stderr)
				logLine("stderr", msg)
				return nil, fmt.Errorf("checkout FETCH_HEAD for ref %s: %s", repo.Ref, strings.TrimSpace(stderr))
			}
			logLine("stdout", "Checked out ref "+repo.Ref)
		}
	}

	// 2. Run agent with streaming output
	activity.RecordHeartbeat(ctx, "running agent")
	a.updateStepStatus(ctx, stepInput.StepRunID, model.StepStatusRunning)

	runner, ok := a.AgentRunners[stepInput.ResolvedOpts.Agent]
	if !ok {
		return nil, fmt.Errorf("unknown agent: %s", stepInput.ResolvedOpts.Agent)
	}

	prompt := input.Prompt
	if input.ConversationHistory != "" {
		prompt = input.ConversationHistory + "\n\n" + prompt
	}

	// Set working directory to the repo if there's exactly one.
	workDir := WorkspacePath
	if len(stepInput.ResolvedOpts.Repos) == 1 {
		workDir = "/workspace/" + repoName(stepInput.ResolvedOpts.Repos[0])
	}

	events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
		Prompt:  prompt,
		WorkDir: workDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	var lastOutput map[string]any
	var gotComplete bool
	for event := range events {
		if event.Type == "" && event.Content == "" {
			continue // skip empty events (filtered noise)
		}
		activity.RecordHeartbeat(ctx, "agent running: "+event.Type)
		buf.add(ctx, seq, event.Content)
		seq++
		if event.Type == "complete" {
			lastOutput = event.Output
			gotComplete = true
		}
		if event.Type == "error" {
			buf.flush(ctx)
			return nil, fmt.Errorf("agent error: %s", event.Content)
		}
	}
	buf.flush(ctx)

	// If the agent never emitted a completion event, the command failed.
	if !gotComplete {
		return &model.StepOutput{
			StepID: stepInput.StepDef.ID,
			Status: model.StepStatusFailed,
			Error:  "agent exited without producing a result",
		}, nil
	}

	// Check for agent-reported error (Claude CLI sets is_error: true on failure).
	if isErr, ok := lastOutput["is_error"]; ok {
		if b, isBool := isErr.(bool); isBool && b {
			errMsg := "agent reported an error"
			if result, ok := lastOutput["result"].(string); ok && result != "" {
				errMsg = result
			}
			return &model.StepOutput{
				StepID: stepInput.StepDef.ID,
				Status: model.StepStatusFailed,
				Output: lastOutput,
				Error:  errMsg,
			}, nil
		}
	}

	// Check for non-zero exit code (shell runner includes exit_code in output).
	if exitCode, ok := lastOutput["exit_code"]; ok {
		if code, isNum := exitCode.(float64); isNum && code != 0 {
			return &model.StepOutput{
				StepID: stepInput.StepDef.ID,
				Status: model.StepStatusFailed,
				Output: lastOutput,
				Error:  fmt.Sprintf("command exited with code %d", int(code)),
			}, nil
		}
	}

	// 3. Extract git diff — run in each repo dir and concatenate
	var diffParts []string
	for _, repo := range stepInput.ResolvedOpts.Repos {
		repoDir := "/workspace/" + repoName(repo)
		d, _, err := sb.Exec(ctx, input.SandboxID, "git -C "+shellquote.Quote(repoDir)+" diff", "/")
		if err != nil {
			activity.GetLogger(ctx).Warn("failed to extract diff", "repo", repoDir, "error", err)
			continue
		}
		if d != "" {
			diffParts = append(diffParts, d)
		}
	}
	diff := strings.Join(diffParts, "\n")

	// 4. Extract structured output from agent
	structured := extractStructuredOutput(lastOutput)

	return &model.StepOutput{
		StepID: stepInput.StepDef.ID,
		Status: model.StepStatusComplete,
		Output: structured,
		Diff:   diff,
	}, nil
}

func repoName(repo model.RepoRef) string {
	if repo.Name != "" {
		return repo.Name
	}
	// Extract name from URL
	url := repo.URL
	for _, suffix := range []string{".git", "/"} {
		if len(url) > len(suffix) {
			url = trimSuffix(url, suffix)
		}
	}
	parts := splitLast(url, "/")
	if parts != "" {
		return parts
	}
	return "repo"
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

func splitLast(s, sep string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if string(s[i]) == sep {
			return s[i+1:]
		}
	}
	return s
}

func extractStructuredOutput(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	// The agent's structured output may be nested under "result" or be the top-level map.
	if result, ok := raw["result"].(map[string]any); ok {
		return result
	}
	return raw
}
