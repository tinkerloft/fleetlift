# Sandbox Sidecar Agent Architecture

How the fleetlift worker and sandbox pods communicate, the role of the `fleetlift-agent`, the file-based protocol, and how everything maps to Kubernetes primitives.

---

## Overview

The worker uses a **submit-and-poll** model: it writes a task manifest into a sandbox, the agent inside the sandbox executes the full pipeline autonomously, and the worker polls for completion. The worker never runs Claude Code or git commands itself.

```
Worker ──write──> manifest.json            write task definition
Worker ──poll───> status.json              non-blocking, 500ms interval
Worker <──read─── result.json              read structured results
Worker ──write──> steering.json            HITL feedback loop
Worker <──read─── result.json (with PRs)   final results
```

If the worker dies, Temporal reschedules on another worker which resumes polling the same sandbox. The agent keeps running independently.

---

## Components

### Worker Pod

The Temporal worker. Runs in the `fleetlift-system` namespace. Contains:
- Temporal worker (activities + workflows)
- Provider implementation (Docker or Kubernetes)
- `TransformV2` workflow logic

The worker only:
1. Creates sandbox pods/containers
2. Writes files into them (manifest, steering)
3. Reads files from them (status, result)
4. Deletes them when done

### Sandbox Pod

An isolated execution environment containing:
- `fleetlift-agent serve` — the agent process (entrypoint)
- Git, language runtimes, build tools
- No Kubernetes API access (`automountServiceAccountToken: false`)

The base image depends on execution type:
- **Agentic**: `claude-code-sandbox:latest` — includes Claude Code CLI, git, common runtimes
- **Deterministic**: a purpose-built image with the required tools + git + fleetlift-agent (e.g. `fleetlift-sandbox-openrewrite:latest`)

The worker selects the image at provision time based on `execution.image` in the task manifest. The agent doesn't know or care — it always runs the same pipeline: clone → transform → verify → collect → (optionally) create PRs.

### fleetlift-agent Binary

A statically-compiled Go binary (~10MB) built from the same module as the worker. Shares protocol types but has no Temporal dependency.

**Lifecycle**:
1. Start → watch for `/workspace/.fleetlift/manifest.json`
2. Manifest found → execute full pipeline
3. If `require_approval`: write results, set status `awaiting_input`, poll for steering
4. On `steer` → re-run Claude Code, update results, resume polling
5. On `approve` → create PRs, set status `complete`
6. On `reject`/`cancel` → set status `cancelled`, exit
7. If `!require_approval`: create PRs (transform) or finish (report), set status `complete`

### Execution Types

The agent always runs the same pipeline. The only thing that differs is the transform step:

- **Agentic** (`execution.type: "agentic"`): Runs Claude Code with the prompt.
- **Deterministic** (`execution.type: "deterministic"`): Runs `execution.command` + `execution.args` directly in the sandbox (e.g. `mvn rewrite:run`).

The agent doesn't know or care which image it's running in.

### Sandbox Image Contract

The sandbox container must:
1. Run `fleetlift-agent serve` as its entrypoint
2. Have `git` available in `$PATH`

The worker handles this automatically via **init container injection**: it adds an init container that copies the `fleetlift-agent` binary from a known image into a shared volume. The main container then runs `/agent-bin/fleetlift-agent serve`. This is the same pattern used by Istio, Vault Agent, and Dapr for sidecar injection.

```yaml
initContainers:
  - name: inject-agent
    image: fleetlift-agent:latest          # ships only the binary
    command: [cp, /usr/local/bin/fleetlift-agent, /agent-bin/]
    volumeMounts:
      - {name: agent-bin, mountPath: /agent-bin}
containers:
  - name: sandbox
    image: openrewrite/rewrite:latest      # user's tool image
    command: [/agent-bin/fleetlift-agent, serve]
    volumeMounts:
      - {name: agent-bin, mountPath: /agent-bin}
      - {name: workspace, mountPath: /workspace}
```

Benefits:
- The agent binary is updated in one place (`fleetlift-agent:latest`), not per tool image
- Users specify a tool image; the worker injects the agent transparently
- Agentic tasks use the same pattern with `claude-code-sandbox:latest` as the main image

The tool image must include `git`. Most code-transformation tool images do. If one doesn't, a derived image (`FROM tool:latest` + install git) is the fallback.

---

## File-Based Protocol

