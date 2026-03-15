# H4–H5 Consolidated Delivery Plan

**Date:** 2026-03-15
**Status:** Draft
**Branch:** `workflow_reliability`
**Depends on:** H1–H3 (complete on this branch)

---

## Goals

1. **H5: Activity Contract Registry** — typed input/output contracts per action type, validated at parse time, exposed via API for future workflow builder UI
2. **H4: Workflow Integration Test Harness** — Temporal test environment + mock sandbox/agent; validates DAG orchestration end-to-end for builtin workflows

## Key Decisions

| Decision | Resolution |
|---|---|
| Registry package location | `internal/model/` — pure data types, no cross-dependency |
| Template `{{ }}` in config values | Skip type checking, validate key names only |
| `channel` on `slack_notify` | Required |
| `create_pr` action type | Exclude from registry — it's a no-op in `ExecuteAction`, real PR creation uses dedicated `CreatePullRequest` activity. Templates keep current behavior; migration to remove `create_pr` action steps is a follow-up |
| H2 handler alignment | Verify existing handler returns match declared contracts; fix mismatches in-phase rather than retrofitting H2 |
| API endpoint | `GET /api/action-types` — returns registry for future drag-and-drop workflow builder |

---

## Phase 1: Contract Types + Registry

> Pure data types and validation logic. No integration points.

| # | Task | Files | Size |
|---|---|---|---|
| 1.1 | `ActionContract`, `FieldContract` types | `model/action_contract.go` (new) | S |
| 1.2 | `ActionRegistry` — `Register`, `Get`, `ValidateConfig`, `Types` | `model/action_contract.go` | M |
| 1.3 | Register 5 action contracts (slack_notify, github_pr_review, github_assign, github_label, github_comment) with input/output/credential declarations | `model/action_contract.go` | M |
| 1.4 | `ValidateConfig` logic — required key check, key name validation, type checking (skip `{{ }}` values) | `model/action_contract.go` | M |
| 1.5 | Unit tests: registry lookup, valid config, missing required key, unknown key, wrong type, template value skip | `model/action_contract_test.go` (new) | M |

**Contracts to register (5 actions):**

| Action | Required Inputs | Outputs | Credentials |
|---|---|---|---|
| `slack_notify` | `channel` (string), `message` (string) | `status` (string), `channel` (string) | `SLACK_BOT_TOKEN` |
| `github_pr_review` | `repo_url` (string), `pr_number` (int), `summary` (string) | `status` (string), `review_id` (int) | `GITHUB_TOKEN` |
| `github_assign` | `repo_url` (string), `issue_number` (int) | `status` (string), `reason` (string) | — |
| `github_label` | `repo_url` (string), `issue_number` (int), `labels` (array) | `status` (string), `labels` (array) | `GITHUB_TOKEN` |
| `github_comment` | `repo_url` (string), `issue_number` (int), `body` (string) | `status` (string), `comment_id` (int) | `GITHUB_TOKEN` |

`github_assign` has optional input `component` (string).

---

## Phase 2: Wire into Validation

> Replace hardcoded action type list with registry. Add config + output ref validation.

| # | Task | Files | Size |
|---|---|---|---|
| 2.1 | Add `DefaultActionRegistry()` constructor returning populated registry | `model/action_contract.go` | S |
| 2.2 | Update `ValidateWorkflow` signature: add `*ActionRegistry` param | `workflow/validate.go` | S |
| 2.3 | Replace `validActionTypes` map with `registry.Get()` lookup; delete hardcoded map | `workflow/validate.go` | S |
| 2.4 | Add config validation call for action steps via `registry.ValidateConfig()` | `workflow/validate.go` | S |
| 2.5 | Add output field cross-validation: when `{{ .Steps.X.Output.Y }}` refs an action step, verify `Y` exists in action contract outputs | `workflow/validate.go` | M |
| 2.6 | Update `ValidateWorkflow` call site to pass registry | `server/handlers/runs.go` | S |
| 2.7 | Tests: unknown config key, missing required config, wrong type, template ref to undeclared action output, template value with `{{ }}` skips type check | `workflow/validate_test.go` | M |

---

## Phase 3: Handler Alignment + Contract Conformance Tests

> Verify action handler returns match declared contracts. Fix mismatches.

