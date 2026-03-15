# H1–H3 Workflow Reliability Implementation Plan

> **Status: COMPLETE** — All 7 tasks implemented and merged. See commits `934a924`–`c2fb23b` on `workflow_reliability` branch.

**Goal:** Catch invalid YAML workflow configs before execution (H1), give action steps access to the credential store with logging (H2), and enforce declared output schemas on agent steps (H3).

**Architecture:** Three independent tracks sharing no state. H1 is pure validation in the HTTP handler; H2 extends the Temporal activity interface with credential plumbing and richer return values; H3 wraps agent prompts with schema instructions and validates agent output post-completion.

**Tech Stack:** Go 1.22, `text/template/parse` (AST walking), `go.temporal.io/sdk`, `testify`, existing `batchInsertLogs`/`CredStore.GetBatch` infra.

---

## Chunk 1: H1 — Workflow Validation

### Task 1: ValidationError types and structural checks

**Files:**
- Create: `internal/workflow/validate.go`
- Create: `internal/workflow/validate_test.go`

- [x] **Step 1: Write failing tests for structural validation**

```go
// internal/workflow/validate_test.go
package workflow

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/tinkerloft/fleetlift/internal/model"
)

func TestValidateWorkflow_DuplicateStepIDs(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "hello"}},
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "world"}},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Len(t, errs, 1)
    assert.Equal(t, "a", errs[0].StepID)
    assert.Contains(t, errs[0].Message, "duplicate")
}

func TestValidateWorkflow_EmptyStepID(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "", Execution: &model.ExecutionDef{Prompt: "hi"}},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Len(t, errs, 1)
    assert.Contains(t, errs[0].Field, "id")
}

func TestValidateWorkflow_InvalidStepIDChars(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "Bad Step!", Execution: &model.ExecutionDef{Prompt: "hi"}},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.NotEmpty(t, errs)
}

func TestValidateWorkflow_UnknownDependsOn(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "b", DependsOn: []string{"nonexistent"}, Execution: &model.ExecutionDef{Prompt: "hi"}},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Len(t, errs, 1)
    assert.Equal(t, "b", errs[0].StepID)
    assert.Contains(t, errs[0].Field, "depends_on")
}

func TestValidateWorkflow_CircularDependency(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", DependsOn: []string{"b"}, Execution: &model.ExecutionDef{Prompt: "hi"}},
            {ID: "b", DependsOn: []string{"a"}, Execution: &model.ExecutionDef{Prompt: "hi"}},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Len(t, errs, 1)
    assert.Contains(t, errs[0].Message, "circular")
}

func TestValidateWorkflow_StepTypeBothExecAndAction(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {
                ID:        "a",
                Execution: &model.ExecutionDef{Prompt: "hi"},
                Action:    &model.ActionDef{Type: "slack_notify"},
            },
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.NotEmpty(t, errs)
    assert.Contains(t, errs[0].Message, "exactly one")
}

func TestValidateWorkflow_StepTypeNeither(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a"},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.NotEmpty(t, errs)
    assert.Contains(t, errs[0].Message, "exactly one")
}

func TestValidateWorkflow_Valid(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "hello"}},
            {ID: "b", DependsOn: []string{"a"}, Execution: &model.ExecutionDef{Prompt: "world"}},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Empty(t, errs)
}
```

- [x] **Step 2: Run tests — expect FAIL (ValidateWorkflow undefined)**

```
go test ./internal/workflow/... -run TestValidateWorkflow -v
```
Expected: compile error — `ValidateWorkflow` not defined.

- [x] **Step 3: Create validate.go with ValidationError and structural checks**