All communication between worker and agent happens via JSON files at well-known paths inside the sandbox:

```
/workspace/.fleetlift/
├── manifest.json       # Worker → Agent: task definition (written once)
├── status.json         # Agent → Worker: lightweight phase (polled frequently)
├── result.json         # Agent → Worker: full structured results
└── steering.json       # Worker → Agent: HITL instruction (deleted after processing)
```

Files are used instead of Kubernetes CR status fields because etcd has a 1.5MB object size limit. Diffs, reports, and agent output can easily exceed this.

### manifest.json (Worker → Agent)

Written once by the worker. The agent watches for this file to start.

```json
{
  "task_id": "slog-migration",
  "mode": "transform",
  "title": "Migrate to structured logging",
  "repositories": [
    {"url": "https://github.com/org/svc.git", "branch": "main", "name": "svc",
     "setup": ["go mod download"]}
  ],
  "execution": {
    "type": "agentic",
    "prompt": "Migrate from log.Printf to slog..."
  },
  "verifiers": [
    {"name": "build", "command": ["go", "build", "./..."]},
    {"name": "test", "command": ["go", "test", "./..."]}
  ],
  "timeout_seconds": 1800,
  "require_approval": true,
  "max_steering_iterations": 5,
  "pull_request": {
    "branch_prefix": "auto/slog-migration",
    "title": "Migrate to structured logging",
    "labels": ["automated"]
  },
  "git_config": {
    "user_email": "claude-agent@noreply.localhost",
    "user_name": "Claude Code Agent",
    "clone_depth": 50
  }
}
```

Also supports: `transformation` (recipe repo), `targets`, `for_each` (report iteration), `deterministic` execution type.

### status.json (Agent → Worker)

Updated at each phase transition. Small and fast to poll.

```json
{
  "phase": "executing",
  "step": "running_claude_code",
  "message": "Running Claude Code on svc...",
  "progress": {"completed_repos": 0, "total_repos": 2},
  "iteration": 0,
  "updated_at": "2024-01-15T10:05:00Z"
}
```

**Phase progression**: `initializing` → `executing` → `verifying` → `awaiting_input` → `creating_prs` → `complete` | `failed` | `cancelled`

### result.json (Agent → Worker)

Full structured results. Written after execution, updated after PRs.

```json
{
  "status": "awaiting_input",
  "repositories": [{
    "name": "svc",
    "status": "success",
    "files_modified": ["pkg/logger/logger.go"],
    "diffs": [{"path": "pkg/logger/logger.go", "status": "modified",
               "additions": 15, "deletions": 8, "diff": "..."}],
    "verifier_results": [{"name": "build", "success": true, "exit_code": 0}],
    "pull_request": null
  }],
  "agent_output": "...",
  "steering_history": [],
  "started_at": "2024-01-15T10:00:00Z",
  "completed_at": null
}
```

### steering.json (Worker → Agent)

Written by worker, polled by agent every 2 seconds. Agent deletes after processing.

```json
{"action": "steer", "prompt": "Also update test helpers", "iteration": 1, "timestamp": "..."}
```

Actions: `steer` (re-run with feedback), `approve` (create PRs), `reject`/`cancel` (abort).

---

## Temporal Workflow

The `TransformV2` workflow orchestrates the agent pattern:

```
TransformV2(task):
  sandbox = Activity(ProvisionSandbox, task.ID)
  defer Activity(CleanupSandbox, sandbox)

  manifest = buildManifest(task)
  Activity(SubmitTaskManifest, sandbox, manifest)

  // Non-blocking: agent runs full pipeline autonomously
  Activity(WaitForAgentPhase, sandbox, "awaiting_input|complete|failed")
  result = Activity(ReadAgentResult, sandbox)

  if result.failed → return failed
  if result.complete → return success (report mode, or no approval needed)

  // HITL steering loop
  if task.RequireApproval:
    cachedDiffs = extractDiffs(result)    // For CLI query handlers
    status = AwaitingApproval

    loop:
      AwaitSignal(approve | reject | steer | cancel)
      switch:
        approve → Activity(SubmitSteering, {action: "approve"})
                  Activity(WaitForAgentPhase, "complete|failed")
                  result = Activity(ReadAgentResult)
                  break
        steer   → Activity(SubmitSteering, {action: "steer", prompt: ...})
                  Activity(WaitForAgentPhase, "awaiting_input|failed")
                  result = Activity(ReadAgentResult)
                  cachedDiffs = extractDiffs(result)
                  continue
        reject  → Activity(SubmitSteering, {action: "cancel"})
                  return cancelled

  return buildTaskResult(result)
```

