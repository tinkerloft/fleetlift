package workflow

import (
	"fmt"
	"regexp"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// ValidationError describes a single structural or semantic problem found in a WorkflowDef.
type ValidationError struct {
	StepID  string
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.StepID != "" {
		return fmt.Sprintf("step %q field %q: %s", e.StepID, e.Field, e.Message)
	}
	return fmt.Sprintf("field %q: %s", e.Field, e.Message)
}

var stepIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var validActionTypes = map[string]bool{
	"slack_notify":     true,
	"github_pr_review": true,
	"github_assign":    true,
	"github_label":     true,
	"github_comment":   true,
	"create_pr":        true,
}

var validAgentTypes = map[string]bool{
	"claude-code": true,
	"codex":       true,
	"shell":       true,
	"":            true,
}

var credNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

var reservedCredNames = map[string]bool{
	"PATH":            true,
	"HOME":            true,
	"LD_PRELOAD":      true,
	"LD_LIBRARY_PATH": true,
	"USER":            true,
	"SHELL":           true,
}

// ValidateWorkflow runs all validation checks on def and params, returning all errors found.
func ValidateWorkflow(def model.WorkflowDef, params map[string]any) []ValidationError {
	var errs []ValidationError
	errs = append(errs, validateStructure(def)...)
	errs = append(errs, validateParameters(def, params)...)
	errs = append(errs, validateActionTypes(def)...)
	errs = append(errs, validateAgentTypes(def)...)
	errs = append(errs, validateCredentialNames(def)...)
	errs = append(errs, validateTemplateRefs(def)...)
	return errs
}

// validateStructure checks step ID validity, dependency references, cycles, and step type exclusivity.
func validateStructure(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError

	// Check for empty/invalid step IDs and duplicate step IDs.
	seen := make(map[string]bool, len(def.Steps))
	for _, step := range def.Steps {
		if step.ID == "" {
			errs = append(errs, ValidationError{StepID: "", Field: "id", Message: "step ID must not be empty"})
			continue
		}
		if !stepIDRe.MatchString(step.ID) {
			errs = append(errs, ValidationError{StepID: step.ID, Field: "id", Message: fmt.Sprintf("step ID %q must match ^[a-z][a-z0-9_-]*$", step.ID)})
		}
		if seen[step.ID] {
			errs = append(errs, ValidationError{StepID: step.ID, Field: "id", Message: fmt.Sprintf("duplicate step ID %q", step.ID)})
		}
		seen[step.ID] = true
	}

	// Build a set of valid step IDs for depends_on validation and cycle detection.
	// Only include IDs that passed format validation.
	validIDs := make(map[string]bool, len(def.Steps))
	for _, step := range def.Steps {
		if step.ID != "" && stepIDRe.MatchString(step.ID) {
			validIDs[step.ID] = true
		}
	}

	// Check depends_on references point to known step IDs.
	for _, step := range def.Steps {
		for _, dep := range step.DependsOn {
			if !validIDs[dep] {
				errs = append(errs, ValidationError{StepID: step.ID, Field: "depends_on", Message: fmt.Sprintf("unknown step dependency %q", dep)})
			}
		}
	}

	// Cycle detection via Kahn's algorithm.
	// Build in-degree map and adjacency list over valid IDs only.
	inDegree := make(map[string]int, len(def.Steps))
	adj := make(map[string][]string, len(def.Steps))
	for _, step := range def.Steps {
		if !validIDs[step.ID] {
			continue
		}
		if _, ok := inDegree[step.ID]; !ok {
			inDegree[step.ID] = 0
		}
		for _, dep := range step.DependsOn {
			if !validIDs[dep] {
				continue
			}
			adj[dep] = append(adj[dep], step.ID)
			inDegree[step.ID]++
		}
	}

	queue := make([]string, 0, len(inDegree))
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	processed := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		processed++
		for _, next := range adj[cur] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if processed < len(inDegree) {
		errs = append(errs, ValidationError{StepID: "", Field: "depends_on", Message: "cycle detected in step dependencies"})
	}

	// Each step must have exactly one of execution or action.
	for _, step := range def.Steps {
		hasExec := step.Execution != nil
		hasAction := step.Action != nil
		if hasExec && hasAction {
			errs = append(errs, ValidationError{StepID: step.ID, Field: "type", Message: "step must have exactly one of 'execution' or 'action', not both"})
		} else if !hasExec && !hasAction {
			errs = append(errs, ValidationError{StepID: step.ID, Field: "type", Message: "step must have exactly one of 'execution' or 'action'"})
		}
	}

	return errs
}

// validateParameters checks that required params are present and values match declared types.
func validateParameters(def model.WorkflowDef, params map[string]any) []ValidationError {
	var errs []ValidationError
	if params == nil {
		params = map[string]any{}
	}
	for _, p := range def.Parameters {
		val, provided := params[p.Name]
		if !provided {
			if p.Required {
				errs = append(errs, ValidationError{Field: p.Name, Message: fmt.Sprintf("required parameter %q is missing", p.Name)})
			}
			continue
		}
		if err := checkParamType(p.Name, p.Type, val); err != nil {
			errs = append(errs, *err)
		}
	}
	return errs
}

func checkParamType(name, typ string, val any) *ValidationError {
	switch typ {
	case "string":
		if _, ok := val.(string); !ok {
			return &ValidationError{Field: name, Message: fmt.Sprintf("parameter %q must be a string, got %T", name, val)}
		}
	case "bool":
		if _, ok := val.(bool); !ok {
			return &ValidationError{Field: name, Message: fmt.Sprintf("parameter %q must be a bool, got %T", name, val)}
		}
	case "int":
		switch v := val.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// ok
		case float32:
			if v != float32(int64(v)) {
				return &ValidationError{Field: name, Message: fmt.Sprintf("parameter %q must be an integer (no fractional part), got %v", name, val)}
			}
		case float64:
			if v != float64(int64(v)) {
				return &ValidationError{Field: name, Message: fmt.Sprintf("parameter %q must be an integer (no fractional part), got %v", name, val)}
			}
		default:
			return &ValidationError{Field: name, Message: fmt.Sprintf("parameter %q must be an integer, got %T", name, val)}
		}
	case "json":
		switch val.(type) {
		case map[string]any, []any:
			// ok
		default:
			return &ValidationError{Field: name, Message: fmt.Sprintf("parameter %q must be a JSON object or array, got %T", name, val)}
		}
	}
	return nil
}