```go
// internal/workflow/validate.go
package workflow

import (
    "fmt"
    "regexp"

    "github.com/tinkerloft/fleetlift/internal/model"
)

// ValidationError describes a single validation failure.
type ValidationError struct {
    StepID  string `json:"step_id,omitempty"` // empty for workflow-level errors
    Field   string `json:"field,omitempty"`
    Message string `json:"message"`
}

func (e ValidationError) Error() string {
    if e.StepID != "" {
        return fmt.Sprintf("step %s (%s): %s", e.StepID, e.Field, e.Message)
    }
    return e.Message
}

var stepIDRegexp = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var knownActionTypes = map[string]bool{
    "slack_notify":    true,
    "github_pr_review": true,
    "github_assign":   true,
    "github_label":    true,
    "github_comment":  true,
    "create_pr":       true,
}

var knownAgentTypes = map[string]bool{
    "claude-code": true,
    "codex":       true,
    "shell":       true,
    "":            true, // empty defaults to claude-code
}

var reservedEnvVars = map[string]bool{
    "PATH": true, "HOME": true, "LD_PRELOAD": true,
    "LD_LIBRARY_PATH": true, "USER": true, "SHELL": true,
}

var credNameRegexp = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ValidateWorkflow validates a parsed WorkflowDef against supplied runtime parameters.
// Returns all validation errors found (not just the first).
func ValidateWorkflow(def model.WorkflowDef, params map[string]any) []ValidationError {
    var errs []ValidationError

    errs = append(errs, validateStructure(def)...)
    errs = append(errs, validateParameters(def, params)...)
    errs = append(errs, validateTemplateRefs(def)...)
    errs = append(errs, validateActionTypes(def)...)
    errs = append(errs, validateAgentTypes(def)...)
    errs = append(errs, validateCredentialNames(def)...)

    return errs
}

// validateStructure checks step IDs, depends_on refs, and cycles.
func validateStructure(def model.WorkflowDef) []ValidationError {
    var errs []ValidationError

    // Build step ID set, check duplicates and format.
    seen := map[string]bool{}
    for _, step := range def.Steps {
        if step.ID == "" {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "id", Message: "step id must not be empty"})
            continue
        }
        if !stepIDRegexp.MatchString(step.ID) {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "id",
                Message: fmt.Sprintf("step id %q must match ^[a-z][a-z0-9_-]*$", step.ID)})
        }
        if seen[step.ID] {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "id",
                Message: fmt.Sprintf("duplicate step id %q", step.ID)})
        }
        seen[step.ID] = true
    }

    // Check depends_on references.
    for _, step := range def.Steps {
        for _, dep := range step.DependsOn {
            if !seen[dep] {
                errs = append(errs, ValidationError{StepID: step.ID, Field: "depends_on",
                    Message: fmt.Sprintf("unknown step %q in depends_on", dep)})
            }
        }
    }

    // Check step type: exactly one of execution or action.
    for _, step := range def.Steps {
        hasExec := step.Execution != nil
        hasAction := step.Action != nil
        if hasExec && hasAction {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "type",
                Message: "step must have exactly one of execution or action, not both"})
        } else if !hasExec && !hasAction {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "type",
                Message: "step must have exactly one of execution or action"})
        }
    }

    // Detect circular dependencies via Kahn's algorithm.
    inDegree := make(map[string]int, len(def.Steps))
    for _, step := range def.Steps {
        if _, ok := inDegree[step.ID]; !ok {
            inDegree[step.ID] = 0
        }
        for _, dep := range step.DependsOn {
            inDegree[step.ID]++
            _ = dep
        }
    }
    // Rebuild: inDegree is count of unresolved dependencies.
    inDegree = make(map[string]int)
    for _, step := range def.Steps {
        inDegree[step.ID] = len(step.DependsOn)
    }
    queue := []string{}
    for _, step := range def.Steps {
        if inDegree[step.ID] == 0 {
            queue = append(queue, step.ID)
        }
    }
    processed := 0
    // Build adjacency: who depends on X?
    dependents := make(map[string][]string)
    for _, step := range def.Steps {
        for _, dep := range step.DependsOn {
            dependents[dep] = append(dependents[dep], step.ID)
        }
    }
    for len(queue) > 0 {
        n := queue[0]
        queue = queue[1:]
        processed++
        for _, dep := range dependents[n] {
            inDegree[dep]--
            if inDegree[dep] == 0 {
                queue = append(queue, dep)
            }
        }
    }
    if processed < len(def.Steps) {
        errs = append(errs, ValidationError{
            Message: "circular dependency detected in workflow steps",
        })
    }

    return errs
}

// validateParameters checks required params are present and types match.
func validateParameters(def model.WorkflowDef, params map[string]any) []ValidationError {
    var errs []ValidationError
    if params == nil {
        params = map[string]any{}
    }
    for _, p := range def.Parameters {
        val, present := params[p.Name]
        if p.Required && !present {
            errs = append(errs, ValidationError{
                Field:   "parameters." + p.Name,
                Message: fmt.Sprintf("required parameter %q is missing", p.Name),
            })
            continue
        }
        if !present {
            continue
        }
        if typeErr := checkParamType(p.Name, p.Type, val); typeErr != "" {
            errs = append(errs, ValidationError{Field: "parameters." + p.Name, Message: typeErr})
        }
    }
    return errs
}

func checkParamType(name, typ string, val any) string {
    switch typ {
    case "string":
        if _, ok := val.(string); !ok {
            return fmt.Sprintf("parameter %q must be a string", name)
        }
    case "int":
        switch v := val.(type) {
        case int, int64:
            // ok
        case float64:
            if v != float64(int(v)) {
                return fmt.Sprintf("parameter %q must be an integer", name)
            }
        default:
            return fmt.Sprintf("parameter %q must be an integer", name)
        }
    case "bool":
        if _, ok := val.(bool); !ok {
            return fmt.Sprintf("parameter %q must be a boolean", name)
        }
    case "json":
        switch val.(type) {
        case map[string]any, []any:
            // ok
        default:
            return fmt.Sprintf("parameter %q must be a JSON object or array", name)
        }
    }
    return ""
}

// validateActionTypes checks action.type against known types.
func validateActionTypes(def model.WorkflowDef) []ValidationError {
    var errs []ValidationError
    for _, step := range def.Steps {
        if step.Action == nil {
            continue
        }
        if !knownActionTypes[step.Action.Type] {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "action.type",
                Message: fmt.Sprintf("unknown action type %q", step.Action.Type)})
        }
    }
    return errs
}

// validateAgentTypes checks execution.agent against known agents.
func validateAgentTypes(def model.WorkflowDef) []ValidationError {
    var errs []ValidationError
    for _, step := range def.Steps {
        if step.Execution == nil {
            continue
        }
        if !knownAgentTypes[step.Execution.Agent] {
            errs = append(errs, ValidationError{StepID: step.ID, Field: "execution.agent",
                Message: fmt.Sprintf("unknown agent type %q", step.Execution.Agent)})
        }
    }
    return errs
}

// validateCredentialNames checks credential name format and reserved names.
func validateCredentialNames(def model.WorkflowDef) []ValidationError {
    var errs []ValidationError
    checkNames := func(stepID string, names []string) {
        for _, name := range names {
            if !credNameRegexp.MatchString(name) {
                errs = append(errs, ValidationError{StepID: stepID, Field: "credentials",
                    Message: fmt.Sprintf("credential name %q must match ^[A-Z][A-Z0-9_]*$", name)})
            } else if reservedEnvVars[name] {
                errs = append(errs, ValidationError{StepID: stepID, Field: "credentials",
                    Message: fmt.Sprintf("credential name %q is a reserved environment variable", name)})
            }
        }
    }
    for _, step := range def.Steps {
        if step.Execution != nil {
            checkNames(step.ID, step.Execution.Credentials)
        }
        if step.Action != nil {
            checkNames(step.ID, step.Action.Credentials)
        }
    }
    return errs
}
```

- [x] **Step 4: Run tests — expect PASS**

```
go test ./internal/workflow/... -run TestValidateWorkflow -v
```

- [x] **Step 5: Commit**

```bash
git add internal/workflow/validate.go internal/workflow/validate_test.go
git commit -m "feat(H1): add workflow structural validation"
```

---

### Task 2: Template AST walking (param refs + step output refs)

**Files:**
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/validate_test.go`

- [x] **Step 1: Write failing tests for template ref extraction and validation**

Add to `validate_test.go`:
```go
func TestValidateWorkflow_UnknownParamRef(t *testing.T) {
    def := model.WorkflowDef{
        Parameters: []model.ParameterDef{{Name: "repo_url", Type: "string"}},
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "fix {{ .Params.typo }}"}},
        },
    }
    errs := ValidateWorkflow(def, map[string]any{"repo_url": "https://example.com"})
    assert.NotEmpty(t, errs)
    assert.Contains(t, errs[0].Message, "typo")
}