### Activities

| Activity | What it does | Blocking? |
|----------|-------------|-----------|
| `ProvisionSandbox` | Create container/pod with appropriate base image, wait for agent ready | Short (~30s) |
| `SubmitTaskManifest` | Write manifest.json to sandbox | Instant |
| `WaitForAgentPhase` | Poll status.json until target phase | Heartbeat-based |
| `ReadAgentResult` | Read result.json from sandbox | Instant |
| `SubmitSteeringAction` | Write steering.json to sandbox | Instant |
| `CleanupSandbox` | Delete container/pod | Instant |

`WaitForAgentPhase` is the only long-running activity. It uses Temporal heartbeats to stay alive and will be rescheduled if the worker dies.

---

## Kubernetes Architecture

No separate controller. No CRDs. The worker manages Kubernetes resources directly.

```
┌─────────────────────────────────────────────────────┐
│  Worker Pod (fleetlift-system namespace)            │
│  - Temporal worker                                  │
│  - K8s provider: creates Jobs, reads pod files      │
│    via exec (2-3 reads total)                       │
└────────────────┬────────────────────────────────────┘
                 │ creates Job
                 │ (labels: fleetlift.io/task-id={taskID})
                 ▼
┌─────────────────────────────────────────────────────┐
│  Sandbox Pod (sandbox-isolated namespace)           │
│  K8s Job with labels for tracking                   │
│  ┌───────────────────────────────────────────────┐  │
│  │ fleetlift-agent serve                         │  │
│  │ - Reads manifest.json                         │  │
│  │ - Clones, transforms, verifies autonomously   │  │
│  │ - Writes status.json, result.json             │  │
│  │ - Polls steering.json for HITL                │  │
│  │ - Creates PRs on approve                      │  │
│  └───────────────────────────────────────────────┘  │
│  automountServiceAccountToken: false                │
│  securityContext: restricted                        │
└─────────────────────────────────────────────────────┘
```

### K8s Primitives

| Primitive | How Used | Why |
|-----------|----------|-----|
| **Job** | One per sandbox. Worker creates directly. | Restart policy, TTL cleanup, pod tracking. Standard K8s primitive for run-to-completion workloads. |
| **Pod** | Created by Job (1:1). Runs `fleetlift-agent serve`. | Execution unit. Agent is the main (and only) container. |
| **Labels** | `fleetlift.io/task-id={taskID}` on Job and Pod. | Worker finds pods by task ID for file I/O. No separate mapping layer needed. |
| **Namespace** | `sandbox-isolated` (configurable). Separate from worker. | Isolation boundary. ResourceQuota, NetworkPolicy applied per-namespace. |
| **ServiceAccount** | Minimal SA with no RBAC bindings for sandbox pods. | Sandbox pods must not talk to the K8s API. `automountServiceAccountToken: false`. |
| **Secret** | `github-credentials`, `claude-credentials` in worker namespace. | Injected as env vars into sandbox pod spec at creation time. Never mounted as volumes. |
| **ResourceQuota** | Applied to `sandbox-isolated` namespace. | Caps total CPU/memory across all concurrent sandboxes. Prevents cluster overcommit. |
| **NetworkPolicy** | Applied to `sandbox-isolated` namespace. | Sandbox egress: allow HTTPS to GitHub, package registries, AI APIs. Deny all ingress. |

### K8s Provider Implementation

