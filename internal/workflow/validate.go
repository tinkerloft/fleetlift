package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template/parse"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// ValidationError describes a single structural or semantic problem found in a WorkflowDef.
type ValidationError struct {
	StepID  string `json:"step_id,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.StepID != "" {
		return fmt.Sprintf("step %q field %q: %s", e.StepID, e.Field, e.Message)
	}
	return fmt.Sprintf("field %q: %s", e.Field, e.Message)
}

var stepIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// defaultRegistry is the package-level registry used by ValidateWorkflow.
// It is initialized once and must not be mutated after init.
// A separate instance is created for the API handler in cmd/server/main.go;
// both are populated by the same DefaultActionRegistry() builder.
var defaultRegistry = model.DefaultActionRegistry()

var validAgentTypes = map[string]bool{
	"claude-code": true,
	"codex":       true,
	"shell":       true,
	"":            true,
}

var credNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// templateBuiltinFuncs is a stub funcs map passed to parse.Parse so that templates using
// built-in functions like "eq", "index", "len" etc. can be parsed without a function-not-found
// error. The values are never called during validation — they only satisfy the parser.
var templateBuiltinFuncs = map[string]any{
	"and":      func() {},
	"call":     func() {},
	"html":     func() {},
	"index":    func() {},
	"slice":    func() {},
	"js":       func() {},
	"len":      func() {},
	"not":      func() {},
	"or":       func() {},
	"print":    func() {},
	"printf":   func() {},
	"println":  func() {},
	"urlquery": func() {},
	"eq":       func() {},
	"ge":       func() {},
	"gt":       func() {},
	"le":       func() {},
	"lt":       func() {},
	"ne":       func() {},
	// Custom template functions registered by RenderPrompt — must be present so
	// parse.Parse does not reject templates that use them.
	"toJSON":   func() {},
	"truncate": func() {},
	"join":     func() {},
}

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
	errs = append(errs, validateSandboxGroups(def)...)
	errs = append(errs, validateCredentialNames(def)...)
	errs = append(errs, validateTemplateRefs(def)...)
	errs = append(errs, validateJSONParamsInRepositories(def)...)
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

// validateActionTypes checks that action steps use a known action type and valid config.
func validateActionTypes(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError
	for _, step := range def.Steps {
		if step.Action == nil {
			continue
		}
		_, ok := defaultRegistry.Get(step.Action.Type)
		if !ok {
			known := defaultRegistry.Types()
			errs = append(errs, ValidationError{
				StepID:  step.ID,
				Field:   "action.type",
				Message: fmt.Sprintf("unknown action type %q; known: %s", step.Action.Type, strings.Join(known, ", ")),
			})
			continue
		}
		// Validate config keys and types against contract
		for _, violation := range defaultRegistry.ValidateConfig(step.Action.Type, step.Action.Config) {
			errs = append(errs, ValidationError{
				StepID:  step.ID,
				Field:   "action.config",
				Message: violation,
			})
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

// validateSandboxGroups checks that steps inside a sandbox group do not also specify a step-level
// sandbox image. The group image (defined under sandbox_groups) is authoritative for all steps in
// the group; per-step overrides are not allowed and would silently be ignored.
func validateSandboxGroups(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError
	for _, step := range def.Steps {
		if step.SandboxGroup != "" && step.Sandbox != nil && step.Sandbox.Image != "" {
			errs = append(errs, ValidationError{
				StepID:  step.ID,
				Field:   "sandbox.image",
				Message: "steps inside a sandbox_group must not specify sandbox.image; set the image on the sandbox group instead",
			})
		}
	}
	return errs
}

// validateCredentialNames checks that execution and action credential names are well-formed and not reserved.
func validateCredentialNames(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError

	checkNames := func(stepID string, creds []string) {
		for _, cred := range creds {
			if !credNameRe.MatchString(cred) {
				errs = append(errs, ValidationError{StepID: stepID, Field: "credentials", Message: fmt.Sprintf("credential name %q must match ^[A-Z][A-Z0-9_]*$", cred)})
				continue
			}
			if reservedCredNames[cred] {
				errs = append(errs, ValidationError{StepID: stepID, Field: "credentials", Message: fmt.Sprintf("credential name %q is reserved and cannot be used", cred)})
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

// StepRef describes a reference to another step's output or status in a template.
type StepRef struct {
	StepID    string
	Field     string // "Output", "Status", "Diff", "Error", or lowercase condition variants
	OutputKey string // non-empty only for .Steps.X.Output.Y
}

// extractTemplateRefs parses a Go template string and extracts .Params.X and .Steps.X.Field
// references. When conditionCtx is true, it expects lowercase keys (params/steps) as used
// by evalCondition in dag.go.
//
// Template parse errors are silently ignored (returning nil, nil, nil) because the runtime
// will catch those when the template is actually executed.
func extractTemplateRefs(templateStr string, conditionCtx bool) (paramRefs []string, stepRefs []StepRef, err error) {
	if templateStr == "" {
		return nil, nil, nil
	}

	paramKey := "Params"
	stepsKey := "Steps"
	if conditionCtx {
		paramKey = "params"
		stepsKey = "steps"
	}

	trees, parseErr := parse.Parse("t", templateStr, "{{", "}}", templateBuiltinFuncs)
	if parseErr != nil {
		// Silently ignore parse errors — runtime will catch them
		return nil, nil, nil
	}
	tree, ok := trees["t"]
	if !ok || tree == nil || tree.Root == nil {
		return nil, nil, nil
	}

	var paramSet []string
	var seenParams = make(map[string]bool)
	var seenSteps = make(map[string]bool)

	var walkNode func(n parse.Node)
	walkNode = func(n parse.Node) {
		if n == nil {
			return
		}
		switch node := n.(type) {
		case *parse.ListNode:
			if node == nil {
				return
			}
			for _, child := range node.Nodes {
				walkNode(child)
			}
		case *parse.ActionNode:
			walkNode(node.Pipe)
		case *parse.IfNode:
			walkNode(node.Pipe)
			walkNode(node.List)
			walkNode(node.ElseList)
		case *parse.RangeNode:
			walkNode(node.Pipe)
			walkNode(node.List)
			walkNode(node.ElseList)
		case *parse.WithNode:
			walkNode(node.Pipe)
			walkNode(node.List)
			walkNode(node.ElseList)
		case *parse.PipeNode:
			if node == nil {
				return
			}
			for _, cmd := range node.Cmds {
				walkNode(cmd)
			}
		case *parse.CommandNode:
			for _, arg := range node.Args {
				walkNode(arg)
			}
		case *parse.FieldNode:
			idents := node.Ident
			if len(idents) >= 2 && idents[0] == paramKey {
				key := idents[1]
				if !seenParams[key] {
					seenParams[key] = true
					paramSet = append(paramSet, key)
				}
			} else if len(idents) >= 3 && idents[0] == stepsKey {
				ref := StepRef{
					StepID: idents[1],
					Field:  idents[2],
				}
				if len(idents) >= 4 && idents[2] == "Output" {
					ref.OutputKey = idents[3]
				}
				compositeKey := ref.StepID + "." + ref.Field + "." + ref.OutputKey
				if !seenSteps[compositeKey] {
					seenSteps[compositeKey] = true
					stepRefs = append(stepRefs, ref)
				}
			}
		}
	}

	walkNode(tree.Root)

	sort.Strings(paramSet)
	if len(paramSet) == 0 {
		paramSet = nil
	}
	if len(stepRefs) == 0 {
		stepRefs = nil
	}
	return paramSet, stepRefs, nil
}

// validateJSONParamsInRepositories checks that any json-typed parameter referenced in a
// repositories: template is piped through toJSON. Without it the Go template engine renders
// the value as a Go fmt string (e.g. "[map[url:...]]") instead of JSON, causing a parse
// failure at runtime.
//
// It detects pipes of the form {{ .Params.X }} where X is a json-typed param and the pipe
// contains no toJSON call. It does NOT flag {{ .Params.X | toJSON }} or literal JSON arrays.
func validateJSONParamsInRepositories(def model.WorkflowDef) []ValidationError {
	jsonParams := make(map[string]bool, len(def.Parameters))
	for _, p := range def.Parameters {
		if p.Type == "json" {
			jsonParams[p.Name] = true
		}
	}
	if len(jsonParams) == 0 {
		return nil
	}

	var errs []ValidationError
	for _, step := range def.Steps {
		repoStr, ok := step.Repositories.(string)
		if !ok || repoStr == "" {
			continue
		}
		for _, name := range jsonParamsWithoutToJSON(repoStr, jsonParams) {
			errs = append(errs, ValidationError{
				StepID:  step.ID,
				Field:   "repositories",
				Message: fmt.Sprintf("json-typed parameter %q must be piped through toJSON (use {{ .Params.%s | toJSON }})", name, name),
			})
		}
	}
	return errs
}

// jsonParamsWithoutToJSON walks the template AST and returns the names of json-typed
// parameters that appear in a pipe without a toJSON call.
func jsonParamsWithoutToJSON(tmpl string, jsonParams map[string]bool) []string {
	trees, err := parse.Parse("t", tmpl, "{{", "}}", templateBuiltinFuncs)
	if err != nil {
		return nil // parse errors are reported by validateTemplateRefs
	}
	tree, ok := trees["t"]
	if !ok || tree == nil || tree.Root == nil {
		return nil
	}

	var violations []string
	seen := make(map[string]bool)

	var walkNode func(n parse.Node)
	walkNode = func(n parse.Node) {
		if n == nil {
			return
		}
		switch node := n.(type) {
		case *parse.ListNode:
			for _, child := range node.Nodes {
				walkNode(child)
			}
		case *parse.ActionNode:
			// An action wraps a single top-level pipe — inspect it directly.
			checkPipe(node.Pipe, jsonParams, seen, &violations)
			// Also descend into any nested pipes inside commands.
			for _, cmd := range node.Pipe.Cmds {
				for _, arg := range cmd.Args {
					walkNode(arg)
				}
			}
		case *parse.IfNode:
			walkNode(node.List)
			walkNode(node.ElseList)
		case *parse.RangeNode:
			walkNode(node.List)
			walkNode(node.ElseList)
		case *parse.WithNode:
			walkNode(node.List)
			walkNode(node.ElseList)
		}
	}
	walkNode(tree.Root)
	return violations
}

// checkPipe examines one PipeNode: if it references a json-typed param and has no toJSON
// call, the param name is appended to violations.
func checkPipe(pipe *parse.PipeNode, jsonParams map[string]bool, seen map[string]bool, violations *[]string) {
	if pipe == nil {
		return
	}
	var jsonParamName string
	hasToJSON := false
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			switch a := arg.(type) {
			case *parse.FieldNode:
				if len(a.Ident) >= 2 && a.Ident[0] == "Params" && jsonParams[a.Ident[1]] {
					jsonParamName = a.Ident[1]
				}
			case *parse.IdentifierNode:
				if a.Ident == "toJSON" {
					hasToJSON = true
				}
			}
		}
	}
	if jsonParamName != "" && !hasToJSON && !seen[jsonParamName] {
		seen[jsonParamName] = true
		*violations = append(*violations, jsonParamName)
	}
}

// upstreamOf returns a set of all step IDs that are upstream of (ancestors of) the given step,
// using BFS over DependsOn links.
func upstreamOf(stepID string, def model.WorkflowDef) map[string]bool {
	// Build direct-parent map: stepID -> list of steps it depends on
	dependsMap := make(map[string][]string, len(def.Steps))
	for _, step := range def.Steps {
		dependsMap[step.ID] = step.DependsOn
	}

	visited := make(map[string]bool)
	queue := []string{stepID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range dependsMap[cur] {
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	return visited
}

// validateTemplateRefs validates all template references in step prompts, action configs,
// and step conditions, checking that referenced params and steps exist and are reachable.
func validateTemplateRefs(def model.WorkflowDef) []ValidationError {
	var errs []ValidationError

	// Build lookup maps
	paramSet := make(map[string]bool, len(def.Parameters))
	for _, p := range def.Parameters {
		paramSet[p.Name] = true
	}

	stepByID := make(map[string]*model.StepDef, len(def.Steps))
	for i := range def.Steps {
		stepByID[def.Steps[i].ID] = &def.Steps[i]
	}

	for i := range def.Steps {
		step := &def.Steps[i]
		upstream := upstreamOf(step.ID, def)

		// Collect template strings with their source field label
		type taggedTemplate struct {
			tmpl  string
			field string
		}
		var templates []taggedTemplate
		if step.Execution != nil && step.Execution.Prompt != "" {
			templates = append(templates, taggedTemplate{tmpl: step.Execution.Prompt, field: "execution.prompt"})
		}
		// Collect string values from action config (sorted for deterministic error ordering).
		if step.Action != nil {
			keys := make([]string, 0, len(step.Action.Config))
			for k := range step.Action.Config {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if s, ok := step.Action.Config[k].(string); ok && s != "" {
					templates = append(templates, taggedTemplate{tmpl: s, field: "action.config"})
				}
			}
		}

		for _, tt := range templates {
			paramRefs, stepRefs, _ := extractTemplateRefs(tt.tmpl, false)

			for _, pRef := range paramRefs {
				if !paramSet[pRef] {
					errs = append(errs, ValidationError{
						StepID:  step.ID,
						Field:   tt.field,
						Message: fmt.Sprintf("template references unknown parameter %q", pRef),
					})
				}
			}

			for _, sRef := range stepRefs {
				refStep, exists := stepByID[sRef.StepID]
				if !exists {
					errs = append(errs, ValidationError{
						StepID:  step.ID,
						Field:   tt.field,
						Message: fmt.Sprintf("template references unknown step %q", sRef.StepID),
					})
					continue
				}
				if !upstream[sRef.StepID] {
					errs = append(errs, ValidationError{
						StepID:  step.ID,
						Field:   tt.field,
						Message: fmt.Sprintf("template references step %q which is not an upstream dependency", sRef.StepID),
					})
					continue
				}
				// If step has an output schema and OutputKey is specified, validate the field exists
				if sRef.OutputKey != "" && refStep.Execution != nil && refStep.Execution.Output != nil {
					schema := refStep.Execution.Output.Schema
					if _, fieldExists := schema[sRef.OutputKey]; !fieldExists {
						errs = append(errs, ValidationError{
							StepID:  step.ID,
							Field:   tt.field,
							Message: fmt.Sprintf("template references output field %q on step %q which is not in its output schema", sRef.OutputKey, sRef.StepID),
						})
					}
				}
				// If step has an action, validate output field ref against action contract
				if sRef.OutputKey != "" && refStep.Action != nil {
					contract, cOK := defaultRegistry.Get(refStep.Action.Type)
					if cOK && !contract.HasOutputField(sRef.OutputKey) {
						errs = append(errs, ValidationError{
							StepID: step.ID,
							Field:  tt.field,
							Message: fmt.Sprintf(".Steps.%s.Output.%s: action %q does not declare output field %q; available: %s",
								sRef.StepID, sRef.OutputKey, refStep.Action.Type, sRef.OutputKey, contract.OutputFieldNames()),
						})
					}
				}
			}
		}

		// Validate condition template (conditionCtx=true)
		if step.Condition != "" {
			paramRefs, stepRefs, _ := extractTemplateRefs(step.Condition, true)
			for _, pRef := range paramRefs {
				if !paramSet[pRef] {
					errs = append(errs, ValidationError{
						StepID:  step.ID,
						Field:   "condition",
						Message: fmt.Sprintf("condition references unknown parameter %q", pRef),
					})
				}
			}
			for _, sRef := range stepRefs {
				if _, exists := stepByID[sRef.StepID]; !exists {
					errs = append(errs, ValidationError{
						StepID:  step.ID,
						Field:   "condition",
						Message: fmt.Sprintf("condition references unknown step %q", sRef.StepID),
					})
				}
				// Note: upstream check intentionally skipped for conditions —
				// conditions can reference any completed step
			}
		}
	}

	return errs
}