func TestValidateWorkflow_ValidParamRef(t *testing.T) {
    def := model.WorkflowDef{
        Parameters: []model.ParameterDef{{Name: "repo_url", Type: "string"}},
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "fix {{ .Params.repo_url }}"}},
        },
    }
    errs := ValidateWorkflow(def, map[string]any{"repo_url": "https://example.com"})
    assert.Empty(t, errs)
}

func TestValidateWorkflow_UnknownStepRef(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "hi"}},
            {ID: "b", DependsOn: []string{"a"}, Execution: &model.ExecutionDef{
                Prompt: "{{ .Steps.missing.Output.result }}",
            }},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.NotEmpty(t, errs)
    assert.Contains(t, errs[0].Message, "missing")
}

func TestValidateWorkflow_StepRefNotUpstream(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "hi"}},
            {ID: "b", Execution: &model.ExecutionDef{
                // b does not depend on a, but references a's output
                Prompt: "{{ .Steps.a.Output.result }}",
            }},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.NotEmpty(t, errs)
    assert.Contains(t, errs[0].Message, "not an upstream")
}

func TestValidateWorkflow_ValidStepRef(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "hi"}},
            {ID: "b", DependsOn: []string{"a"}, Execution: &model.ExecutionDef{
                Prompt: "{{ .Steps.a.Output.result }}",
            }},
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Empty(t, errs)
}

func TestValidateWorkflow_SchemaFieldRef(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {
                ID: "a",
                Execution: &model.ExecutionDef{
                    Prompt: "hi",
                    Output: &model.OutputSchemaDef{
                        Schema: map[string]any{"result": "string"},
                    },
                },
            },
            {
                ID:        "b",
                DependsOn: []string{"a"},
                Execution: &model.ExecutionDef{
                    Prompt: "{{ .Steps.a.Output.result }} and {{ .Steps.a.Output.missing_field }}",
                },
            },
        },
    }
    errs := ValidateWorkflow(def, nil)
    assert.Len(t, errs, 1)
    assert.Contains(t, errs[0].Message, "missing_field")
}

func TestValidateWorkflow_ConditionRef(t *testing.T) {
    def := model.WorkflowDef{
        Steps: []model.StepDef{
            {ID: "a", Execution: &model.ExecutionDef{Prompt: "hi"}},
            {
                ID:        "b",
                DependsOn: []string{"a"},
                Condition: `{{ eq (index .steps "a").status "complete" }}`,
                Execution: &model.ExecutionDef{Prompt: "run"},
            },
        },
    }
    // Valid condition referencing existing step
    errs := ValidateWorkflow(def, nil)
    assert.Empty(t, errs)
}

func TestExtractTemplateRefs_ParamAndStepRefs(t *testing.T) {
    paramRefs, stepRefs, err := extractTemplateRefs(`{{ .Params.repo_url }} {{ .Steps.clone.Output.diff }}`, false)
    assert.NoError(t, err)
    assert.Equal(t, []string{"repo_url"}, paramRefs)
    require.Len(t, stepRefs, 1)
    assert.Equal(t, "clone", stepRefs[0].StepID)
    assert.Equal(t, "Output", stepRefs[0].Field)
    assert.Equal(t, "diff", stepRefs[0].OutputKey)
}

func TestExtractTemplateRefs_ConditionContext(t *testing.T) {
    paramRefs, stepRefs, err := extractTemplateRefs(`{{ .steps.clone.status }}`, true)
    assert.NoError(t, err)
    assert.Empty(t, paramRefs)
    require.Len(t, stepRefs, 1)
    assert.Equal(t, "clone", stepRefs[0].StepID)
    assert.Equal(t, "status", stepRefs[0].Field)
}
```

- [x] **Step 2: Run tests — expect FAIL**

```
go test ./internal/workflow/... -run "TestValidateWorkflow_Unknown|TestValidateWorkflow_Valid|TestValidateWorkflow_Step|TestValidateWorkflow_Schema|TestValidateWorkflow_Condition|TestExtractTemplateRefs" -v
```

- [x] **Step 3: Add StepRef type and extractTemplateRefs + validateTemplateRefs to validate.go**

Add to `validate.go`:
```go
import (
    // existing imports +
    "text/template/parse"
    "sort"
    "strings"
)

// StepRef represents a .Steps.X.Field or .Steps.X.Output.Y reference.
type StepRef struct {
    StepID    string
    Field     string // "Output", "Status", "Diff", "Error", or lowercase condition variants
    OutputKey string // non-empty only for .Steps.X.Output.Y
}

// extractTemplateRefs parses a Go template string and extracts .Params.X and .Steps.X.* references.
// If conditionCtx is true, looks for lowercase .params.* and .steps.*.* (evalCondition context).
func extractTemplateRefs(templateStr string, conditionCtx bool) (paramRefs []string, stepRefs []StepRef, err error) {
    if templateStr == "" {
        return nil, nil, nil
    }
    trees, err := parse.Parse("t", templateStr, "{{", "}}", nil)
    if err != nil {
        // Template parse errors are not our concern here — runtime will catch them.
        return nil, nil, nil
    }
    tree, ok := trees["t"]
    if !ok || tree == nil || tree.Root == nil {
        return nil, nil, nil
    }

    paramKey := "Params"
    stepsKey := "Steps"
    if conditionCtx {
        paramKey = "params"
        stepsKey = "steps"
    }

    seen := map[string]bool{}
    seenStep := map[string]bool{}

    var walk func(node parse.Node)
    walk = func(node parse.Node) {
        if node == nil {
            return
        }
        switch n := node.(type) {
        case *parse.ListNode:
            if n == nil {
                return
            }
            for _, child := range n.Nodes {
                walk(child)
            }
        case *parse.ActionNode:
            walk(n.Pipe)
        case *parse.IfNode:
            walk(n.Pipe)
            walk(n.List)
            walk(n.ElseList)
        case *parse.RangeNode:
            walk(n.Pipe)
            walk(n.List)
            walk(n.ElseList)
        case *parse.WithNode:
            walk(n.Pipe)
            walk(n.List)
            walk(n.ElseList)
        case *parse.PipeNode:
            for _, cmd := range n.Cmds {
                walk(cmd)
            }
        case *parse.CommandNode:
            for _, arg := range n.Args {
                walk(arg)
            }
        case *parse.FieldNode:
            idents := n.Ident
            if len(idents) >= 2 && idents[0] == paramKey {
                ref := idents[1]
                if !seen[ref] {
                    seen[ref] = true
                    paramRefs = append(paramRefs, ref)
                }
            }
            if len(idents) >= 3 && idents[0] == stepsKey {
                stepID := idents[1]
                field := idents[2]
                var outputKey string
                if len(idents) >= 4 && (field == "Output" || field == "output") {
                    outputKey = idents[3]
                }
                key := stepID + "." + field + "." + outputKey
                if !seenStep[key] {
                    seenStep[key] = true
                    stepRefs = append(stepRefs, StepRef{
                        StepID:    stepID,
                        Field:     field,
                        OutputKey: outputKey,
                    })
                }
            }
        }
    }
    walk(tree.Root)

    sort.Strings(paramRefs)
    return paramRefs, stepRefs, nil
}

