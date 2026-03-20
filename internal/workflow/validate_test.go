package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// helper to build a minimal valid WorkflowDef with a single execution step
func validSingleStepDef() model.WorkflowDef {
	return model.WorkflowDef{
		Version: 1,
		ID:      "test-workflow",
		Title:   "Test",
		Steps: []model.StepDef{
			{
				ID: "step-one",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}
}

func TestValidateWorkflow_Valid(t *testing.T) {
	def := validSingleStepDef()
	errs := ValidateWorkflow(def, nil)
	assert.Empty(t, errs)
}

func TestValidateWorkflow_DuplicateStepIDs(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "y"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "id" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected duplicate step ID error, got %v", errs)
}

func TestValidateWorkflow_EmptyStepID(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Field == "id" && strings.Contains(e.Message, "empty") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected empty step ID error, got %v", errs)
}

func TestValidateWorkflow_InvalidStepIDChars(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "Step_One!", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "Step_One!" && e.Field == "id" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected invalid step ID chars error, got %v", errs)
}

func TestValidateWorkflow_UnknownDependsOn(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", DependsOn: []string{"nonexistent"}, Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "depends_on" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected unknown depends_on error, got %v", errs)
}

func TestValidateWorkflow_CircularDependency(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-a", DependsOn: []string{"step-b"}, Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
			{ID: "step-b", DependsOn: []string{"step-a"}, Execution: &model.ExecutionDef{Agent: "shell", Prompt: "y"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Field == "depends_on" && strings.Contains(e.Message, "cycle") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected circular dependency error, got %v", errs)
}

func TestValidateWorkflow_SelfCycle(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "a", DependsOn: []string{"a"}, Execution: &model.ExecutionDef{Prompt: "hi"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	// Should have depends_on error (unknown step "a" — since "a" tries to depend on itself
	// which is technically a valid step ID but creates a cycle)
	// OR circular dependency error
	// Either is acceptable as long as the workflow is rejected
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "circular") || strings.Contains(e.Message, "depends_on") || e.Field == "depends_on" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected circular or depends_on error for self-cycle")
}

func TestValidateWorkflow_StepTypeBothExecAndAction(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID:        "step-one",
				Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"},
				Action:    &model.ActionDef{Type: "slack_notify"},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "type" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected step type conflict error, got %v", errs)
}

func TestValidateWorkflow_StepTypeNeither(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one"},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "type" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected missing step type error, got %v", errs)
}

func TestValidateWorkflow_RequiredParam(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "repo_url", Type: "string", Required: true},
		},
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, map[string]any{})
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.Field == "repo_url" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected required param error, got %v", errs)
}

func TestValidateWorkflow_ParamTypeString(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "greeting", Type: "string", Required: true},
		},
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	// Passing an int when string is expected
	errs := ValidateWorkflow(def, map[string]any{"greeting": 42})
	found := false
	for _, e := range errs {
		if e.Field == "greeting" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected param type error for string, got %v", errs)

	// Passing a string — should be fine
	errs = ValidateWorkflow(def, map[string]any{"greeting": "hello"})
	for _, e := range errs {
		assert.NotEqual(t, "greeting", e.Field, "unexpected param type error for valid string, got %v", errs)
	}
}

func TestValidateWorkflow_ParamTypeBool(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "flag", Type: "bool", Required: true},
		},
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, map[string]any{"flag": "true"})
	found := false
	for _, e := range errs {
		if e.Field == "flag" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected bool type error when string passed, got %v", errs)

	errs = ValidateWorkflow(def, map[string]any{"flag": true})
	for _, e := range errs {
		assert.NotEqual(t, "flag", e.Field, "unexpected error for valid bool, got %v", errs)
	}
}

func TestValidateWorkflow_ParamTypeInt(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "count", Type: "int", Required: true},
		},
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	// Fractional float should fail
	errs := ValidateWorkflow(def, map[string]any{"count": 3.5})
	found := false
	for _, e := range errs {
		if e.Field == "count" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected int type error for fractional float, got %v", errs)

	// Whole-number float should pass (json numbers come as float64)
	errs = ValidateWorkflow(def, map[string]any{"count": float64(3)})
	for _, e := range errs {
		assert.NotEqual(t, "count", e.Field, "unexpected error for whole-number float as int, got %v", errs)
	}

	// Plain int should pass
	errs = ValidateWorkflow(def, map[string]any{"count": 5})
	for _, e := range errs {
		assert.NotEqual(t, "count", e.Field, "unexpected error for plain int, got %v", errs)
	}
}

