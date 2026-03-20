package template

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// loadADS loads the auto-debt-slayer template and parses it.
func loadADS(t *testing.T) model.WorkflowDef {
	t.Helper()
	p, err := NewBuiltinProvider()
	require.NoError(t, err)
	tmpl, err := p.Get(context.Background(), "", "auto-debt-slayer")
	require.NoError(t, err)
	var def model.WorkflowDef
	require.NoError(t, model.ParseWorkflowYAML([]byte(tmpl.YAMLBody), &def))
	return def
}

func TestADS_EnrichPrompt_RequiredParamsOnly(t *testing.T) {
	def := loadADS(t)
	enrich := def.Steps[0]

	rendered, err := RenderPrompt(enrich.Execution.Prompt, RenderContext{
		Params: map[string]any{
			"ticket_key":   "AFX-1234",
			"jira_base_url": "https://myorg.atlassian.net",
			"github_repo":  "https://github.com/org/repo",
		},
		Steps: map[string]*model.StepOutput{},
	})
	require.NoError(t, err)
	assert.Contains(t, rendered, "AFX-1234")
	assert.Contains(t, rendered, "https://myorg.atlassian.net")
	// Default budget applied when param absent
	assert.Contains(t, rendered, "10.00")
}

func TestADS_EnrichPrompt_WithBudgetParam(t *testing.T) {
	def := loadADS(t)
	enrich := def.Steps[0]

	rendered, err := RenderPrompt(enrich.Execution.Prompt, RenderContext{
		Params: map[string]any{
			"ticket_key":          "AFX-1234",
			"jira_base_url":       "https://myorg.atlassian.net",
			"github_repo":         "https://github.com/org/repo",
			"per_task_budget_usd": "25.00",
		},
		Steps: map[string]*model.StepOutput{},
	})
	require.NoError(t, err)
	assert.Contains(t, rendered, "25.00")
	assert.NotContains(t, rendered, "10.00")
}

func TestADS_AssessPrompt_RendersEnrichOutput(t *testing.T) {
	def := loadADS(t)
	assess := def.Steps[1]

	enrichOutput := map[string]any{
		"ticket_summary":     "Fix null pointer in auth handler",
		"ticket_description": "When user logs in with an expired token...",
		"related_files":      []string{"internal/auth/handler.go"},
	}

	rendered, err := RenderPrompt(assess.Execution.Prompt, RenderContext{
		Params: map[string]any{
			"ticket_key":   "AFX-1234",
			"jira_base_url": "https://myorg.atlassian.net",
			"github_repo":  "https://github.com/org/repo",
		},
		Steps: map[string]*model.StepOutput{
			"enrich": {
				Status: model.StepStatusComplete,
				Output: enrichOutput,
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, rendered, "Fix null pointer in auth handler")
	assert.Contains(t, rendered, "10.00") // default budget
}

func TestADS_ExecutePullRequest_RendersFromAssessOutput(t *testing.T) {
	def := loadADS(t)
	execute := def.Steps[2]
	require.NotNil(t, execute.PullRequest)

	assessOutput := map[string]any{
		"decision":       "execute",
		"pr_title_hint":  "fix(AFX-1234): null pointer in auth handler",
		"pr_body_draft":  "## AFX-1234\n\nFixes null pointer...",
		"caveats":        []string{},
		"risks":          []string{},
		"estimated_complexity": "simple",
	}
	ctx := RenderContext{
		Params: map[string]any{
			"ticket_key":  "AFX-1234",
			"github_repo": "https://github.com/org/repo",
		},
		Steps: map[string]*model.StepOutput{
			"enrich": {Status: model.StepStatusComplete, Output: map[string]any{}},
			"assess": {Status: model.StepStatusComplete, Output: assessOutput},
		},
	}

	branch, err := RenderPrompt(execute.PullRequest.BranchPrefix, ctx)
	require.NoError(t, err)
	assert.Equal(t, "agent/AFX-1234-", branch)

	title, err := RenderPrompt(execute.PullRequest.Title, ctx)
	require.NoError(t, err)
	assert.Equal(t, "fix(AFX-1234): null pointer in auth handler", title)

	body, err := RenderPrompt(execute.PullRequest.Body, ctx)
	require.NoError(t, err)
	assert.Contains(t, body, "AFX-1234")
}

func TestADS_NotifyMessage_ExecutePath(t *testing.T) {
	def := loadADS(t)
	notify := def.Steps[3]
	require.NotNil(t, notify.Action)

	msg := notify.Action.Config["message"].(string)

	rendered, err := RenderPrompt(msg, RenderContext{
		Params: map[string]any{
			"ticket_key": "AFX-1234",
		},
		Steps: map[string]*model.StepOutput{
			"assess": {
				Status: model.StepStatusComplete,
				Output: map[string]any{
					"decision":         "execute",
					"decision_reasons": []string{},
				},
			},
			"execute": {
				Status: model.StepStatusComplete,
				Output: map[string]any{
					"agent_summary":   "Fixed null pointer in auth handler",
					"total_cost_usd":  2.15,
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, rendered, "PR created for AFX-1234")
	assert.Contains(t, rendered, "Fixed null pointer in auth handler")
	assert.Contains(t, rendered, "2.15")
}

func TestADS_NotifyMessage_ManualNeededPath(t *testing.T) {
	def := loadADS(t)
	notify := def.Steps[3]
	msg := notify.Action.Config["message"].(string)

	rendered, err := RenderPrompt(msg, RenderContext{
		Params: map[string]any{
			"ticket_key": "AFX-1234",
		},
		Steps: map[string]*model.StepOutput{
			"assess": {
				Status: model.StepStatusComplete,
				Output: map[string]any{
					"decision":         "manual_needed",
					"decision_reasons": []string{"requirements unclear", "scope too broad"},
				},
			},
			"execute": {
				Status: model.StepStatusSkipped,
				Output: nil,
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, rendered, "Manual review needed for AFX-1234")
	assert.True(t,
		strings.Contains(rendered, "requirements unclear") &&
			strings.Contains(rendered, "scope too broad"),
		"expected decision reasons in message, got: %s", rendered)
}

func TestADS_NotifyChannel_OptionalParamAbsent(t *testing.T) {
	def := loadADS(t)
	notify := def.Steps[3]
	channel := notify.Action.Config["channel"].(string)

	// slack_channel_id not provided — must not error
	rendered, err := RenderPrompt(channel, RenderContext{
		Params: map[string]any{"ticket_key": "AFX-1234"},
		Steps:  map[string]*model.StepOutput{},
	})
	require.NoError(t, err)
	assert.Equal(t, "", rendered)
}