// validateTemplateRefs validates .Params.X and .Steps.X.* references in all templates.
func validateTemplateRefs(def model.WorkflowDef) []ValidationError {
    var errs []ValidationError

    // Build param name set.
    paramNames := map[string]bool{}
    for _, p := range def.Parameters {
        paramNames[p.Name] = true
    }

    // Build step set and their declared schemas.
    stepExists := map[string]bool{}
    stepSchema := map[string]map[string]any{}
    for _, step := range def.Steps {
        stepExists[step.ID] = true
        if step.Execution != nil && step.Execution.Output != nil {
            stepSchema[step.ID] = step.Execution.Output.Schema
        }
    }

    // Build transitive dependency map: for each step, which steps are upstream?
    // BFS/DFS from depends_on.
    upstreamOf := func(stepID string) map[string]bool {
        upstream := map[string]bool{}
        queue := []string{stepID}
        for len(queue) > 0 {
            cur := queue[0]
            queue = queue[1:]
            for _, s := range def.Steps {
                if s.ID == cur {
                    for _, dep := range s.DependsOn {
                        if !upstream[dep] {
                            upstream[dep] = true
                            queue = append(queue, dep)
                        }
                    }
                }
            }
        }
        return upstream
    }

    // Collect all template strings per step.
    for _, step := range def.Steps {
        upstream := upstreamOf(step.ID)

        var templates []string
        if step.Execution != nil {
            templates = append(templates, step.Execution.Prompt)
        }
        if step.Action != nil {
            for _, v := range step.Action.Config {
                if s, ok := v.(string); ok {
                    templates = append(templates, s)
                }
            }
        }

        // Validate non-condition templates.
        for _, tmpl := range templates {
            paramRefs, stepRefs, _ := extractTemplateRefs(tmpl, false)
            for _, ref := range paramRefs {
                if !paramNames[ref] {
                    errs = append(errs, ValidationError{StepID: step.ID, Field: "template",
                        Message: fmt.Sprintf("template references undefined parameter %q", ref)})
                }
            }
            for _, sr := range stepRefs {
                if !stepExists[sr.StepID] {
                    errs = append(errs, ValidationError{StepID: step.ID, Field: "template",
                        Message: fmt.Sprintf("template references undefined step %q", sr.StepID)})
                    continue
                }
                if !upstream[sr.StepID] {
                    errs = append(errs, ValidationError{StepID: step.ID, Field: "template",
                        Message: fmt.Sprintf("template references step %q which is not an upstream dependency", sr.StepID)})
                    continue
                }
                // If the referenced step has a declared schema, check the field exists.
                if sr.OutputKey != "" {
                    if schema, hasSch := stepSchema[sr.StepID]; hasSch {
                        if _, fieldExists := schema[sr.OutputKey]; !fieldExists {
                            errs = append(errs, ValidationError{StepID: step.ID, Field: "template",
                                Message: fmt.Sprintf("template references field %q which is not declared in step %q output schema", sr.OutputKey, sr.StepID)})
                        }
                    }
                }
            }
        }

        // Validate condition template (lowercase context).
        if step.Condition != "" {
            _, stepRefs, _ := extractTemplateRefs(step.Condition, true)
            for _, sr := range stepRefs {
                if !stepExists[sr.StepID] {
                    errs = append(errs, ValidationError{StepID: step.ID, Field: "condition",
                        Message: fmt.Sprintf("condition references undefined step %q", sr.StepID)})
                }
            }
        }
    }

    return errs
}
```

Also update `ValidateWorkflow` to include `validateTemplateRefs`:
```go
func ValidateWorkflow(def model.WorkflowDef, params map[string]any) []ValidationError {
    var errs []ValidationError
    errs = append(errs, validateStructure(def)...)
    errs = append(errs, validateParameters(def, params)...)
    errs = append(errs, validateTemplateRefs(def)...)
    errs = append(errs, validateActionTypes(def)...)
    errs = append(errs, validateAgentTypes(def)...)
    errs = append(errs, validateCredentialNames(def)...)
    return errs
}
```

Add missing imports to validate.go: `"sort"`, `"strings"`, `"text/template/parse"`.

- [x] **Step 4: Run tests — expect PASS**

```
go test ./internal/workflow/... -run "TestValidateWorkflow|TestExtractTemplateRefs" -v
```

- [x] **Step 5: Commit**

```bash
git add internal/workflow/validate.go internal/workflow/validate_test.go
git commit -m "feat(H1): add template AST ref validation"
```

---

### Task 3: HTTP integration — 400 on validation failure

**Files:**
- Modify: `internal/server/handlers/runs.go`
- Modify: `internal/server/handlers/runs_test.go`

- [x] **Step 1: Write failing test for 400 response**

Add to `runs_test.go`:
```go
func TestRunsCreate_ValidationError(t *testing.T) {
    // Uses existing test helpers in the file.
    // Create a workflow template with an invalid definition (circular deps).
    // POST to /runs, expect 400 with validation_errors key.
    // This test uses the in-process test server setup already in runs_test.go.
    // Look at the existing test setup pattern in that file to wire correctly.
}
```

Read `runs_test.go` first to understand the existing test server setup before writing this test.

- [x] **Step 2: Read existing runs_test.go to understand test harness**

Read: `internal/server/handlers/runs_test.go` — identify how the test server is constructed and how to inject a mock/real registry that returns a template with invalid YAML.

- [x] **Step 3: Add validation call to runs.go**

In `RunsHandler.Create`, after parsing YAML and before DB insert:

```go
// After: var def model.WorkflowDef / ParseWorkflowYAML
if errs := workflow.ValidateWorkflow(def, req.Parameters); len(errs) > 0 {
    writeJSON(w, http.StatusBadRequest, map[string]any{
        "error":             "workflow validation failed",
        "validation_errors": errs,
    })
    return
}
```

Add import: `"github.com/tinkerloft/fleetlift/internal/workflow"` (if not already present — check existing imports).

- [x] **Step 4: Run tests — all pass**

```
go test ./internal/server/handlers/... ./internal/workflow/... -v
```

- [x] **Step 5: Commit**

```bash
git add internal/server/handlers/runs.go internal/server/handlers/runs_test.go
git commit -m "feat(H1): wire ValidateWorkflow into RunsHandler.Create"
```

---

## Chunk 2: H2 — Action Step Credential Access + Logging

### Task 4: Model change — add Credentials to ActionDef

**Files:**
- Modify: `internal/model/workflow.go`

- [x] **Step 1: Add Credentials field to ActionDef**

In `internal/model/workflow.go`, change:
```go
type ActionDef struct {
    Type   string         `yaml:"type"`
    Config map[string]any `yaml:"config"`
}
```
to:
```go
type ActionDef struct {
    Type        string         `yaml:"type"`
    Config      map[string]any `yaml:"config"`
    Credentials []string       `yaml:"credentials,omitempty"`
}
```

- [x] **Step 2: Build verification**

```
go build ./...
```

- [x] **Step 3: Run full test suite — no regressions**

```
go test ./...
```

- [x] **Step 4: Commit**

```bash
git add internal/model/workflow.go
git commit -m "feat(H2): add Credentials field to ActionDef"
```

---

### Task 5: Update ExecuteAction signature, handlers, logging, return values

**Files:**
- Modify: `internal/activity/actions.go`

- [x] **Step 1: Write failing tests**

Create `internal/activity/actions_test.go`:
```go
package activity

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