func TestValidateWorkflow_ParamTypeJSON(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "config", Type: "json", Required: true},
		},
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	// String is not json
	errs := ValidateWorkflow(def, map[string]any{"config": "hello"})
	found := false
	for _, e := range errs {
		if e.Field == "config" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected json type error for string, got %v", errs)

	// Map is ok
	errs = ValidateWorkflow(def, map[string]any{"config": map[string]any{"key": "val"}})
	for _, e := range errs {
		assert.NotEqual(t, "config", e.Field, "unexpected error for map json param, got %v", errs)
	}

	// Slice is ok
	errs = ValidateWorkflow(def, map[string]any{"config": []any{"a", "b"}})
	for _, e := range errs {
		assert.NotEqual(t, "config", e.Field, "unexpected error for slice json param, got %v", errs)
	}
}

func TestValidateWorkflow_UnknownActionType(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", Action: &model.ActionDef{Type: "send_email"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "action.type" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected unknown action type error, got %v", errs)
}

func TestValidateWorkflow_UnknownAgentType(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "gpt-4", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "execution.agent" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected unknown agent type error, got %v", errs)
}

func TestValidateWorkflow_CredentialNameFormat(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "step-one",
				Execution: &model.ExecutionDef{
					Agent:       "shell",
					Prompt:      "x",
					Credentials: []string{"invalid-name"},
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "credentials" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected credential name format error, got %v", errs)
}

func TestValidateWorkflow_ReservedCredentialName(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "step-one",
				Execution: &model.ExecutionDef{
					Agent:       "shell",
					Prompt:      "x",
					Credentials: []string{"PATH"},
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "credentials" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected reserved credential name error, got %v", errs)
}

// --- Template ref validation tests ---

func TestValidateWorkflow_UnknownParamRef(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "repo_url", Type: "string"},
		},
		Steps: []model.StepDef{
			{
				ID: "step-one",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "clone {{ .Params.typo }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "typo") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error containing 'typo', got %v", errs)
}

func TestValidateWorkflow_ValidParamRef(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "repo_url", Type: "string"},
		},
		Steps: []model.StepDef{
			{
				ID: "step-one",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "clone {{ .Params.repo_url }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	for _, e := range errs {
		assert.False(t, strings.Contains(e.Message, "repo_url"), "unexpected error for valid param ref, got %v", errs)
	}
}

func TestValidateWorkflow_UnknownStepRef(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "a",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "do something",
				},
			},
			{
				ID:        "b",
				DependsOn: []string{"a"},
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "use {{ .Steps.missing.Output.result }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "missing") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error containing 'missing', got %v", errs)
}

func TestValidateWorkflow_StepRefNotUpstream(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "a",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "do something",
				},
			},
			{
				ID: "b",
				// b does NOT depend on a
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "use {{ .Steps.a.Output.result }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if strings.Contains(strings.ToLower(e.Message), "upstream") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error containing 'upstream', got %v", errs)
}

func TestValidateWorkflow_ValidStepRef(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "a",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "do something",
				},
			},
			{
				ID:        "b",
				DependsOn: []string{"a"},
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "use {{ .Steps.a.Output.result }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	for _, e := range errs {
		assert.False(t, strings.Contains(e.Message, "upstream") || strings.Contains(e.Message, "unknown step"),
			"unexpected template ref error, got %v", errs)
	}
}

func TestValidateWorkflow_SchemaFieldRef(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "a",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "do something",
					Output: &model.OutputSchemaDef{
						Schema: map[string]any{"result": "string"},
					},
				},
			},
			{
				ID:        "b",
				DependsOn: []string{"a"},
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "use {{ .Steps.a.Output.result }} and {{ .Steps.a.Output.missing_field }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	schemaErrs := 0
	for _, e := range errs {
		if strings.Contains(e.Message, "missing_field") {
			schemaErrs++
		}
		assert.False(t, strings.Contains(e.Message, "result") && !strings.Contains(e.Message, "missing"),
			"unexpected error for valid schema field 'result', got %v", e)
	}
	assert.Equal(t, 1, schemaErrs, "expected exactly 1 error for missing_field, got %v", errs)
}

