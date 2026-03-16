# H4: Workflow Integration Test Harness

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development to implement this plan.

**Goal:** Temporal test environment + mock activities validating DAG orchestration end-to-end

**Architecture:** Tests live in `internal/workflow/` (not behind build tag), use `testsuite.WorkflowTestSuite` with `env.OnActivity()` mocks. Each test constructs a `WorkflowDef` inline and verifies activity call sequences + final workflow result.

**Tech Stack:** `go.temporal.io/sdk/testsuite`, `testify/assert`, `testify/mock`

---

### Task 1: Test Helpers + Happy-Path Linear Workflow

**Files:**
- Create: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write test helper**

```go
// newDAGTestEnv creates WorkflowTestSuite env with DAGWorkflow + StepWorkflow registered
// and default mocks for DB activities (UpdateRunStatus, CreateStepRun, CompleteStepRun, CreateInboxItem, CleanupSandbox)
func newDAGTestEnv(t *testing.T) *testsuite.TestWorkflowEnvironment
```

Default mocks return success for all DB/status activities. Tests override specific activities as needed.

Register both `DAGWorkflow` and `StepWorkflow` since DAG launches child StepWorkflows.

- [ ] **Step 2: Write happy-path test — 2-step linear (agent → action)**

`TestDAGWorkflow_LinearAgentThenAction`:
- Step 1: agent step (execution with prompt), depends_on: none
- Step 2: action step (slack_notify), depends_on: [step-1]
- Mock ProvisionSandbox → "sb-1"
- Mock ExecuteStep → StepOutput{Status: complete, Output: {summary: "done"}}
- Mock ExecuteAction → {status: "sent", channel: "#ops"}
- Assert: workflow completes without error
- Assert: UpdateRunStatus called with "running" then "complete"

- [ ] **Step 3: Run test, verify it passes**

```bash
go test ./internal/workflow/ -run TestDAGWorkflow_LinearAgentThenAction -v
```

- [ ] **Step 4: Commit**

---

### Task 2: Failure Propagation + Downstream Skip

**Files:**
- Modify: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write failure propagation test**

`TestDAGWorkflow_StepFailsRunFails`:
- Step 1: agent step, fails (ExecuteStep returns error)
- Step 2: depends_on step-1
- Assert: workflow returns error containing "step step-1 failed"
- Assert: UpdateRunStatus deferred call gets "failed"

- [ ] **Step 2: Write downstream skip test**

`TestDAGWorkflow_DownstreamSkippedOnFailure`:
- Step 1: agent step (succeeds)
- Step 2: agent step, depends_on [step-1] (fails)
- Step 3: agent step, depends_on [step-2]
- Assert: workflow error mentions step-2
- Assert: step-3 never executes (ProvisionSandbox/ExecuteStep NOT called for step-3)

- [ ] **Step 3: Write optional step failure doesn't fail run**

`TestDAGWorkflow_OptionalStepFailureDoesntFailRun`:
- Step 1: agent step (succeeds)
- Step 2: agent step, optional: true (fails)
- Assert: workflow completes without error

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Commit**

---

### Task 3: Fan-Out + Aggregation

**Files:**
- Modify: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write fan-out test**

`TestDAGWorkflow_FanOutParallelSteps`:
- Step 1: agent step with 2 repositories → fan-out to 2 child workflows
- Mock ProvisionSandbox → "sb-group"
- Mock ExecuteStep → complete for both
- Assert: workflow completes without error
- Assert: CreateStepRun called at least twice (one per fan-out child)

- [ ] **Step 2: Write fan-out partial failure test**

`TestDAGWorkflow_FanOutPartialFailure`:
- Step 1: agent step with 2 repos, one child fails
- Assert: workflow fails (fan-out aggregation marks step as failed)

- [ ] **Step 3: Run tests, verify pass**

- [ ] **Step 4: Commit**

---

### Task 4: Condition Evaluation

**Files:**
- Modify: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write condition-skips test**

`TestDAGWorkflow_ConditionFalseSkipsStep`:
- Step 1: agent step (succeeds)
- Step 2: depends_on [step-1], condition: `{{eq (index .steps "step-1").status "failed"}}`
- Since step-1 succeeded, condition is false → step-2 skipped
- Assert: workflow completes without error
- Assert: ExecuteStep/ExecuteAction NOT called for step-2

- [ ] **Step 2: Write condition-true test**

`TestDAGWorkflow_ConditionTrueExecutesStep`:
- Step 1: agent step (succeeds)
- Step 2: depends_on [step-1], condition: `{{eq (index .steps "step-1").status "complete"}}`
- Condition is true → step-2 executes
- Assert: workflow completes, both steps ran

- [ ] **Step 3: Run tests, verify pass**

- [ ] **Step 4: Commit**

---

### Task 5: Action Step Dispatch + Credential Preflight

**Files:**
- Modify: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write action step test**