type mockCredStore struct {
    data map[string]string
}

func (m *mockCredStore) Get(_ context.Context, _, name string) (string, error) {
    if v, ok := m.data[name]; ok {
        return v, nil
    }
    return "", fmt.Errorf("credential %q not found", name)
}

func (m *mockCredStore) GetBatch(_ context.Context, _ string, names []string) (map[string]string, error) {
    out := map[string]string{}
    for _, n := range names {
        if v, ok := m.data[n]; ok {
            out[n] = v
        }
    }
    return out, nil
}

func TestExecuteAction_UnknownType(t *testing.T) {
    a := &Activities{CredStore: &mockCredStore{}}
    _, err := a.ExecuteAction(context.Background(), "step-run-1", "bad_type", nil, "team-1", nil)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "unknown action type")
}

func TestExecuteAction_SignatureAcceptsNewArgs(t *testing.T) {
    // Verify the new signature compiles with stepRunID, teamID, credNames.
    a := &Activities{CredStore: &mockCredStore{data: map[string]string{}}}
    _, err := a.ExecuteAction(context.Background(), "step-run-1", "slack_notify",
        map[string]any{"channel": "", "message": ""},
        "team-1", []string{})
    // Returns nil (empty channel/message causes early return with no error).
    assert.NoError(t, err)
}
```

- [x] **Step 2: Run test — expect FAIL (wrong ExecuteAction signature)**

```
go test ./internal/activity/... -run TestExecuteAction -v
```

- [x] **Step 3: Update ExecuteAction signature and all handlers in actions.go**

Replace the entire `actions.go` content with the updated version:

```go
package activity

import (
    "context"
    "fmt"
    "os"
    "strings"

    "github.com/google/go-github/v62/github"
    "go.temporal.io/sdk/activity"
    "golang.org/x/oauth2"
)

// ExecuteAction dispatches an action step to the appropriate handler based on action type.
// stepRunID is used for logging. credNames are fetched from CredStore using teamID.
func (a *Activities) ExecuteAction(ctx context.Context, stepRunID string, actionType string, config map[string]any, teamID string, credNames []string) (map[string]any, error) {
    // Fetch credentials from the store.
    credentials := map[string]string{}
    if len(credNames) > 0 && a.CredStore != nil {
        var err error
        credentials, err = a.CredStore.GetBatch(ctx, teamID, credNames)
        if err != nil {
            return nil, fmt.Errorf("fetch credentials: %w", err)
        }
    }

    // Log action dispatch (redact credential values).
    logAction(ctx, a, stepRunID, fmt.Sprintf("Executing action: %s", actionType))

    var result map[string]any
    var err error
    switch actionType {
    case "slack_notify":
        result, err = a.actionNotifySlack(ctx, config, credentials)
    case "github_pr_review":
        result, err = a.actionGitHubPostReviewComment(ctx, config, credentials)
    case "github_assign":
        result, err = a.actionGitHubAssignIssue(ctx, config, credentials)
    case "github_label":
        result, err = a.actionGitHubAddLabel(ctx, config, credentials)
    case "github_comment":
        result, err = a.actionGitHubPostIssueComment(ctx, config, credentials)
    case "create_pr":
        return map[string]any{"status": "skipped_in_action"}, nil
    default:
        return nil, fmt.Errorf("unknown action type: %s", actionType)
    }

    if err != nil {
        logAction(ctx, a, stepRunID, fmt.Sprintf("Action failed: %v", err))
        return nil, err
    }
    logAction(ctx, a, stepRunID, "Action completed successfully")
    return result, nil
}

// logAction writes a single log line for an action step. Silently ignored if DB is nil.
func logAction(ctx context.Context, a *Activities, stepRunID, msg string) {
    if a.DB == nil || stepRunID == "" {
        return
    }
    _ = batchInsertLogs(ctx, a, stepRunID, []logLine{{Seq: 0, Stream: "stdout", Content: msg}})
}

// credOrEnv returns the credential value from the credentials map, falling back to os.Getenv.
func credOrEnv(credentials map[string]string, name string) string {
    if v, ok := credentials[name]; ok && v != "" {
        return v
    }
    return os.Getenv(name)
}

func (a *Activities) actionNotifySlack(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
    channel, _ := config["channel"].(string)
    message, _ := config["message"].(string)
    if channel == "" || message == "" {
        activity.GetLogger(ctx).Warn("slack_notify: missing channel or message")
        return map[string]any{"status": "skipped", "reason": "missing channel or message"}, nil
    }

    slackActs := NewSlackActivities()
    _, err := slackActs.NotifySlack(ctx, channel, message, nil)
    if err != nil {
        return nil, err
    }
    return map[string]any{"status": "sent", "channel": channel}, nil
}

