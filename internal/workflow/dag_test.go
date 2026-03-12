package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestFindReady(t *testing.T) {
	pending := map[string]model.StepDef{
		"a": {ID: "a", DependsOn: nil},
		"b": {ID: "b", DependsOn: []string{"a"}},
		"c": {ID: "c", DependsOn: []string{"a"}},
		"d": {ID: "d", DependsOn: []string{"b", "c"}},
	}
	done := map[string]*model.StepOutput{}

	// Only "a" has no dependencies
	ready := findReady(pending, done)
	assert.Len(t, ready, 1)
	assert.Equal(t, "a", ready[0].ID)

	// After "a" completes, "b" and "c" are ready
	done["a"] = &model.StepOutput{StepID: "a", Status: model.StepStatusComplete}
	delete(pending, "a")
	ready = findReady(pending, done)
	assert.Len(t, ready, 2)
	ids := []string{ready[0].ID, ready[1].ID}
	assert.ElementsMatch(t, []string{"b", "c"}, ids)

	// After "b" and "c" complete, "d" is ready
	done["b"] = &model.StepOutput{StepID: "b", Status: model.StepStatusComplete}
	done["c"] = &model.StepOutput{StepID: "c", Status: model.StepStatusComplete}
	delete(pending, "b")
	delete(pending, "c")
	ready = findReady(pending, done)
	assert.Len(t, ready, 1)
	assert.Equal(t, "d", ready[0].ID)
}

func TestFindReady_DeterministicOrder(t *testing.T) {
	pending := map[string]model.StepDef{
		"z-step": {ID: "z-step"},
		"a-step": {ID: "a-step"},
		"m-step": {ID: "m-step"},
	}
	ready := findReady(pending, map[string]*model.StepOutput{})
	require.Len(t, ready, 3)
	assert.Equal(t, "a-step", ready[0].ID)
	assert.Equal(t, "m-step", ready[1].ID)
	assert.Equal(t, "z-step", ready[2].ID)
}

func TestFindReady_EmptyPending(t *testing.T) {
	pending := map[string]model.StepDef{}
	done := map[string]*model.StepOutput{}
	ready := findReady(pending, done)
	assert.Empty(t, ready)
}

func TestShouldPause(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		output *model.StepOutput
		want   bool
	}{
		{"never policy", "never", &model.StepOutput{}, false},
		{"empty policy", "", &model.StepOutput{}, false},
		{"always policy", "always", &model.StepOutput{}, true},
		{"on_changes with diff", "on_changes", &model.StepOutput{Diff: "some diff"}, true},
		{"on_changes no diff", "on_changes", &model.StepOutput{Diff: ""}, false},
		{"agent needs review", "agent", &model.StepOutput{Output: map[string]any{"needs_review": true}}, true},
		{"agent no review", "agent", &model.StepOutput{Output: map[string]any{"needs_review": false}}, false},
		{"agent nil output", "agent", &model.StepOutput{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := model.StepDef{ApprovalPolicy: tt.policy}
			got := shouldPause(def, tt.output)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsOptional(t *testing.T) {
	steps := []model.StepDef{
		{ID: "a", Optional: false},
		{ID: "b", Optional: true},
	}
	assert.False(t, isOptional(steps, "a"))
	assert.True(t, isOptional(steps, "b"))
	assert.False(t, isOptional(steps, "nonexistent"))
}

func TestSkipDownstream(t *testing.T) {
	allSteps := []model.StepDef{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
		{ID: "d"},
	}
	pending := map[string]model.StepDef{
		"b": allSteps[1],
		"c": allSteps[2],
		"d": allSteps[3],
	}
	outputs := map[string]*model.StepOutput{}

	skipDownstream(pending, "a", allSteps, outputs)

	// "b" depends on "a", should be skipped
	assert.Equal(t, model.StepStatusSkipped, outputs["b"].Status)
	// "c" depends on "b", should also be skipped (recursive)
	assert.Equal(t, model.StepStatusSkipped, outputs["c"].Status)
	// "d" has no dependency on "a", should still be pending
	_, stillPending := pending["d"]
	assert.True(t, stillPending)
}

func TestEvalCondition_Always(t *testing.T) {
	assert.True(t, evalCondition("true", nil, nil))
}

func TestEvalCondition_StepStatus(t *testing.T) {
	outputs := map[string]*model.StepOutput{
		"step-a": {Status: model.StepStatusComplete},
	}
	assert.True(t, evalCondition(`{{eq (index .steps "step-a").status "complete"}}`, nil, outputs))
	assert.False(t, evalCondition(`{{eq (index .steps "step-a").status "failed"}}`, nil, outputs))
}

func TestEvalCondition_Empty(t *testing.T) {
	assert.True(t, evalCondition("", nil, nil))
}

func TestEvalCondition_InvalidTemplate(t *testing.T) {
	assert.False(t, evalCondition("{{broken", nil, nil))
}

func TestResolveStep_NilExecution(t *testing.T) {
	step := model.StepDef{ID: "action-step"}
	opts, err := resolveStep(step, nil, nil)
	assert.NoError(t, err)
	assert.Empty(t, opts.Prompt)
}

func TestResolveStep_WithPrompt(t *testing.T) {
	step := model.StepDef{
		ID: "analyze",
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Analyze the repo",
		},
	}
	opts, err := resolveStep(step, map[string]any{}, map[string]*model.StepOutput{})
	assert.NoError(t, err)
	assert.Equal(t, "Analyze the repo", opts.Prompt)
	assert.Equal(t, "claude-code", opts.Agent)
}

func TestResolveStep_DefaultAgent(t *testing.T) {
	step := model.StepDef{
		ID: "s1",
		Execution: &model.ExecutionDef{
			Prompt: "Do something",
		},
	}
	opts, err := resolveStep(step, map[string]any{}, map[string]*model.StepOutput{})
	assert.NoError(t, err)
	assert.Equal(t, "claude-code", opts.Agent)
}

func TestResolveStep_WithStaticRepos(t *testing.T) {
	step := model.StepDef{
		ID: "transform",
		Repositories: []any{
			map[string]any{"url": "https://github.com/acme/api", "branch": "main"},
			map[string]any{"url": "https://github.com/acme/web"},
		},
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Fix TODOs",
		},
	}
	opts, err := resolveStep(step, map[string]any{}, map[string]*model.StepOutput{})
	assert.NoError(t, err)
	assert.Len(t, opts.Repos, 2)
	assert.Equal(t, "https://github.com/acme/api", opts.Repos[0].URL)
	assert.Equal(t, "main", opts.Repos[0].Branch)
	assert.Equal(t, "https://github.com/acme/web", opts.Repos[1].URL)
}

func TestResolveStep_NilRepositories(t *testing.T) {
	step := model.StepDef{
		ID: "no-repos",
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Fix TODOs",
		},
	}
	opts, err := resolveStep(step, map[string]any{}, map[string]*model.StepOutput{})
	assert.NoError(t, err)
	assert.Empty(t, opts.Repos)
}