`TestDAGWorkflow_ActionStepDispatch`:
- Single action step: type=slack_notify, config={channel: "#test", message: "hello"}
- Mock ExecuteAction → {status: "sent", channel: "#test"}
- Assert: workflow completes
- Assert: ExecuteAction called with correct actionType + config
- Assert: ProvisionSandbox NOT called (action steps don't need sandbox)

- [ ] **Step 2: Write credential preflight test**

`TestDAGWorkflow_CredentialPreflightFails`:
- Step 1: agent step with credentials: ["GITHUB_TOKEN"]
- Mock ValidateCredentials → error "missing GITHUB_TOKEN"
- Assert: workflow fails with "credential preflight" error
- Assert: no steps execute (no ProvisionSandbox/ExecuteStep calls)

- [ ] **Step 3: Write credential preflight success test**

`TestDAGWorkflow_CredentialPreflightPasses`:
- Step 1: agent step with credentials: ["GITHUB_TOKEN"]
- Mock ValidateCredentials → nil
- Mock other activities → success
- Assert: workflow completes

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Commit**

---

### Task 6: Sandbox Group Reuse + HITL Signal

**Files:**
- Modify: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write sandbox group reuse test**

`TestDAGWorkflow_SandboxGroupReuse`:
- Step 1: sandbox_group: "main", agent step
- Step 2: sandbox_group: "main", depends_on [step-1], agent step
- Mock ProvisionSandbox → "shared-sb" (called once for group)
- Assert: ProvisionSandbox called once at DAG level for the group
- Assert: both child StepWorkflows receive sandbox_id="shared-sb"
- Assert: CleanupSandbox called once in deferred cleanup

- [ ] **Step 2: Write HITL approval test**

`TestDAGWorkflow_HITLApproval`:
- Step 1: agent step, approval_policy: "always"
- Mock ExecuteStep → StepOutput with diff
- After step enters awaiting_input, send SignalApprove to child workflow
- Assert: workflow completes successfully

Use `env.RegisterDelayedCallback()` to send signal after step execution.

- [ ] **Step 3: Write HITL rejection test**

`TestDAGWorkflow_HITLRejection`:
- Step 1: agent step, approval_policy: "always"
- Send SignalReject after awaiting_input
- Assert: step fails with "rejected by user", workflow fails

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Commit**

---

### Task 7: Template Rendering + Action Config Rendering

**Files:**
- Modify: `internal/workflow/dag_integration_test.go`

- [ ] **Step 1: Write template rendering in prompts test**

`TestDAGWorkflow_TemplateRendering`:
- Parameters: {repo_url: "https://github.com/test/repo"}
- Step 1: agent step, prompt: "Analyze {{ .Params.repo_url }}"
- Mock ExecuteStep → verify prompt passed contains "Analyze https://github.com/test/repo"
- Assert: workflow completes

Use `mock.MatchedBy` on ExecuteStep to verify rendered prompt.

- [ ] **Step 2: Write action config template rendering test**

`TestDAGWorkflow_ActionConfigRendering`:
- Parameters: {slack_channel: "#ops"}
- Step 1: agent step (succeeds, output: {summary: "all good"})
- Step 2: action step, depends_on [step-1], config: {channel: "{{ .Params.slack_channel }}", message: "Result: {{ .Steps.step_1.Output.summary }}"}
- Mock ExecuteAction → verify config has rendered values
- Assert: workflow completes

Note: Step output ref syntax in action config uses `.Steps.step_1.Output.summary` — verify the template rendering path in `dag.go` `resolveStep` / action config rendering.

- [ ] **Step 3: Run all tests, verify pass**

```bash
go test ./internal/workflow/ -run TestDAGWorkflow -v -count=1
```

- [ ] **Step 4: Commit**

---

## Notes

- All tests use `env.OnActivity("ActivityName").Return(...)` pattern (not mock structs) since we're testing DAG orchestration, not individual activity behavior
- HITL tests are the trickiest — Temporal test env requires `RegisterDelayedCallback` to simulate signals arriving after workflow reaches a blocking point
- The existing `tests/integration/dag_test.go` (behind build tag) can remain — these new tests are unit-level using Temporal's mock environment
- Fan-out tests need careful activity mock setup since each child workflow calls its own set of activities
- Template rendering tests verify the DAG's `resolveStep` and action config rendering actually pass correct values to activities

## Sequencing

```
Task 1 (helpers + happy path)
    ↓
Task 2 (failure propagation) ──→ Task 3 (fan-out)
    ↓                                 ↓
Task 4 (conditions)           Task 5 (actions + creds)
    ↓                                 ↓
Task 6 (sandbox groups + HITL)  ←────┘
    ↓
Task 7 (template rendering)
```

Tasks 2-5 can run sequentially after Task 1. Task 6 depends on understanding from earlier tasks. Task 7 is the final integration-level check.
