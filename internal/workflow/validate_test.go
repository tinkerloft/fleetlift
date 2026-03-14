package workflow

import (
	"testing"

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
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid def, got %v", errs)
	}
}

func TestValidateWorkflow_DuplicateStepIDs(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "y"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "step-one", "id", "") {
		t.Errorf("expected duplicate step ID error, got %v", errs)
	}
}

func TestValidateWorkflow_EmptyStepID(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "", "id", "") {
		t.Errorf("expected empty step ID error, got %v", errs)
	}
}

func TestValidateWorkflow_InvalidStepIDChars(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "Step_One!", Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "Step_One!", "id", "") {
		t.Errorf("expected invalid step ID chars error, got %v", errs)
	}
}

func TestValidateWorkflow_UnknownDependsOn(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", DependsOn: []string{"nonexistent"}, Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "step-one", "depends_on", "") {
		t.Errorf("expected unknown depends_on error, got %v", errs)
	}
}

func TestValidateWorkflow_CircularDependency(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-a", DependsOn: []string{"step-b"}, Execution: &model.ExecutionDef{Agent: "shell", Prompt: "x"}},
			{ID: "step-b", DependsOn: []string{"step-a"}, Execution: &model.ExecutionDef{Agent: "shell", Prompt: "y"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "", "depends_on", "cycle") {
		t.Errorf("expected circular dependency error, got %v", errs)
	}
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
	if !hasError(errs, "step-one", "type", "") {
		t.Errorf("expected step type conflict error, got %v", errs)
	}
}

func TestValidateWorkflow_StepTypeNeither(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one"},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "step-one", "type", "") {
		t.Errorf("expected missing step type error, got %v", errs)
	}
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
	if !hasError(errs, "", "repo_url", "") {
		t.Errorf("expected required param error, got %v", errs)
	}
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
	if !hasError(errs, "", "greeting", "") {
		t.Errorf("expected param type error for string, got %v", errs)
	}
	// Passing a string — should be fine
	errs = ValidateWorkflow(def, map[string]any{"greeting": "hello"})
	if hasError(errs, "", "greeting", "") {
		t.Errorf("unexpected param type error for valid string, got %v", errs)
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
	if !hasError(errs, "", "flag", "") {
		t.Errorf("expected bool type error when string passed, got %v", errs)
	}
	errs = ValidateWorkflow(def, map[string]any{"flag": true})
	if hasError(errs, "", "flag", "") {
		t.Errorf("unexpected error for valid bool, got %v", errs)
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
	if !hasError(errs, "", "count", "") {
		t.Errorf("expected int type error for fractional float, got %v", errs)
	}
	// Whole-number float should pass (json numbers come as float64)
	errs = ValidateWorkflow(def, map[string]any{"count": float64(3)})
	if hasError(errs, "", "count", "") {
		t.Errorf("unexpected error for whole-number float as int, got %v", errs)
	}
	// Plain int should pass
	errs = ValidateWorkflow(def, map[string]any{"count": 5})
	if hasError(errs, "", "count", "") {
		t.Errorf("unexpected error for plain int, got %v", errs)
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
	if !hasError(errs, "", "config", "") {
		t.Errorf("expected json type error for string, got %v", errs)
	}
	// Map is ok
	errs = ValidateWorkflow(def, map[string]any{"config": map[string]any{"key": "val"}})
	if hasError(errs, "", "config", "") {
		t.Errorf("unexpected error for map json param, got %v", errs)
	}
	// Slice is ok
	errs = ValidateWorkflow(def, map[string]any{"config": []any{"a", "b"}})
	if hasError(errs, "", "config", "") {
		t.Errorf("unexpected error for slice json param, got %v", errs)
	}
}

func TestValidateWorkflow_UnknownActionType(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", Action: &model.ActionDef{Type: "send_email"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "step-one", "action.type", "") {
		t.Errorf("expected unknown action type error, got %v", errs)
	}
}

func TestValidateWorkflow_UnknownAgentType(t *testing.T) {
	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-one", Execution: &model.ExecutionDef{Agent: "gpt-4", Prompt: "x"}},
		},
	}
	errs := ValidateWorkflow(def, nil)
	if !hasError(errs, "step-one", "execution.agent", "") {
		t.Errorf("expected unknown agent type error, got %v", errs)
	}
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
	if !hasError(errs, "step-one", "credentials", "") {
		t.Errorf("expected credential name format error, got %v", errs)
	}
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
	if !hasError(errs, "step-one", "credentials", "") {
		t.Errorf("expected reserved credential name error, got %v", errs)
	}
}

// hasError checks if any ValidationError in the slice matches the given criteria.
// Empty strings are treated as wildcards (match any value).
func hasError(errs []ValidationError, stepID, field, msgSubstr string) bool {
	for _, e := range errs {
		stepMatch := stepID == "" || e.StepID == stepID
		fieldMatch := field == "" || e.Field == field
		msgMatch := msgSubstr == "" || contains(e.Message, msgSubstr)
		if stepMatch && fieldMatch && msgMatch {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