func (a *Activities) actionGitHubPostReviewComment(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
    repoURL, _ := config["repo_url"].(string)
    prNumber := toInt(config["pr_number"])
    summary, _ := config["summary"].(string)

    if repoURL == "" || prNumber == 0 {
        return nil, fmt.Errorf("github_pr_review: missing repo_url or pr_number")
    }

    token := credOrEnv(credentials, "GITHUB_TOKEN")
    if token == "" {
        return nil, fmt.Errorf("github_pr_review: GITHUB_TOKEN not available")
    }
    ghClient := newGitHubClientWithToken(ctx, token)

    if summary == "" {
        activity.GetLogger(ctx).Warn("github_pr_review: empty summary, skipping")
        return map[string]any{"status": "skipped", "reason": "empty summary"}, nil
    }

    owner, repo := extractOwnerRepo(repoURL)
    review, _, err := ghClient.PullRequests.CreateReview(ctx, owner, repo, prNumber, &github.PullRequestReviewRequest{
        Body:  github.String(summary),
        Event: github.String("COMMENT"),
    })
    if err != nil {
        return nil, err
    }
    reviewID := int64(0)
    if review != nil && review.ID != nil {
        reviewID = *review.ID
    }
    return map[string]any{"status": "posted", "review_id": reviewID}, nil
}

func (a *Activities) actionGitHubAssignIssue(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
    repoURL, _ := config["repo_url"].(string)
    issueNumber := toInt(config["issue_number"])

    if repoURL == "" || issueNumber == 0 {
        return nil, fmt.Errorf("github_assign: missing repo_url or issue_number")
    }

    activity.GetLogger(ctx).Info("github_assign: auto-assignment not yet configured",
        "repo", repoURL, "issue", issueNumber)
    return map[string]any{"status": "skipped", "reason": "not configured"}, nil
}

func (a *Activities) actionGitHubAddLabel(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
    repoURL, _ := config["repo_url"].(string)
    issueNumber := toInt(config["issue_number"])
    labels := toStringSlice(config["labels"])

    if repoURL == "" || issueNumber == 0 || len(labels) == 0 {
        return nil, fmt.Errorf("github_label: missing repo_url, issue_number, or labels")
    }

    token := credOrEnv(credentials, "GITHUB_TOKEN")
    if token == "" {
        return nil, fmt.Errorf("GITHUB_TOKEN not available")
    }
    ghClient := newGitHubClientWithToken(ctx, token)

    owner, repo := extractOwnerRepo(repoURL)
    _, _, err := ghClient.Issues.AddLabelsToIssue(ctx, owner, repo, issueNumber, labels)
    if err != nil {
        return nil, err
    }
    return map[string]any{"status": "labeled", "labels": labels}, nil
}

func (a *Activities) actionGitHubPostIssueComment(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
    repoURL, _ := config["repo_url"].(string)
    issueNumber := toInt(config["issue_number"])
    body, _ := config["body"].(string)

    if repoURL == "" || issueNumber == 0 || body == "" {
        return nil, fmt.Errorf("github_comment: missing repo_url, issue_number, or body")
    }

    token := credOrEnv(credentials, "GITHUB_TOKEN")
    if token == "" {
        return nil, fmt.Errorf("GITHUB_TOKEN not available")
    }
    owner, repoName := extractOwnerRepo(repoURL)
    ghClient := newGitHubClientWithToken(ctx, token)
    comment, _, err := ghClient.Issues.CreateComment(ctx, owner, repoName, issueNumber,
        &github.IssueRequest{Body: github.String(body)})
    if err != nil {
        return nil, err
    }
    commentID := int64(0)
    if comment != nil && comment.ID != nil {
        commentID = *comment.ID
    }
    return map[string]any{"status": "posted", "comment_id": commentID}, nil
}

// newGitHubClientWithToken creates a GitHub client with the given token.
func newGitHubClientWithToken(ctx context.Context, token string) *github.Client {
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    tc := oauth2.NewClient(ctx, ts)
    return github.NewClient(tc)
}

// newGitHubClient creates a GitHub client from GITHUB_TOKEN env var.
// Kept for backward compat with any callers outside this file.
func newGitHubClient(ctx context.Context) *github.Client {
    token := os.Getenv("GITHUB_TOKEN")
    if token == "" {
        return nil
    }
    return newGitHubClientWithToken(ctx, token)
}

func toInt(v any) int {
    switch val := v.(type) {
    case int:
        return val
    case float64:
        return int(val)
    case int64:
        return int(val)
    case string:
        var n int
        _, _ = fmt.Sscanf(val, "%d", &n)
        return n
    }
    return 0
}

func toStringSlice(v any) []string {
    switch val := v.(type) {
    case []string:
        return val
    case []any:
        var out []string
        for _, item := range val {
            if s, ok := item.(string); ok {
                out = append(out, s)
            }
        }
        return out
    case string:
        if val != "" {
            return strings.Split(val, ",")
        }
    }
    return nil
}
```

Note: The `actionGitHubPostIssueComment` previously delegated to `ghActs.PostIssueComment`. Check `github.go` to see if that creates the comment with a `*github.IssueComment` or `*github.IssueRequest` — use the correct struct. Read `github.go` first.

- [x] **Step 4: Read github.go to verify IssueComment struct used**

Read `internal/activity/github.go` — check the `PostIssueComment` implementation for correct API usage.

- [x] **Step 5: Fix any struct mismatch from step 3**

If `github.go` uses `github.IssueComment{Body: ...}` for `CreateComment`, update `actionGitHubPostIssueComment` accordingly. The GitHub API's `CreateComment` takes `*github.IssueComment`, not `*github.IssueRequest`.

Correct form:
```go
comment, _, err := ghClient.Issues.CreateComment(ctx, owner, repoName, issueNumber,
    &github.IssueComment{Body: github.String(body)})
