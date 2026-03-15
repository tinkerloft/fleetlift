# H1–H3: Workflow Engine Reliability — Design Spec

**Date:** 2026-03-14
**Status:** Draft
**Tracks:** H1 (Workflow Validation), H2 (Action Step Credential Access), H3 (Output Schema Enforcement)

---

## Context

Every new workflow template currently requires manual end-to-end debugging against a live stack. Invalid YAML (bad dep refs, typos in step IDs, missing credentials, unknown action types) crashes at runtime inside Temporal activities with Go template panics or missing-key errors. Output schemas declared in YAML are decorative — nothing validates or extracts them. Action steps can't access the credential store and produce no logs or stored output.

**Goal:** Users write YAML, validation catches mistakes before execution starts, activities have clear contracts, and failures are loud and actionable.

---

## H1: Workflow Validation

### Overview

A `ValidateWorkflow` function runs in the HTTP handler (`RunsHandler.Create`) between `ParseWorkflowYAML()` and the DB insert. Invalid workflows return 400 with all errors at once — no Temporal workflow is started.

### Location

New file: `internal/workflow/validate.go`

### Function Signature

```go
func ValidateWorkflow(def model.WorkflowDef, params map[string]any) []ValidationError

type ValidationError struct {
    StepID  string // empty for workflow-level errors
    Field   string // e.g. "depends_on", "execution.prompt"
    Message string
}
```

### Checks

#### 1. Structural Validation

- **Duplicate step IDs** — scan `def.Steps`, error if any ID appears twice.
- **Empty/invalid step IDs** — must be non-empty, match `^[a-z][a-z0-9_-]*$`.
- **`depends_on` references** — each entry must be a defined step ID.
- **Circular dependency detection** — run Kahn's algorithm (topological sort). If the sorted result is shorter than the step count, there's a cycle. Report which steps are involved.
- **Unreachable steps** — after topo sort, any step that has no dependents (nothing depends on it) AND is not a "leaf" step (has `depends_on` but produces no output consumed by anyone) is flagged as a warning, not an error. True unreachable steps (not in any dependency chain and not a terminal step) are errors.
- **Step type** — each step must have exactly one of `execution` or `action` (not both, not neither).

#### 2. Parameter Validation

- **Required params present** — for each `ParameterDef` with `required: true`, check that `params` contains the key.
- **Type checking** — validate supplied param values against declared `ParameterDef.Type`:
  - `string` → must be a string
  - `int` → must be numeric (int or float64 with no fractional part)
  - `bool` → must be boolean
  - `json` → must be a map or array (already parsed from JSON request body)
- **Param refs in templates** — parse all Go templates (prompts, action configs, conditions, repository specs) and extract `.Params.X` references. Verify each `X` exists in `def.Parameters`. This uses Go's `text/template/parse` package to walk the AST — no execution needed.

#### 3. Step Output Reference Validation

- Extract `.Steps.X.*` references from all templates (prompts, action configs, repository specs). Supported accessors: `.Steps.X.Output.Y`, `.Steps.X.Status`, `.Steps.X.Diff`, `.Steps.X.Error`, and piped forms like `{{ .Steps.X.Output | toJSON }}`.
- Verify step `X` exists.
- Verify step `X` is an upstream dependency (direct or transitive) of the referencing step. A step can only access output from steps that are guaranteed to have completed before it runs.
- If step `X` has an output schema declared and the template accesses `.Steps.X.Output.Y`, verify field `Y` exists in that schema (cross-references with H3).
- **Condition templates are validated separately.** The `evalCondition` function in `dag.go` uses a different context with lowercase keys (`.steps.X.status`, `.params.X`). H1 validates condition templates against this lowercase convention, not the `RenderContext` convention.

#### 4. Action Type Validation

- Validate `action.type` against known types: `slack_notify`, `github_pr_review`, `github_assign`, `github_label`, `github_comment`, `create_pr`.
- Hardcoded list for now. H5 (future) replaces with a registry.

#### 5. Agent Type Validation

- Validate `execution.agent` against known agents: `claude-code`, `codex`, `shell`.
- Empty defaults to `claude-code` (not an error).

#### 6. Credential Reference Validation

- All credential names in `execution.credentials` and `action.credentials` must match `^[A-Z][A-Z0-9_]*$`.
- Must not be reserved env var names (`PATH`, `HOME`, `LD_PRELOAD`, `LD_LIBRARY_PATH`, `USER`, `SHELL`).
- This moves the runtime check from `provision.go` to validation time. The runtime check stays as defense-in-depth.

### HTTP Integration

In `RunsHandler.Create`, after parsing YAML and before the DB insert:

```go
if errs := workflow.ValidateWorkflow(def, req.Parameters); len(errs) > 0 {
    writeJSON(w, http.StatusBadRequest, map[string]any{
        "error":             "workflow validation failed",
        "validation_errors": errs,
    })
    return
}
```

### Template AST Walking

To extract `.Params.X` and `.Steps.X.*` references without executing templates, use `text/template/parse`:

```go
func extractTemplateRefs(templateStr string) (paramRefs []string, stepRefs []StepRef, err error)

type StepRef struct {
    StepID     string
    Field      string // "Output", "Status", "Diff", "Error"
    OutputKey  string // non-empty only for .Steps.X.Output.Y
}
```

Parse the template, walk the `parse.Tree`, look for `FieldNode` chains matching `.Params.*` and `.Steps.*.*` (with optional third level for `.Output.Y`). Handle piped expressions (`.Steps.X.Output | toJSON`) by recognizing the pipe target.

For condition templates, use a separate extractor that looks for lowercase `.params.*` and `.steps.*.*` matching the `evalCondition` context shape.

---

## H2: Action Step Credential Access + Logging

### Overview

Action steps get credential store access (not just worker env vars), log what they do, and store meaningful results as step output.

### Model Change

**`internal/model/workflow.go`** — add `Credentials` to `ActionDef`:

```go
type ActionDef struct {
    Type        string         `yaml:"type"`
    Config      map[string]any `yaml:"config"`
    Credentials []string       `yaml:"credentials,omitempty"`
}
```

### Credential Flow

No new activity needed. The `Activities` struct already has `CredStore` with a `GetBatch` method, used by `ProvisionSandbox` in `provision.go`. The `ExecuteAction` activity calls `a.CredStore.GetBatch(ctx, teamID, credNames)` directly — no extra Temporal activity round-trip.

**`internal/workflow/dag.go`** — changes to the action step path:

1. Update `executeAction` function signature to accept `teamID`, `stepRunID`, and credential names:
   ```go
   func executeAction(ctx workflow.Context, step model.StepDef, teamID string, stepRunID string, credNames []string) *model.StepOutput
   ```
2. Pass `input.TeamID`, `stepRunID`, and `step.Action.Credentials` from the call site (line ~243).
3. The activity call passes these through to `ExecuteAction`.

**`internal/activity/actions.go`** — `ExecuteAction` fetches credentials internally:
```go
func (a *Activities) ExecuteAction(ctx context.Context, stepRunID string, actionType string,
    config map[string]any, teamID string, credNames []string) (map[string]any, error) {

    credentials := map[string]string{}
    if len(credNames) > 0 {
        var err error
        credentials, err = a.CredStore.GetBatch(ctx, teamID, credNames)
        if err != nil {
            return nil, fmt.Errorf("fetch credentials: %w", err)
        }
    }
    // dispatch to handler with credentials...
}
```

**Backward compatibility:** If `credentials` is empty, action handlers fall back to `os.Getenv` (existing behavior). This avoids breaking templates that don't declare credentials yet.

### Action Handler Credential Access

Each action handler receives the credentials map. Handlers look up credentials by name, falling back to env vars for backward compatibility:

```go
func (a *Activities) actionGitHubPostReviewComment(ctx context.Context,
    config map[string]any, credentials map[string]string) (map[string]any, error) {

    token := credentials["GITHUB_TOKEN"]
    if token == "" {
        token = os.Getenv("GITHUB_TOKEN") // fallback for templates without credentials declared
    }
    if token == "" {
        return nil, fmt.Errorf("github_pr_review: GITHUB_TOKEN not available")
    }
    // ...
}
```

### Action Logging

Action steps currently produce no logs visible in the UI. Add logging via the same `batchInsertLogs` mechanism used by agent steps:

- Before dispatching: log "Executing action: slack_notify" with config summary (redact credential values).
- After completion: log "Action completed successfully" or "Action failed: ..." with error details.
- This requires passing `stepRunID` to the activity so it can write log lines. The `stepRunID` is already created in the caller scope in `dag.go` (via `CreateStepRunActivity`) but not currently passed to `executeAction`. The updated `executeAction` function and `ExecuteAction` activity signatures (shown in Credential Flow above) include `stepRunID`.

### Action Result Storage

Currently several actions return `nil` for their result:
- `slack_notify` → `return nil, a.actionNotifySlack(...)`
- `github_pr_review` → `return nil, a.actionGitHubPostReviewComment(...)`

Change all action handlers to return meaningful `map[string]any` results:

| Action | Return Value |
|--------|-------------|
| `slack_notify` | `{"status": "sent", "channel": "..."}` |
| `github_pr_review` | `{"status": "posted", "review_id": N}` |
| `github_assign` | `{"status": "assigned"}` or `{"status": "skipped", "reason": "not configured"}` |
| `github_label` | `{"status": "labeled", "labels": [...]}` |
| `github_comment` | `{"status": "posted", "comment_id": N}` |

These results are stored as step output and available to downstream steps via `{{ .Steps.X.Output.status }}`.

---

## H3: Output Schema Enforcement

### Overview

The `output.schema` field in YAML step definitions becomes enforced. Agent prompts get schema instructions appended, and results are validated against the declared schema. Missing required fields fail the step.

### Prompt Suffix Injection

**`internal/activity/execute.go`** — before running the agent (after building the prompt, before `runner.Run`):

If `stepDef.Execution.Output` is set, append a structured output instruction to the prompt:

```go
func appendOutputSchemaInstructions(prompt string, schema map[string]any) string
```

Generates something like:

```
IMPORTANT: At the end of your response, you MUST output a JSON object with exactly these fields,
wrapped in a ```json fenced code block:

{"root_cause": "<string>", "affected_files": ["<array of strings>"], "severity": "<string>"}

This structured output is required for downstream workflow steps.
```

The schema map is `{field: type}` where type is `"string"`, `"array"`, `"boolean"`, `"number"`, or `"object"`. The instruction shows the expected shape with placeholder values.

### Extraction

**New function in `internal/activity/execute.go`:**

```go
func extractSchemaFields(resultText string, schema map[string]any) (map[string]any, error)
```

After `extractStructuredOutput()`:

1. If the structured output already contains all declared schema fields → use it directly (agent returned a structured map).
2. Otherwise, look for a ```json fenced code block in the `result` string field. Parse the last such block found.
3. If no fenced block, try parsing the last `{...}` JSON object in the result text.
4. If extraction fails entirely → return error.

### Validation

After extraction, validate field presence and basic types:

```go
func validateOutputSchema(output map[string]any, schema map[string]any) []string
```

- For each field in the schema, check it exists in the output.
- Basic type check: `"string"` → must be string, `"array"` → must be slice, `"boolean"` → must be bool, `"number"` → must be numeric.
- Returns list of violation messages. If non-empty, step fails with: `"output schema validation failed: missing fields: root_cause, severity"`.

### Integration Point

In `ExecuteStep`, after line ~228:

```go
structured := extractStructuredOutput(lastOutput)

// If the step declares an output schema, enforce it.
if stepInput.StepDef.Execution != nil && stepInput.StepDef.Execution.Output != nil {
    schema := stepInput.StepDef.Execution.Output.Schema
    resultText, _ := lastOutput["result"].(string)

    // Try to extract declared schema fields from the agent's result text.
    // The result text is typically a string (agent's response), not a structured map.
    extracted, err := extractSchemaFields(resultText, schema)
    if err == nil {
        // Replace structured output with only the declared schema fields.
        // This strips agent metadata (session_id, usage, etc.) so downstream
        // steps get clean, predictable data.
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

### Cross-Reference with H1

H1 validation checks `.Steps.X.Output.Y` references. If step X declares an output schema, H1 verifies that field Y exists in that schema at validation time. This catches field name typos like `{{ .Steps.review.Output.summry }}` before execution.

### Existing Template Impact

7 output schemas across 6 template files today (all decorative):

| Template | Schema Fields |
|----------|--------------|
| `pr-review` (clone step) | `diff`, `title`, `base_branch` |
| `pr-review` (review step) | `comments`, `summary`, `approval` |
| `incident-response` | `root_cause`, `affected_files`, `severity` |
| `audit` | `findings`, `risk_level`, `summary` |
| `triage` | `type`, `component`, `severity`, `summary` |
| `migration` | `affected`, `impact_level`, `affected_files` |
| `dependency-update` | `affected`, `files` |

These will become enforced. The prompt suffix injection should make agents comply in most cases. Steps that don't produce valid output will fail loudly — which is the desired behavior.

---

## Files Changed Summary

| File | Change | Track |
|------|--------|-------|
| `internal/workflow/validate.go` | **New** — `ValidateWorkflow`, `ValidationError`, template AST walker | H1 |
| `internal/workflow/validate_test.go` | **New** — tests for all validation checks | H1 |
| `internal/server/handlers/runs.go` | Call `ValidateWorkflow` before DB insert | H1 |
| `internal/model/workflow.go` | Add `Credentials` to `ActionDef` | H2 |
| `internal/workflow/dag.go` | Update `executeAction` signature to pass `teamID`, `stepRunID`, credential names; update call site | H2 |
| `internal/activity/actions.go` | New signature with `stepRunID` + `teamID` + `credNames`, call `CredStore.GetBatch`, return meaningful results, add logging | H2 |
| `internal/activity/execute.go` | Append schema instructions to prompt, extract + validate output schema | H3 |

---

## What's NOT In Scope

- **JSON Schema validation library** — schemas are simple `{field: type}` maps, not full JSON Schema. No need for a library.
- **Second LLM call for extraction** — if the agent doesn't comply with prompt instructions, the step fails. No post-processing LLM call.
- **Action config schema validation** (H5) — each action type having declared required config fields is future work.
- **Template rendering safety** (H6) — fuzzy matching for typos ("did you mean 'review'?") is future work.
- **Credential existence check** — H1 validates credential name format, not whether the credential exists in the DB. Existence is checked at runtime by `CredStore.GetBatch` inside `ExecuteAction`.

---

## Open Questions

1. **Validation on template save too?** Currently validation runs on run creation. Should `POST /api/workflows` (template save) also validate? This would catch errors even earlier but the endpoint doesn't have runtime parameters to validate against.
2. **Schema field optionality?** Current schemas don't distinguish required vs optional fields. Should we add `required: true` per-field, or treat all declared fields as required? Recommendation: all required for now, add optionality later if needed.
