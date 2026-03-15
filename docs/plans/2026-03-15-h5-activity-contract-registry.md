# H5: Activity Contract Registry — Design Spec

**Date:** 2026-03-15
**Status:** Draft
**Depends on:** H1–H3 (workflow validation, credential access, output schema enforcement)

---

## Motivation: GitHub Actions as Model

GitHub Actions works because each action is a tested, documented building block with enforced contracts. A user composes workflows with confidence because:

1. Every action declares its inputs (required/optional, typed) and outputs
2. Validation catches contract violations before execution
3. Outputs flow predictably between steps via `steps.X.outputs.Y`
4. Actions are independently testable

**After H1–H3, Fleetlift gets about halfway there.** H1 validates workflow structure and template refs. H2 gives action steps credential access and meaningful return values. H3 enforces output schemas on agent steps. But action steps still have untyped `config map[string]any` — pass `chanel` instead of `channel` and you discover it at runtime.

### Gap Analysis

| GitHub Actions principle | H1-H3 | Gap |
|---|---|---|
| Declared action inputs with types | No — `config map[string]any` | **H5: input schemas per action type** |
| Declared action outputs | No — hardcoded in Go, undiscoverable | **H5: output schemas per action type** |
| Validate config at parse time | No — H1 validates type name only | **H5: config validation in ValidateWorkflow** |
| Agent output enforcement | Yes (H3) | — |
| Independent action testing | No — requires Temporal + CredStore | **H5: test harness per action** |
| Composable/reusable fragments | No — monolithic YAML | Future (not H5) |
| Action versioning | No — hardcoded Go functions | Future (not H5) |
| Non-deterministic agent steps | Partial (H3 enforces schema, but extraction is best-effort) | Inherent LLM limitation |

---

## Current Action Inventory

Derived from `internal/activity/actions.go` and builtin templates:

| Action Type | Config Keys Used | Config Keys Required | Returns | Used By |
|---|---|---|---|---|
| `slack_notify` | `channel`, `message` | `message` | `nil` | audit, bug-fix, incident-response, migration |
| `github_pr_review` | `repo_url`, `pr_number`, `summary` | all | `nil` | pr-review |
| `github_assign` | `repo_url`, `issue_number`, `component` | `repo_url`, `issue_number` | `nil` (no-op) | triage |
| `github_label` | `repo_url`, `issue_number`, `labels` | all | `nil` | triage |
| `github_comment` | `repo_url`, `issue_number`, `body` | all | `nil` | triage |
| `create_pr` | `branch_prefix`, `title`, `draft` | `title` | `{"status": "skipped_in_action"}` | fleet-transform, migration |

**Key problems visible:**
- All actions return `nil` — H2 plans to fix this but no declared schema for what they'll return
- `incident-response` template omits `channel` for `slack_notify` — no validation catches this
- `github_assign` is a no-op (logs "not yet implemented") — no way to discover this from YAML
- Config key types are implicit (`pr_number` must be int, `labels` must be array)

---

## Design

### Contract Definition

New file: `internal/activity/registry.go`

```go
type ActionContract struct {
    Type        string
    Description string
    Inputs      []FieldContract
    Outputs     []FieldContract
    Credentials []string // credential names this action may use
}

type FieldContract struct {
    Name        string
    Type        string // "string", "int", "bool", "array", "object"
    Required    bool
    Description string
}

type ActionRegistry struct {
    contracts map[string]ActionContract
}

func NewActionRegistry() *ActionRegistry
func (r *ActionRegistry) Register(c ActionContract)
func (r *ActionRegistry) Get(actionType string) (ActionContract, bool)
func (r *ActionRegistry) ValidateConfig(actionType string, config map[string]any) []string
func (r *ActionRegistry) Types() []string
```

### Registry Population

Same file. Register all builtin actions at init:

```go
func NewActionRegistry() *ActionRegistry {
    r := &ActionRegistry{contracts: map[string]ActionContract{}}

    r.Register(ActionContract{
        Type:        "slack_notify",
        Description: "Send a Slack notification to a channel",
        Inputs: []FieldContract{
            {Name: "channel", Type: "string", Required: false, Description: "Slack channel (uses default if omitted)"},
            {Name: "message", Type: "string", Required: true, Description: "Message text"},
        },
        Outputs: []FieldContract{
            {Name: "status", Type: "string", Required: true, Description: "sent | failed"},
            {Name: "channel", Type: "string", Required: true, Description: "Channel message was sent to"},
        },
        Credentials: []string{"SLACK_BOT_TOKEN"},
    })

    r.Register(ActionContract{
        Type:        "github_pr_review",
        Description: "Post a review comment on a GitHub pull request",
        Inputs: []FieldContract{
            {Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
            {Name: "pr_number", Type: "int", Required: true, Description: "Pull request number"},
            {Name: "summary", Type: "string", Required: true, Description: "Review summary text"},
        },
        Outputs: []FieldContract{
            {Name: "status", Type: "string", Required: true, Description: "posted | failed"},
            {Name: "review_id", Type: "int", Required: true, Description: "GitHub review ID"},
        },
        Credentials: []string{"GITHUB_TOKEN"},
    })

    // ... github_assign, github_label, github_comment, create_pr

    return r
}
```

### Validation Integration (extends H1)

`ValidateWorkflow` already validates action type names against a hardcoded list. Replace with registry lookup + config validation:

```go
// In internal/workflow/validate.go — replace action type check

func validateActionSteps(def model.WorkflowDef, registry *ActionRegistry) []ValidationError {
    var errs []ValidationError
    for _, step := range def.Steps {
        if step.Action == nil {
            continue
        }
        contract, ok := registry.Get(step.Action.Type)
        if !ok {
            errs = append(errs, ValidationError{
                StepID: step.ID, Field: "action.type",
                Message: fmt.Sprintf("unknown action type %q; known: %s", step.Action.Type, strings.Join(registry.Types(), ", ")),
            })
            continue
        }
        // Validate config keys and types against contract
        for _, violation := range registry.ValidateConfig(step.Action.Type, step.Action.Config) {
            errs = append(errs, ValidationError{
                StepID: step.ID, Field: "action.config", Message: violation,
            })
        }
    }
    return errs
}
```