```

- [x] **Step 6: Run tests — expect PASS**

```
go test ./internal/activity/... -run TestExecuteAction -v
```

- [x] **Step 7: Build verification**

```
go build ./...
```

- [x] **Step 8: Commit**

```bash
git add internal/activity/actions.go internal/activity/actions_test.go
git commit -m "feat(H2): update ExecuteAction with credential access, logging, meaningful results"
```

---

### Task 6: Update dag.go — pass teamID, stepRunID, credNames to executeAction

**Files:**
- Modify: `internal/workflow/dag.go`

- [x] **Step 1: Update executeAction signature and workflow.ExecuteActivity call**

In `dag.go`, change `executeAction`:
```go
// Old:
func executeAction(ctx workflow.Context, step model.StepDef, _ ResolvedStepOpts) *model.StepOutput {
    ao := workflow.ActivityOptions{
        StartToCloseTimeout: 5 * time.Minute,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
    }
    actCtx := workflow.WithActivityOptions(ctx, ao)
    var result map[string]any
    err := workflow.ExecuteActivity(actCtx, "ExecuteAction", step.Action.Type, step.Action.Config).Get(actCtx, &result)
    ...
}

// New:
func executeAction(ctx workflow.Context, step model.StepDef, teamID, stepRunID string, credNames []string) *model.StepOutput {
    ao := workflow.ActivityOptions{
        StartToCloseTimeout: 5 * time.Minute,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
    }
    actCtx := workflow.WithActivityOptions(ctx, ao)
    var result map[string]any
    err := workflow.ExecuteActivity(actCtx, "ExecuteAction", stepRunID, step.Action.Type, step.Action.Config, teamID, credNames).Get(actCtx, &result)
    ...
}
```

- [x] **Step 2: Update the call site in the action step path (~line 243)**

Find:
```go
results[i] = executeAction(gCtx, step, resolved)
```

Change to:
```go
results[i] = executeAction(gCtx, step, input.TeamID, stepRunID, step.Action.Credentials)
```

Note: `stepRunID` is available in the same scope from the `CreateStepRunActivity` call. Confirm this is true by reading the surrounding code before making the change.

- [x] **Step 3: Build verification**

```
go build ./...
```

- [x] **Step 4: Run full test suite**

```
go test ./...
```

- [x] **Step 5: Commit**

```bash
git add internal/workflow/dag.go
git commit -m "feat(H2): pass teamID, stepRunID, credNames through executeAction"
```

---

## Chunk 3: H3 — Output Schema Enforcement

### Task 7: Schema instructions, extraction, and validation

**Files:**
- Modify: `internal/activity/execute.go`

- [x] **Step 1: Write failing tests**

Add to `internal/activity/execute_test.go`:
```go
func TestAppendOutputSchemaInstructions(t *testing.T) {
    schema := map[string]any{
        "root_cause":     "string",
        "affected_files": "array",
        "severity":       "string",
    }
    result := appendOutputSchemaInstructions("Fix the bug.", schema)
    assert.Contains(t, result, "Fix the bug.")
    assert.Contains(t, result, "```json")
    assert.Contains(t, result, "root_cause")
    assert.Contains(t, result, "affected_files")
    assert.Contains(t, result, "severity")
    assert.Contains(t, result, "IMPORTANT")
}

func TestExtractSchemaFields_FromFencedBlock(t *testing.T) {
    resultText := `I analyzed the code.

` + "```" + `json
{"root_cause": "nil pointer", "affected_files": ["main.go"], "severity": "high"}
` + "```"

    schema := map[string]any{
        "root_cause":     "string",
        "affected_files": "array",
        "severity":       "string",
    }
    out, err := extractSchemaFields(resultText, schema)
    require.NoError(t, err)
    assert.Equal(t, "nil pointer", out["root_cause"])
    assert.Equal(t, "high", out["severity"])
}

func TestExtractSchemaFields_FromBareJSON(t *testing.T) {
    resultText := `Analysis: {"root_cause": "memory leak", "severity": "low"}`
    schema := map[string]any{"root_cause": "string", "severity": "string"}
    out, err := extractSchemaFields(resultText, schema)
    require.NoError(t, err)
    assert.Equal(t, "memory leak", out["root_cause"])
}

func TestExtractSchemaFields_NoJSON(t *testing.T) {
    _, err := extractSchemaFields("no json here", map[string]any{"root_cause": "string"})
    assert.Error(t, err)
}

func TestValidateOutputSchema_AllPresent(t *testing.T) {
    output := map[string]any{
        "root_cause": "nil pointer",
        "severity":   "high",
    }
    schema := map[string]any{"root_cause": "string", "severity": "string"}
    violations := validateOutputSchema(output, schema)
    assert.Empty(t, violations)
}

func TestValidateOutputSchema_MissingField(t *testing.T) {
    output := map[string]any{"root_cause": "nil pointer"}
    schema := map[string]any{"root_cause": "string", "severity": "string"}
    violations := validateOutputSchema(output, schema)
    assert.Len(t, violations, 1)
    assert.Contains(t, violations[0], "severity")
}

func TestValidateOutputSchema_WrongType(t *testing.T) {
    output := map[string]any{"root_cause": 42, "severity": "high"}
    schema := map[string]any{"root_cause": "string", "severity": "string"}
    violations := validateOutputSchema(output, schema)
    assert.Len(t, violations, 1)
    assert.Contains(t, violations[0], "root_cause")
}
```

- [x] **Step 2: Run tests — expect FAIL**

```
go test ./internal/activity/... -run "TestAppendOutputSchema|TestExtractSchemaFields|TestValidateOutputSchema" -v
```

- [x] **Step 3: Add three functions to execute.go**

Add after `extractStructuredOutput` in `execute.go`:

```go
import (
    // existing imports +
    "encoding/json"
    "regexp"
    "sort"
    "strings"
)

