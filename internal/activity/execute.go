package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

var checkpointBranchRe = regexp.MustCompile(`^fleetlift/checkpoint/[a-zA-Z0-9_-]+$`)

// buildContinuationPrompt prepends the original prompt with continuation context.
func buildContinuationPrompt(originalPrompt string, cc *model.ContinuationContext) string {
	if cc == nil {
		return originalPrompt
	}
	header := fmt.Sprintf(
		"[CONTINUATION CONTEXT]\nPrevious step asked: %q\nHuman answered: %q\n\n"+
			"Your working state has been preserved. If a checkpoint branch was provided, "+
			"your working directory already contains your previous changes.\n"+
			"[END CONTINUATION CONTEXT]\n\n",
		cc.Question, cc.HumanAnswer,
	)
	return header + originalPrompt
}

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

	// After cloning, checkout checkpoint branch if this is a continuation step
	if input.ContinuationContext != nil && input.ContinuationContext.CheckpointBranch != "" {
		branch := input.ContinuationContext.CheckpointBranch
		if !checkpointBranchRe.MatchString(branch) {
			return nil, fmt.Errorf("invalid checkpoint branch name: %q", branch)
		}
		for _, repo := range stepInput.ResolvedOpts.Repos {
			repoDir := "/workspace/" + repoName(repo)
			checkoutCmd := fmt.Sprintf("cd %s && git fetch origin %s && git checkout %s",
				shellquote.Quote(repoDir),
				shellquote.Quote(branch),
				shellquote.Quote(branch),
			)
			if _, _, err := sb.Exec(ctx, input.SandboxID, checkoutCmd, "/"); err != nil {
				return nil, fmt.Errorf("checkout checkpoint branch %q: %w", branch, err)
			}
		}
	}

	// 2. Run agent with streaming output
	activity.RecordHeartbeat(ctx, "running agent")
	a.updateStepStatus(ctx, stepInput.StepRunID, model.StepStatusRunning)

	runner, ok := a.AgentRunners[stepInput.ResolvedOpts.Agent]
	if !ok {
		return nil, fmt.Errorf("unknown agent: %s", stepInput.ResolvedOpts.Agent)
	}

	// Apply continuation context to prompt if present
	if input.ContinuationContext != nil {
		input.Prompt = buildContinuationPrompt(input.Prompt, input.ContinuationContext)
	}

	prompt := input.Prompt
	if input.ConversationHistory != "" {
		prompt = input.ConversationHistory + "\n\n" + prompt
	}

	// Append schema output instructions if step declares an output schema.
	if stepInput.StepDef.Execution != nil && stepInput.StepDef.Execution.Output != nil {
		prompt = appendOutputSchemaInstructions(prompt, stepInput.StepDef.Execution.Output.Schema)
	}

	// Set working directory to the repo if there's exactly one.
	workDir := WorkspacePath
	if len(stepInput.ResolvedOpts.Repos) == 1 {
		workDir = "/workspace/" + repoName(stepInput.ResolvedOpts.Repos[0])
	}

	events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
		Prompt:         prompt,
		WorkDir:        workDir,
		MaxTurns:       stepInput.ResolvedOpts.MaxTurns,
		EvalPluginDirs: input.EvalPluginDirs,
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

	// Check if MCP handler set status to awaiting_input during this execution
	if a.DB != nil {
		var dbStatus string
		if err := a.DB.QueryRowContext(ctx,
			"SELECT status FROM step_runs WHERE id = $1",
			stepInput.StepRunID,
		).Scan(&dbStatus); err == nil && dbStatus == "awaiting_input" {
			var inboxItemID, question string
			_ = a.DB.QueryRowContext(ctx,
				`SELECT id, COALESCE(question,'') FROM inbox_items
				 WHERE step_run_id = $1 AND kind = 'request_input'
				 ORDER BY created_at DESC LIMIT 1`,
				stepInput.StepRunID,
			).Scan(&inboxItemID, &question)

			var checkpointBranch, stateArtifactID string
			_ = a.DB.QueryRowContext(ctx,
				`SELECT COALESCE(checkpoint_branch,''), COALESCE(checkpoint_artifact_id::text,'')
				 FROM step_runs WHERE id = $1`,
				stepInput.StepRunID,
			).Scan(&checkpointBranch, &stateArtifactID)

			return &model.StepOutput{
				StepID:           stepInput.StepDef.ID,
				Status:           model.StepStatusAwaitingInput,
				InboxItemID:      inboxItemID,
				Question:         question,
				CheckpointBranch: checkpointBranch,
				StateArtifactID:  stateArtifactID,
			}, nil
		}
	}

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
				StepID:  stepInput.StepDef.ID,
				Status:  model.StepStatusFailed,
				Output:  lastOutput,
				Error:   errMsg,
				CostUSD: extractCostUSD(lastOutput),
			}, nil
		}
	}

	// Check for non-zero exit code (shell runner includes exit_code in output).
	if exitCode, ok := lastOutput["exit_code"]; ok {
		if code, isNum := exitCode.(float64); isNum && code != 0 {
			return &model.StepOutput{
				StepID:  stepInput.StepDef.ID,
				Status:  model.StepStatusFailed,
				Output:  lastOutput,
				Error:   fmt.Sprintf("command exited with code %d", int(code)),
				CostUSD: extractCostUSD(lastOutput),
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

	// If the step declares an output schema, enforce it.
	if stepInput.StepDef.Execution != nil && stepInput.StepDef.Execution.Output != nil {
		enforced, err := enforceOutputSchema(lastOutput, stepInput.StepDef.Execution.Output.Schema)
		if err != nil {
			return &model.StepOutput{
				StepID:  stepInput.StepDef.ID,
				Status:  model.StepStatusFailed,
				Output:  structured,
				Error:   err.Error(),
				CostUSD: extractCostUSD(lastOutput),
			}, nil
		}
		structured = enforced
	}

	return &model.StepOutput{
		StepID:  stepInput.StepDef.ID,
		Status:  model.StepStatusComplete,
		Output:  structured,
		Diff:    diff,
		CostUSD: extractCostUSD(lastOutput),
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

// enforceOutputSchema extracts schema fields from lastOutput and validates them.
// Returns the filtered structured output, or an error if extraction or validation fails.
func enforceOutputSchema(lastOutput map[string]any, schema map[string]any) (map[string]any, error) {
	resultText, _ := lastOutput["result"].(string)

	extracted, err := extractSchemaFields(resultText, schema)
	if err != nil {
		return nil, fmt.Errorf("agent output did not contain valid JSON matching schema: %w", err)
	}

	violations := validateOutputSchema(extracted, schema)
	if len(violations) > 0 {
		return nil, fmt.Errorf("output schema validation failed: %s", strings.Join(violations, "; "))
	}

	return extracted, nil
}

func extractStructuredOutput(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	// The agent's structured output may be nested under "result" as a map (rare)
	// or as a string containing a fenced JSON block (the common case).
	switch r := raw["result"].(type) {
	case map[string]any:
		return r
	case string:
		// Try fenced ```json ... ``` block first, then bare {...}.
		if matches := fencedJSONRe.FindAllStringSubmatch(r, -1); len(matches) > 0 {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(matches[len(matches)-1][1]), &parsed); err == nil {
				return parsed
			}
		}
		if matches := bareJSONRe.FindAllString(r, -1); len(matches) > 0 {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(matches[len(matches)-1]), &parsed); err == nil {
				return parsed
			}
		}
	}
	return raw
}

// appendOutputSchemaInstructions appends schema output instructions to the prompt.
// If schema is empty, the prompt is returned unchanged.
func appendOutputSchemaInstructions(prompt string, schema map[string]any) string {
	if len(schema) == 0 {
		return prompt
	}

	// Build example JSON with placeholder values based on declared types.
	keys := make([]string, 0, len(schema))
	for k := range schema {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var placeholder any
		typ, _ := schema[k].(string)
		switch typ {
		case "array":
			placeholder = []any{"<array>"}
		case "boolean":
			placeholder = "<boolean>"
		case "number":
			placeholder = "<number>"
		case "object":
			placeholder = map[string]any{}
		default:
			placeholder = "<string>"
		}
		valBytes, _ := json.Marshal(placeholder)
		keyBytes, _ := json.Marshal(k)
		parts = append(parts, string(keyBytes)+": "+string(valBytes))
	}
	exampleJSON := "{" + strings.Join(parts, ", ") + "}"

	return prompt + "\n\nIMPORTANT: At the end of your response, you MUST output a JSON object with exactly these fields,\nwrapped in a ```json fenced code block:\n\n" + exampleJSON + "\n\nThis structured output is required for downstream workflow steps."
}

var (
	fencedJSONRe = regexp.MustCompile("(?s)```json\\s*\\n([\\s\\S]*?)\\n```")
	bareJSONRe   = regexp.MustCompile(`(?s)\{(?:[^{}]|\{[^{}]*\})*\}`)
)

// extractSchemaFields extracts declared schema fields from the agent's result text.
func extractSchemaFields(resultText string, schema map[string]any) (map[string]any, error) {
	// Strategy 1: find fenced ```json ... ``` blocks, parse the last one.
	fencedMatches := fencedJSONRe.FindAllStringSubmatch(resultText, -1)
	if len(fencedMatches) > 0 {
		last := fencedMatches[len(fencedMatches)-1][1]
		var parsed map[string]any
		if err := json.Unmarshal([]byte(last), &parsed); err == nil {
			return filterSchema(parsed, schema), nil
		}
	}

	// Strategy 2: find bare {...} JSON objects, parse the last one.
	bareMatches := bareJSONRe.FindAllString(resultText, -1)
	if len(bareMatches) > 0 {
		last := bareMatches[len(bareMatches)-1]
		var parsed map[string]any
		if err := json.Unmarshal([]byte(last), &parsed); err == nil {
			return filterSchema(parsed, schema), nil
		}
	}

	return nil, fmt.Errorf("no JSON object found in agent output")
}

// filterSchema returns a new map containing only keys declared in schema that exist in parsed.
func filterSchema(parsed map[string]any, schema map[string]any) map[string]any {
	result := make(map[string]any, len(schema))
	for k := range schema {
		if v, ok := parsed[k]; ok {
			result[k] = v
		}
	}
	return result
}

// extractCostUSD reads the agent cost from a Claude Code result event.
// Claude Code CLI emits total_cost_usd in the result event (older versions used cost_usd).
func extractCostUSD(raw map[string]any) float64 {
	if v, ok := raw["total_cost_usd"].(float64); ok {
		return v
	}
	if v, ok := raw["cost_usd"].(float64); ok {
		return v
	}
	return 0
}

// validateOutputSchema checks that all schema fields are present in output with the correct types.
// Returns a sorted list of violation messages.
func validateOutputSchema(output map[string]any, schema map[string]any) []string {
	var violations []string
	for field, typVal := range schema {
		val, ok := output[field]
		if !ok {
			violations = append(violations, fmt.Sprintf("missing required field %q", field))
			continue
		}
		typ, _ := typVal.(string)
		switch typ {
		case "string":
			if _, ok := val.(string); !ok {
				violations = append(violations, fmt.Sprintf("field %q must be a string, got %T", field, val))
			}
		case "array":
			if _, ok := val.([]any); !ok {
				violations = append(violations, fmt.Sprintf("field %q must be a array, got %T", field, val))
			}
		case "boolean":
			if _, ok := val.(bool); !ok {
				violations = append(violations, fmt.Sprintf("field %q must be a boolean, got %T", field, val))
			}
		case "number":
			switch val.(type) {
			case float64, int, int64:
				// valid
			default:
				violations = append(violations, fmt.Sprintf("field %q must be a number, got %T", field, val))
			}
		}
	}
	sort.Strings(violations)
	return violations
}