// validateActionTypes checks that action steps use a known action type.
func validateActionTypes(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError
	for _, step := range def.Steps {
		if step.Action == nil {
			continue
		}
		if !validActionTypes[step.Action.Type] {
			errs = append(errs, ValidationError{StepID: step.ID, Field: "action.type", Message: fmt.Sprintf("unknown action type %q", step.Action.Type)})
		}
	}
	return errs
}

// validateAgentTypes checks that execution steps use a known agent type.
func validateAgentTypes(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError
	for _, step := range def.Steps {
		if step.Execution == nil {
			continue
		}
		if !validAgentTypes[step.Execution.Agent] {
			errs = append(errs, ValidationError{StepID: step.ID, Field: "execution.agent", Message: fmt.Sprintf("unknown agent type %q", step.Execution.Agent)})
		}
	}
	return errs
}

// validateCredentialNames checks that execution credential names are well-formed and not reserved.
func validateCredentialNames(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError
	for _, step := range def.Steps {
		if step.Execution == nil {
			continue
		}
		for _, cred := range step.Execution.Credentials {
			if !credNameRe.MatchString(cred) {
				errs = append(errs, ValidationError{StepID: step.ID, Field: "credentials", Message: fmt.Sprintf("credential name %q must match ^[A-Z][A-Z0-9_]*$", cred)})
				continue
			}
			if reservedCredNames[cred] {
				errs = append(errs, ValidationError{StepID: step.ID, Field: "credentials", Message: fmt.Sprintf("credential name %q is reserved and cannot be used", cred)})
			}
		}
	}
	return errs
}

// validateTemplateRefs is a stub — template ref validation is implemented in Task 2.
func validateTemplateRefs(def model.WorkflowDef) []ValidationError {
	return nil
}