// appendOutputSchemaInstructions appends a structured output instruction to the prompt.
// schema is {field: type} where type is "string", "array", "boolean", "number", or "object".
func appendOutputSchemaInstructions(prompt string, schema map[string]any) string {
    if len(schema) == 0 {
        return prompt
    }

    // Build sorted keys for deterministic output.
    keys := make([]string, 0, len(schema))
    for k := range schema {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    placeholders := make(map[string]string, len(schema))
    for _, k := range keys {
        typ, _ := schema[k].(string)
        switch typ {
        case "array":
            placeholders[k] = `["<` + typ + `>"]`
        case "boolean":
            placeholders[k] = "<boolean>"
        case "number":
            placeholders[k] = "<number>"
        case "object":
            placeholders[k] = "{}"
        default:
            placeholders[k] = "<string>"
        }
    }

    // Build example JSON.
    parts := make([]string, 0, len(keys))
    for _, k := range keys {
        valJSON, _ := json.Marshal(placeholders[k])
        parts = append(parts, fmt.Sprintf("%q: %s", k, string(valJSON)))
    }
    exampleJSON := "{" + strings.Join(parts, ", ") + "}"

    instruction := fmt.Sprintf(
        "\n\nIMPORTANT: At the end of your response, you MUST output a JSON object with exactly these fields,\nwrapped in a ```json fenced code block:\n\n%s\n\nThis structured output is required for downstream workflow steps.",
        exampleJSON,
    )
    return prompt + instruction
}

var jsonFenceRegexp = regexp.MustCompile("(?s)```json\\s*\\n([^`]+?)\\n?```")
var bareJSONRegexp = regexp.MustCompile(`(?s)\{[^{}]*\}`)

// extractSchemaFields extracts declared schema fields from an agent's result text.
// Tries: (1) fenced ```json block, (2) last bare {...} JSON object.
func extractSchemaFields(resultText string, schema map[string]any) (map[string]any, error) {
    if resultText == "" {
        return nil, fmt.Errorf("empty result text")
    }

    // Try fenced code blocks — find the last one.
    matches := jsonFenceRegexp.FindAllStringSubmatch(resultText, -1)
    if len(matches) > 0 {
        last := matches[len(matches)-1][1]
        var out map[string]any
        if err := json.Unmarshal([]byte(strings.TrimSpace(last)), &out); err == nil {
            return filterSchema(out, schema), nil
        }
    }

    // Try last bare JSON object.
    bareMatches := bareJSONRegexp.FindAllString(resultText, -1)
    if len(bareMatches) > 0 {
        last := bareMatches[len(bareMatches)-1]
        var out map[string]any
        if err := json.Unmarshal([]byte(last), &out); err == nil {
            return filterSchema(out, schema), nil
        }
    }

    return nil, fmt.Errorf("no JSON object found in agent output")
}

// filterSchema returns only the keys declared in schema.
func filterSchema(out map[string]any, schema map[string]any) map[string]any {
    result := make(map[string]any, len(schema))
    for k := range schema {
        if v, ok := out[k]; ok {
            result[k] = v
        }
    }
    return result
}

// validateOutputSchema checks that all declared schema fields are present with correct types.
// Returns a list of violation messages.
func validateOutputSchema(output map[string]any, schema map[string]any) []string {
    var violations []string
    for field, typVal := range schema {
        val, ok := output[field]
        if !ok {
            violations = append(violations, fmt.Sprintf("missing required field %q", field))
            continue
        }
        typ, _ := typVal.(string)
        if typeViolation := checkOutputFieldType(field, typ, val); typeViolation != "" {
            violations = append(violations, typeViolation)
        }
    }
    sort.Strings(violations)
    return violations
}

func checkOutputFieldType(field, typ string, val any) string {
    switch typ {
    case "string":
        if _, ok := val.(string); !ok {
            return fmt.Sprintf("field %q must be a string, got %T", field, val)
        }
    case "array":
        if _, ok := val.([]any); !ok {
            return fmt.Sprintf("field %q must be an array, got %T", field, val)
        }
    case "boolean":
        if _, ok := val.(bool); !ok {
            return fmt.Sprintf("field %q must be a boolean, got %T", field, val)
        }
    case "number":
        switch val.(type) {
        case float64, int, int64:
            // ok
        default:
            return fmt.Sprintf("field %q must be a number, got %T", field, val)
        }
    }
    return ""
}
```

- [x] **Step 4: Run tests — expect PASS**

```
go test ./internal/activity/... -run "TestAppendOutputSchema|TestExtractSchemaFields|TestValidateOutputSchema" -v
```

- [x] **Step 5: Wire into ExecuteStep — prompt injection + schema enforcement**

In `ExecuteStep` in `execute.go`:

**Prompt injection** — after building `prompt` (line ~136), before `runner.Run`:
```go
// Append schema instructions if the step declares an output schema.
if stepInput.StepDef.Execution != nil && stepInput.StepDef.Execution.Output != nil {
    prompt = appendOutputSchemaInstructions(prompt, stepInput.StepDef.Execution.Output.Schema)
}
```

**Schema enforcement** — after `structured := extractStructuredOutput(lastOutput)` (line ~228):
```go
if stepInput.StepDef.Execution != nil && stepInput.StepDef.Execution.Output != nil {
    schema := stepInput.StepDef.Execution.Output.Schema
    resultText, _ := lastOutput["result"].(string)

    extracted, extractErr := extractSchemaFields(resultText, schema)
    if extractErr == nil {
        structured = extracted
    }

    violations := validateOutputSchema(structured, schema)
    if len(violations) > 0 {
        return &model.StepOutput{
            StepID: stepInput.StepDef.ID,
            Status: model.StepStatusFailed,
            Output: structured,
            Error:  fmt.Sprintf("output schema validation failed: %s", strings.Join(violations, "; ")),
        }, nil
    }
}
```

- [x] **Step 6: Run full test suite**

```
go test ./...
```

- [x] **Step 7: Lint**

```
make lint
```

- [x] **Step 8: Build**

```
go build ./...
```

- [x] **Step 9: Commit**

```bash
git add internal/activity/execute.go internal/activity/execute_test.go
git commit -m "feat(H3): enforce output schema — prompt injection, extraction, validation"
```

---

## Final Verification

- [x] Run `go test ./...` — all tests pass
- [x] Run `make lint` — no errors
- [x] Run `go build ./...` — clean build
- [x] Update implementation plan: mark H1, H2, H3 as complete in `docs/plans/2026-03-14-reliability-audit.md`

---

## Notes

- `actionGitHubPostIssueComment` previously delegated to `ghActs.PostIssueComment(ctx, repoURL, issueNumber, body)` — check `github.go` to see if there's a simpler internal helper to reuse vs calling the API directly.
- The `logAction` helper accepts a `seq int64` parameter. `ExecuteAction` maintains a local counter (start=0, end=1), avoiding collisions in `step_run_logs`.
- H1's condition template validation checks that referenced step IDs exist, but doesn't check that the step is an upstream dependency (conditions can reference any completed step). This is intentional.
- The `extractSchemaFields` bare JSON regex is simple and may match partial JSON. The fenced block path is preferred and more reliable.