func TestValidateWorkflow_ConditionRef(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "a",
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "do something",
				},
			},
			{
				ID:        "b",
				DependsOn: []string{"a"},
				Condition: `{{ eq (index .steps "a").status "complete" }}`,
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "run b",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	for _, e := range errs {
		assert.False(t, strings.Contains(e.Message, "unknown step") || strings.Contains(e.Message, "upstream"),
			"unexpected error for valid condition ref, got %v", e)
	}
}

func TestValidateWorkflow_ConditionUnknownParamRef(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{{Name: "env", Type: "string"}},
		Steps: []model.StepDef{
			{ID: "a", Execution: &model.ExecutionDef{Prompt: "hi"}},
			{
				ID:        "b",
				DependsOn: []string{"a"},
				Condition: `{{ eq .params.typo "prod" }}`,
				Execution: &model.ExecutionDef{Prompt: "run"},
			},
		},
	}
	errs := ValidateWorkflow(def, map[string]any{"env": "prod"})
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "typo") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error for undefined param 'typo' in condition")
}

func TestValidateWorkflow_ConditionUnknownStep(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "a", Execution: &model.ExecutionDef{Prompt: "hi"}},
			{
				ID:        "b",
				DependsOn: []string{"a"},
				Condition: `{{ eq .steps.ghost.status "complete" }}`,
				Execution: &model.ExecutionDef{Prompt: "run"},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "ghost") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error for undefined step 'ghost' in condition")
}

func TestValidateWorkflow_ActionCredentialNameFormat(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "a", Action: &model.ActionDef{
				Type:        "slack_notify",
				Credentials: []string{"invalid-name"},
			}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "invalid-name") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error for invalid action credential name")
}

// --- extractTemplateRefs unit tests ---

func TestExtractTemplateRefs_ParamAndStepRefs(t *testing.T) {
	paramRefs, stepRefs, err := extractTemplateRefs(`{{ .Params.repo_url }} {{ .Steps.clone.Output.diff }}`, false)
	assert.NoError(t, err)
	assert.Equal(t, []string{"repo_url"}, paramRefs)
	assert.Len(t, stepRefs, 1)
	assert.Equal(t, StepRef{StepID: "clone", Field: "Output", OutputKey: "diff"}, stepRefs[0])
}

func TestExtractTemplateRefs_ConditionContext(t *testing.T) {
	paramRefs, stepRefs, err := extractTemplateRefs(`{{ .steps.clone.status }}`, true)
	assert.NoError(t, err)
	assert.Empty(t, paramRefs)
	assert.Len(t, stepRefs, 1)
	assert.Equal(t, StepRef{StepID: "clone", Field: "status", OutputKey: ""}, stepRefs[0])
}

func TestExtractTemplateRefs_EmptyTemplate(t *testing.T) {
	paramRefs, stepRefs, err := extractTemplateRefs("", false)
	assert.NoError(t, err)
	assert.Nil(t, paramRefs)
	assert.Nil(t, stepRefs)
}

func TestExtractTemplateRefs_InvalidTemplate(t *testing.T) {
	// Unclosed template action — parse error should be silently ignored
	paramRefs, stepRefs, err := extractTemplateRefs("{{ .Params.x", false)
	assert.NoError(t, err)
	assert.Nil(t, paramRefs)
	assert.Nil(t, stepRefs)
}

// --- Action config and output ref validation tests ---

func TestValidateWorkflow_ActionConfigMissingRequired(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "notify", Action: &model.ActionDef{
				Type:   "slack_notify",
				Config: map[string]any{"channel": "#general"},
				// missing "message"
			}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if e.StepID == "notify" && e.Field == "action.config" && strings.Contains(e.Message, "message") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected missing required config key error, got %v", errs)
}