| # | Task | Files | Size |
|---|---|---|---|
| 3.1 | Audit each handler's return map against its declared contract outputs | `activity/actions.go` | S |
| 3.2 | Fix `slack_notify` — currently returns `"skipped"` on missing channel; should return error since `channel` is required | `activity/actions.go` | S |
| 3.3 | Contract conformance test harness — for each registered action, verify: handler exists, required inputs produce non-nil output with declared keys, missing required input returns error | `activity/actions_contract_test.go` (new) | M |
| 3.4 | Fix builtin templates with missing/incorrect config (e.g. `incident-response` missing `channel` for `slack_notify`) | `template/workflows/*.yaml` | S |

---

## Phase 4: API Endpoint

> Expose registry for future workflow builder UI.

| # | Task | Files | Size |
|---|---|---|---|
| 4.1 | `GET /api/action-types` handler — returns list of `ActionContract` as JSON | `server/handlers/actions.go` (new) | S |
| 4.2 | Register route in router | `server/router.go` | S |
| 4.3 | Test: endpoint returns all registered actions with correct structure | `server/handlers/actions_test.go` (new) | S |

---

## Phase 5: Integration Test Harness (H4)

> Temporal test environment with mock sandbox/agent. Validates full DAG orchestration.

| # | Task | Files | Size |
|---|---|---|---|
| 5.1 | Test harness setup — `testsuite.WorkflowTestSuite` + mock `Activities` with mock sandbox client, mock credential store, mock agent runner | `workflow/dag_integration_test.go` (new) | L |
| 5.2 | Happy-path test: 2-step linear workflow (agent → action), verify step status transitions, output passing between steps | `workflow/dag_integration_test.go` | M |
| 5.3 | Fan-out test: workflow with parallel steps + join, verify all branches execute and results aggregate | `workflow/dag_integration_test.go` | M |
| 5.4 | Failure propagation test: agent step fails → verify run fails, downstream steps skipped | `workflow/dag_integration_test.go` | M |
| 5.5 | Condition evaluation test: step with `condition` field, verify skip/execute based on upstream output | `workflow/dag_integration_test.go` | M |
| 5.6 | HITL test: step with `human_in_the_loop: true`, verify workflow pauses and resumes on signal | `workflow/dag_integration_test.go` | M |
| 5.7 | Template rendering test: load a builtin template YAML, render with params, execute through test suite, verify no panics or missing refs | `workflow/dag_integration_test.go` | M |

---

## Sequencing

```
Phase 1 (registry types)
    ↓
Phase 2 (wire into validation)  →  Phase 3 (handler alignment)
    ↓                                   ↓
Phase 4 (API endpoint)           Phase 5 (integration tests)
```

- Phases 1→2 are sequential (2 uses registry from 1)
- Phases 3 and 4 can start after Phase 2
- Phase 5 is independent of 1–4 but benefits from contract conformance (Phase 3) being done first
- Phase 5 can start in parallel with Phase 3/4 if needed

---

## Files Changed Summary

| File | Change |
|---|---|
| `internal/model/action_contract.go` | **New** — types, registry, validation, builtin registrations |
| `internal/model/action_contract_test.go` | **New** — registry + ValidateConfig tests |
| `internal/workflow/validate.go` | Replace hardcoded map with registry; add config + output ref validation |
| `internal/workflow/validate_test.go` | Add action config/output validation tests |
| `internal/server/handlers/runs.go` | Pass registry to ValidateWorkflow |
| `internal/server/handlers/actions.go` | **New** — GET /api/action-types |
| `internal/server/handlers/actions_test.go` | **New** — endpoint test |
| `internal/server/router.go` | Register action-types route |
| `internal/activity/actions.go` | Fix slack_notify channel handling |
| `internal/activity/actions_contract_test.go` | **New** — contract conformance tests |
| `internal/template/workflows/*.yaml` | Fix templates with missing/incorrect config |
| `internal/workflow/dag_integration_test.go` | **New** — Temporal integration test harness |

---

## Not In Scope

- External/user-defined action types (future: YAML-defined actions)
- Action versioning
- Composite actions / reusable workflow fragments
- Full JSON Schema (`$ref`, `oneOf`, etc.)
- Runtime output validation for actions (contract conformance enforced by tests)
- `create_pr` migration from action steps to dedicated activity in templates (follow-up)
- Drag-and-drop workflow builder UI (Phase 4 enables it)