**Config validation handles templates:** Config values containing `{{ }}` are skipped for type checking (can't validate a rendered value at parse time) but the key name is still validated against the contract.

### Output Ref Cross-Validation (extends H1 step output ref check)

When H1 validates `.Steps.X.Output.Y` and step X is an action step, look up the action contract and verify field Y exists in the output schema:

```go
// step X is an action step — validate output field ref
if step.Action != nil {
    contract, _ := registry.Get(step.Action.Type)
    if !hasOutputField(contract, fieldName) {
        errs = append(errs, ValidationError{
            StepID: refStep.ID, Field: "template",
            Message: fmt.Sprintf(".Steps.%s.Output.%s: action %q does not declare output field %q; available: %s",
                stepID, fieldName, step.Action.Type, fieldName, outputFieldNames(contract)),
        })
    }
}
```

### Handler Signature Update

Action handlers currently return `nil`. Update each to return declared output fields per contract. This overlaps with H2 planned work — coordinate:

```go
// Before (current)
func (a *Activities) actionNotifySlack(ctx context.Context, config map[string]any) (map[string]any, error) {
    // ...
    return nil, a.slack.NotifySlack(...)
}

// After
func (a *Activities) actionNotifySlack(ctx context.Context, config map[string]any, creds map[string]string) (map[string]any, error) {
    err := a.slack.NotifySlack(...)
    if err != nil {
        return map[string]any{"status": "failed"}, err
    }
    return map[string]any{"status": "sent", "channel": channel}, nil
}
```

### Action Test Harness

Each action gets a test that validates its contract:

```go
// internal/activity/actions_contract_test.go

func TestActionContracts(t *testing.T) {
    registry := NewActionRegistry()
    for _, actionType := range registry.Types() {
        t.Run(actionType, func(t *testing.T) {
            contract, _ := registry.Get(actionType)

            // Verify handler exists
            // Verify required inputs produce non-nil output
            // Verify output keys match declared outputs
            // Verify missing required input returns error
        })
    }
}
```

---

## Implementation Plan

### Phase 1: Registry + Contracts

| # | Task | Files | Est |
|---|---|---|---|
| 1 | Create `ActionContract`, `FieldContract`, `ActionRegistry` types | `activity/registry.go` (new) | S |
| 2 | Register all 6 action types with input/output/credential contracts | `activity/registry.go` | M |
| 3 | Implement `ValidateConfig` — check required keys, key names, types (skip template values) | `activity/registry.go` | M |
| 4 | Unit tests for registry and config validation | `activity/registry_test.go` (new) | M |

### Phase 2: Wire into H1 Validation

| # | Task | Files | Est |
|---|---|---|---|
| 5 | Replace hardcoded action type list in `ValidateWorkflow` with registry lookup | `workflow/validate.go` | S |
| 6 | Add config validation call for action steps | `workflow/validate.go` | S |
| 7 | Add output field cross-validation for `.Steps.X.Output.Y` on action steps | `workflow/validate.go` | M |
| 8 | Pass registry to `ValidateWorkflow` (update signature + handler call site) | `workflow/validate.go`, `server/handlers/runs.go` | S |
| 9 | Tests: invalid config key, missing required config, wrong type, template ref to undeclared output | `workflow/validate_test.go` | M |

### Phase 3: Handler Updates + Contract Tests

| # | Task | Files | Est |
|---|---|---|---|
| 10 | Update all action handlers to return declared output fields (coordinate with H2) | `activity/actions.go` | M |
| 11 | Contract conformance tests — each action tested against its declared contract | `activity/actions_contract_test.go` (new) | M |
| 12 | Update builtin templates that pass incorrect/missing config (e.g. incident-response missing channel) | `template/workflows/*.yaml` | S |

### Ordering

- **Phase 1** can start independently of H1-H3
- **Phase 2** requires H1 (`ValidateWorkflow` exists)
- **Phase 3** requires H2 (handler signatures updated with credentials)
- Phase 1 → Phase 2 → Phase 3 is sequential

---

## Files Changed Summary

| File | Change |
|---|---|
| `internal/activity/registry.go` | **New** — ActionContract, FieldContract, ActionRegistry, builtin registrations |
| `internal/activity/registry_test.go` | **New** — registry + ValidateConfig tests |
| `internal/activity/actions_contract_test.go` | **New** — contract conformance tests per action |
| `internal/activity/actions.go` | Update handlers to return declared outputs (with H2) |
| `internal/workflow/validate.go` | Replace hardcoded list with registry; add config + output ref validation |
| `internal/workflow/validate_test.go` | Add action config/output validation tests |
| `internal/server/handlers/runs.go` | Pass registry to ValidateWorkflow |
| `internal/template/workflows/*.yaml` | Fix templates with missing/incorrect config |

---

## What's NOT In Scope

- **External/user-defined actions** — registry is Go code, not YAML. User-extensible actions are a separate design.
- **Action versioning** — no version field on contracts. All actions are current version.
- **Composite actions / reusable workflow fragments** — different design space.
- **Full JSON Schema** — contracts use simple `{name, type, required}` tuples, not JSON Schema `$ref` etc.
- **Runtime output validation for actions** — H3 handles agent output schemas. Action outputs are Go code returning typed maps, so contract conformance is enforced by tests, not runtime validation.

---

## Open Questions

1. **Where should the registry live?** `activity/registry.go` keeps it near the handlers, but `ValidateWorkflow` in `workflow/` needs to import it. This creates `workflow → activity` dependency. Alternative: put contracts in `model/` or a new `actiontype/` package.
2. **Template values in config validation** — skip type-checking any config value containing `{{ }}`? Or try to infer type from the parameter definition it references?
3. **Should registry be available via API?** A `GET /api/action-types` endpoint would let the UI show available actions + required config when building workflows. Low effort if registry exists.
4. **Coordinate with H2 or subsume?** H2 updates handler signatures (add credentials). H5 Phase 3 also updates handlers (return declared outputs). Could combine into one pass.
