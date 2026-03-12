package activity

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
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

	// 1. Clone repos
	for _, repo := range stepInput.ResolvedOpts.Repos {
		cloneCmd := fmt.Sprintf("git clone --depth %s", DefaultCloneDepth)
		if repo.Branch != "" {
			cloneCmd += fmt.Sprintf(" --branch %s", shellQuote(repo.Branch))
		}
		cloneCmd += fmt.Sprintf(" %s /workspace/%s", shellQuote(repo.URL), shellQuote(repoName(repo)))
		activity.RecordHeartbeat(ctx, "cloning "+repoName(repo))
		a.updateStepStatus(ctx, stepInput.StepRunID, model.StepStatusCloning)

		if _, _, err := sb.Exec(ctx, input.SandboxID, cloneCmd, "/"); err != nil {
			return nil, fmt.Errorf("clone %s: %w", repo.URL, err)
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

	// Enrich prompt with approved knowledge items if configured
	if stepInput.StepDef.Knowledge != nil && stepInput.StepDef.Knowledge.Enrich && a.KnowledgeStore != nil {
		maxItems := stepInput.StepDef.Knowledge.MaxItems
		if maxItems == 0 {
			maxItems = 10
		}
		knowledgeItems, err := a.KnowledgeStore.ListApprovedByWorkflow(ctx,
			stepInput.TeamID,
			stepInput.WorkflowTemplateID,
			maxItems,
		)
		if err == nil && len(knowledgeItems) > 0 {
			enrichBlock := knowledge.FormatEnrichmentBlock(knowledgeItems)
			prompt = enrichBlock + "\n\n" + prompt
		}
	}

	// Instruct agent to capture knowledge if configured
	if stepInput.StepDef.Knowledge != nil && stepInput.StepDef.Knowledge.Capture {
		prompt += "\n\n## Knowledge Capture\n\nBefore exiting, write `fleetlift-knowledge.json` to the current directory with any insights you gained. Format:\n```json\n[\n  {\"type\": \"pattern|correction|gotcha|context\", \"summary\": \"brief insight\", \"details\": \"optional detail\", \"confidence\": 0.9}\n]\n```\nOnly include non-obvious insights worth sharing with future runs."
	}

	events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
		Prompt:  prompt,
		WorkDir: WorkspacePath,
	})
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	var seq int64
	var lastOutput map[string]any
	for event := range events {
		activity.RecordHeartbeat(ctx, "agent running: "+event.Type)
		a.writeLogLine(ctx, stepInput.StepRunID, seq, "stdout", event.Content)
		seq++
		if event.Type == "complete" {
			lastOutput = event.Output
		}
		if event.Type == "error" {
			return nil, fmt.Errorf("agent error: %s", event.Content)
		}
	}

	// 3. Extract git diff — run in each repo dir and concatenate
	var diffParts []string
	for _, repo := range stepInput.ResolvedOpts.Repos {
		repoDir := "/workspace/" + repoName(repo)
		if d, _, err := sb.Exec(ctx, input.SandboxID, "git diff", repoDir); err == nil && d != "" {
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