```go
type k8sProvider struct {
    clientset  *kubernetes.Clientset
    namespace  string   // sandbox-isolated
}

// Provision: create a Job, wait for its pod to be Running
func (p *k8sProvider) Provision(ctx, opts) (*Sandbox, error) {
    job := buildJob(opts)  // labels: fleetlift.io/task-id={taskID}
    clientset.BatchV1().Jobs(p.namespace).Create(ctx, job)
    pod := waitForPodRunning(ctx, job)
    return &Sandbox{ID: job.Name}
}

// SubmitManifest: exec into pod to write the file
func (p *k8sProvider) SubmitManifest(ctx, id, manifest) error {
    pod := findPodForJob(ctx, id)
    execWriteFile(ctx, pod, "/workspace/.fleetlift/manifest.json", manifest)
}

// PollStatus: exec cat to read the small status file
func (p *k8sProvider) PollStatus(ctx, id) (*AgentStatus, error) {
    pod := findPodForJob(ctx, id)
    data := execReadFile(ctx, pod, "/workspace/.fleetlift/status.json")
    return parseAgentStatus(data)
}

// ReadResult: exec cat to read the result file
func (p *k8sProvider) ReadResult(ctx, id) ([]byte, error) {
    pod := findPodForJob(ctx, id)
    return execReadFile(ctx, pod, "/workspace/.fleetlift/result.json")
}

// Cleanup: delete the Job (propagation: foreground deletes the pod too)
func (p *k8sProvider) Cleanup(ctx, id) error {
    clientset.BatchV1().Jobs(p.namespace).Delete(ctx, id, propagationForeground)
}
```

File reads use `kubectl exec cat` — a single exec call per file read.

### Worker RBAC

```yaml
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]    # Only for file read/write
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["github-credentials", "claude-credentials"]
```

### Sandbox Pod Security

```yaml
spec:
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000        # "agent" user
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: agent
    image: claude-code-sandbox:latest    # or deterministic image per execution.image
    command: ["fleetlift-agent", "serve"]
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
      readOnlyRootFilesystem: false  # Agent needs to write to /workspace
    resources:
      limits:
        memory: "4Gi"
        cpu: "2"
      requests:
        memory: "2Gi"
        cpu: "1"
    env:
    - name: ANTHROPIC_API_KEY
      valueFrom:
        secretKeyRef: {name: claude-credentials, key: api-key}
    - name: GITHUB_TOKEN
      valueFrom:
        secretKeyRef: {name: github-credentials, key: token}
```

---

## Docker Provider (Local Development)

The same protocol works with Docker. The Docker provider implements the same `sandbox.Provider` interface using `docker cp` (CopyTo/CopyFrom) instead of `kubectl exec`.

```
Docker Provider:
  SubmitManifest  → docker cp manifest.json container:/workspace/.fleetlift/
  PollStatus      → docker cp container:/workspace/.fleetlift/status.json (tar extract)
  ReadResult      → docker cp container:/workspace/.fleetlift/result.json (tar extract)
  SubmitSteering  → docker cp steering.json container:/workspace/.fleetlift/
```

The worker selects provider based on `SANDBOX_PROVIDER` env var (default: `docker`).

---

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Agent crashes | Status stops updating → worker detects via pod status check / heartbeat timeout → Temporal retries |
| Worker dies mid-poll | Temporal reschedules → new worker resumes polling same sandbox (agent still running) |
| Claude Code hangs | Agent enforces `timeout_seconds`, kills process, writes `failed` status |
| Sandbox OOM | Pod killed by kubelet → worker detects pod not running → reports failure |
| Steering before agent ready | Agent polls at start of each watch cycle, processes latest file |
| Multiple steers rapid-fire | Each replaces steering.json; agent reads latest when ready |
| Large diffs | Agent truncates per-file diffs (default 1000 lines per file) |

---

## Source Files

| File | Purpose |
|------|---------|
| `internal/agent/protocol/types.go` | Shared protocol types (manifest, status, result, steering) |
| `internal/agent/pipeline.go` | Agent main loop: watch manifest → execute → poll steering |
| `internal/agent/clone.go` | Git clone, credentials, setup commands |
| `internal/agent/transform.go` | Claude Code / deterministic execution |
| `internal/agent/verify.go` | Run verifiers |
| `internal/agent/collect.go` | Collect diffs, modified files, reports |
| `internal/agent/pr.go` | Create pull requests (git branch, commit, push, gh pr) |
| `cmd/agent/main.go` | Binary entrypoint: `fleetlift-agent serve` |
| `internal/activity/agent.go` | Temporal activities: SubmitManifest, WaitForPhase, ReadResult, SubmitSteering |
| `internal/activity/manifest.go` | Convert `model.Task` → `protocol.TaskManifest` |
| `internal/workflow/transform_v2.go` | TransformV2 workflow (agent-mode) |
| `internal/sandbox/provider.go` | Provider interface (extended with task ops) |
| `internal/sandbox/docker/provider.go` | Docker provider (implements task ops via CopyTo/CopyFrom) |