// toJSON is registered in internal/template/render.go, so we use it here.
func TestResolveStep_WithTemplatedRepos(t *testing.T) {
	step := model.StepDef{
		ID:           "transform",
		Repositories: `{{ toJSON .Params.repos }}`,
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Fix TODOs",
		},
	}
	params := map[string]any{
		"repos": []any{
			map[string]any{"url": "https://github.com/acme/api"},
		},
	}
	opts, err := resolveStep(step, params, map[string]*model.StepOutput{})
	assert.NoError(t, err)
	assert.Len(t, opts.Repos, 1)
	assert.Equal(t, "https://github.com/acme/api", opts.Repos[0].URL)
}

func TestAggregateFanOut_AllComplete(t *testing.T) {
	results := []*model.StepOutput{
		{StepID: "transform", Status: model.StepStatusComplete, Diff: "diff1"},
		{StepID: "transform", Status: model.StepStatusComplete, Diff: "diff2"},
	}
	agg := aggregateFanOut("transform", results)
	assert.Equal(t, model.StepStatusComplete, agg.Status)
	assert.Len(t, agg.Outputs, 2)
	assert.Equal(t, "transform", agg.StepID)
}

func TestAggregateFanOut_WithFailure(t *testing.T) {
	results := []*model.StepOutput{
		{StepID: "transform", Status: model.StepStatusComplete},
		{StepID: "transform", Status: model.StepStatusFailed, Error: "timeout"},
	}
	agg := aggregateFanOut("transform", results)
	assert.Equal(t, model.StepStatusFailed, agg.Status)
	assert.Len(t, agg.Outputs, 2)
	assert.Equal(t, "timeout", agg.Error)
}

func TestAggregateFanOut_MultipleFailures(t *testing.T) {
	results := []*model.StepOutput{
		{StepID: "transform", Status: model.StepStatusFailed, Error: "clone failed"},
		{StepID: "transform", Status: model.StepStatusFailed, Error: "timeout"},
	}
	agg := aggregateFanOut("transform", results)
	assert.Equal(t, model.StepStatusFailed, agg.Status)
	assert.Len(t, agg.Outputs, 2)
	assert.Contains(t, agg.Error, "clone failed")
	assert.Contains(t, agg.Error, "timeout")
}

func TestFanOutApprovalPolicyOverride(t *testing.T) {
	// This test documents the expected behavior: fan-out steps must never have
	// HITL approval_policy other than "never" to prevent signal routing hangs.
	// The guard in DAGWorkflow enforces this at runtime.
	t.Log("fan-out HITL guard is enforced in DAGWorkflow — see dag.go fan-out section")
}