func TestValidateWorkflow_ActionConfigUnknownKey(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "notify", Action: &model.ActionDef{
				Type:   "slack_notify",
				Config: map[string]any{"channel": "#general", "message": "hi", "chanel": "typo"},
			}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if e.StepID == "notify" && strings.Contains(e.Message, "chanel") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected unknown config key error for 'chanel', got %v", errs)
}

func TestValidateWorkflow_ActionOutputRefValidation(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "label",
				Action: &model.ActionDef{
					Type: "github_label",
					Config: map[string]any{
						"repo_url":     "https://github.com/org/repo",
						"issue_number": 42,
						"labels":       []any{"bug"},
					},
				},
			},
			{
				ID:        "report",
				DependsOn: []string{"label"},
				Execution: &model.ExecutionDef{
					Agent:  "shell",
					Prompt: "Labels: {{ .Steps.label.Output.labels }} and {{ .Steps.label.Output.nonexistent }}",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "nonexistent") && strings.Contains(e.Message, "github_label") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected error for undeclared action output field 'nonexistent', got %v", errs)
}

func TestValidateWorkflow_ActionConfigTemplateSkipsType(t *testing.T) {
	def := model.WorkflowDef{
		Parameters: []model.ParameterDef{
			{Name: "issue_number", Type: "int"},
		},
		Steps: []model.StepDef{
			{ID: "label", Action: &model.ActionDef{
				Type: "github_label",
				Config: map[string]any{
					"repo_url":     "https://github.com/org/repo",
					"issue_number": "{{ .Params.issue_number }}",
					"labels":       []any{"bug"},
				},
			}},
		},
	}
	errs := ValidateWorkflow(def, map[string]any{"issue_number": 42})
	for _, e := range errs {
		assert.False(t, strings.Contains(e.Message, "issue_number") && strings.Contains(e.Message, "int"),
			"template value should skip type check, got %v", e)
	}
}

// --- Sandbox group validation tests ---

func TestValidateWorkflow_SandboxGroupValid(t *testing.T) {
	def := model.WorkflowDef{
		SandboxGroups: map[string]model.SandboxGroupDef{
			"test-group": {Image: "claude-code:latest"},
		},
		Steps: []model.StepDef{
			{
				ID:           "step-one",
				SandboxGroup: "test-group",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if e.Field == "sandbox.image" {
			found = true
			break
		}
	}
	assert.False(t, found, "expected no sandbox.image error for valid sandbox group usage, got %v", errs)
}

func TestValidateWorkflow_SandboxGroupWithStepImageConflict(t *testing.T) {
	def := model.WorkflowDef{
		SandboxGroups: map[string]model.SandboxGroupDef{
			"test-group": {Image: "claude-code:latest"},
		},
		Steps: []model.StepDef{
			{
				ID:           "step-one",
				SandboxGroup: "test-group",
				Sandbox: &model.SandboxSpec{
					Image: "different-image:latest",
				},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if e.StepID == "step-one" && e.Field == "sandbox.image" && strings.Contains(e.Message, "sandbox_group") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected sandbox.image conflict error, got %v", errs)
}

func TestValidateWorkflow_SandboxGroupEmptyImageAllowed(t *testing.T) {
	// A step in a sandbox_group with Sandbox but empty Image should be allowed
	def := model.WorkflowDef{
		SandboxGroups: map[string]model.SandboxGroupDef{
			"test-group": {Image: "claude-code:latest"},
		},
		Steps: []model.StepDef{
			{
				ID:           "step-one",
				SandboxGroup: "test-group",
				Sandbox:      &model.SandboxSpec{},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if e.Field == "sandbox.image" {
			found = true
			break
		}
	}
	assert.False(t, found, "expected no sandbox.image error when Image is empty, got %v", errs)
}

func TestValidateWorkflow_NoSandboxGroupWithStepImage(t *testing.T) {
	// A step not in a sandbox_group can specify its own image
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID: "step-one",
				Sandbox: &model.SandboxSpec{
					Image: "custom-image:latest",
				},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}
	errs := ValidateWorkflow(def, nil)
	found := false
	for _, e := range errs {
		if e.Field == "sandbox.image" {
			found = true
			break
		}
	}
	assert.False(t, found, "expected no sandbox.image error when not in a group, got %v", errs)
}
