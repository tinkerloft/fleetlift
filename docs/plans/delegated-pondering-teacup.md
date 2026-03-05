# Fleetlift → Agentbox + Fleetlift Split Plan

Three documents:
1. [Agentbox Specification](#1-agentbox-specification)
2. [Fleetlift Specification](#2-fleetlift-specification)
3. [Split Plan](#3-split-plan)

---

## 1. Agentbox Specification

### Vision

A generic agent hosting platform that provisions sandboxed containers (Docker locally, K8s in production), manages agent lifecycle via a file-based protocol, and provides Temporal activity helpers for durable orchestration. Consumers (like Fleetlift, Auto-Debt-Slayer, etc.) bring their own domain logic, manifests, and agent implementations.

### What Agentbox IS
- Container lifecycle management (provision, exec, copy, cleanup)
- A file-based IPC protocol (manifest in → status polling → result out → steering loop)
- Docker + K8s sandbox providers behind a `Provider` interface
- Temporal activity wrappers for sandbox + agent protocol operations
- An agent binary framework (pipeline skeleton, deps interfaces, verifier execution)
- **AI CLI/SDK agnostic** — the platform does not know or care which AI tool runs inside the sandbox

### What Agentbox IS NOT
- A workflow engine (consumers bring Temporal/Argo/Restate)
- An AI runtime (consumers bring Claude Code/Codex/Gemini/Aider/any CLI or SDK)
- A domain model (consumers define their own task/result types)
- A git platform (consumers handle cloning, PRs, credentials)
- A cost calculator (consumers interpret metadata; platform only transports it)

### Multi-Agent CLI/SDK Support

Agentbox is designed so the AI tool running inside the sandbox is a consumer decision, not a platform decision. The manifest and result payloads are opaque `json.RawMessage` / `[]byte` — agentbox never interprets them. Only the status (phases, steering, metadata) uses platform-defined types.

**How different agent CLIs plug in:**

| Agent CLI/SDK | Container Image | Invocation (consumer's step func) | Token Extraction |
|---|---|---|---|
| Claude Code | Image with `@anthropic-ai/claude-code` | `claude -p --output-format json` | Parse JSON stdout → write to `Metadata` |
| OpenAI Codex | Image with `codex` | `codex --quiet --json` | Parse JSON stdout → write to `Metadata` |
| Gemini CLI | Image with `gemini-cli` | `gemini -p` | Parse API response → write to `Metadata` |
| Aider | Image with `aider` | `aider --yes --message "..."` | Regex parse summary line → write to `Metadata` |
| Custom SDK | Image with consumer's binary | Consumer's code calls any API | Consumer writes to `Metadata` |
| Deterministic (no AI) | Any OCI image | `bash -c "..."` | No token metadata |

**The consumer controls:**
1. Which container image to use (which CLI is installed)
2. The agent binary's step functions (which CLI to invoke, how to parse output)
3. The manifest schema (what fields the agent needs)
4. The result schema (what the agent reports back)
5. How to interpret `Metadata` (token counts, cost calculation, model info)

**agentbox controls:**
1. Container lifecycle (provision, exec, cleanup)
2. File-based protocol mechanics (manifest delivery, status polling, result retrieval)
3. Steering loop (pause, approve, reject, steer, cancel)
4. `Metadata` transport (opaque key-value pairs on status and result)

### Public API Surface

#### `sandbox` package

```go
// Container lifecycle — unchanged from current
type Provider interface {
    Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)
    Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)
    ExecShell(ctx context.Context, id string, command string, user string) (*ExecResult, error)
    CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error
    CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)
    Status(ctx context.Context, id string) (*SandboxStatus, error)
    Cleanup(ctx context.Context, id string) error
    Name() string
}

// Agent file-protocol operations — CHANGED: PollStatus returns []byte
type AgentProvider interface {
    Provider
    SubmitManifest(ctx context.Context, id string, manifest []byte) error
    PollStatus(ctx context.Context, id string) ([]byte, error)        // was *protocol.AgentStatus
    ReadResult(ctx context.Context, id string) ([]byte, error)
    SubmitSteering(ctx context.Context, id string, instruction []byte) error
}

// CHANGED: UseAgentMode → explicit Cmd override; added VolumeMounts
type ProvisionOptions struct {
    TaskID       string
    Image        string
    Cmd          []string              // NEW: explicit command override (empty = use image CMD)
    WorkingDir   string
    Env          map[string]string
    Resources    ResourceLimits
    Volumes      []VolumeMount         // NEW: named persistent volumes
    Timeout      time.Duration
    // K8s-specific
    RuntimeClass   string             // NEW: e.g. "gvisor" — see Security section
    NodeSelector   map[string]string  // NEW: for dedicated node pools
    Tolerations    []Toleration       // NEW: for dedicated node pools
    UserNamespace  bool               // NEW: enable user namespace isolation
    OwnerRef       *OwnerReference    // NEW: for K8s GC (orphan cleanup)
}

type VolumeMount struct {
    Name      string
    HostPath  string   // Docker
    ClaimName string   // K8s PVC
    MountPath string
    ReadOnly  bool
}

type ResourceLimits struct {
    MemoryBytes      int64
    CPUQuota         int64
    EphemeralStorage int64  // NEW: K8s ephemeral-storage limit
}
```

#### `protocol` package (generic subset only)

```go
const (
    DefaultBasePath  = "/workspace/.agentbox"
    ManifestFilename = "manifest.json"
    StatusFilename   = "status.json"
    ResultFilename   = "result.json"
    SteeringFilename = "steering.json"
)

// Computed paths
func ManifestPath(base string) string
func StatusPath(base string) string
func ResultPath(base string) string
func SteeringPath(base string) string

// Phase lifecycle — generic phases only
type Phase string
const (
    PhaseInitializing Phase = "initializing"
    PhaseExecuting    Phase = "executing"
    PhaseVerifying    Phase = "verifying"
    PhaseAwaitingInput Phase = "awaiting_input"  // HITL pause point
    PhaseComplete     Phase = "complete"
    PhaseFailed       Phase = "failed"
    PhaseCancelled    Phase = "cancelled"
)

// Status written by agent → read by orchestrator
type AgentStatus struct {
    Phase     Phase             `json:"phase"`
    Step      string            `json:"step,omitempty"`
    Message   string            `json:"message,omitempty"`
    Progress  *StatusProgress   `json:"progress,omitempty"`
    Iteration int               `json:"iteration"`
    Metadata  map[string]string `json:"metadata,omitempty"`  // opaque KV — consumer writes token counts, cost, model info
    UpdatedAt time.Time         `json:"updated_at"`
}

// Well-known metadata keys (conventions, not enforced):
//   "input_tokens"  — total input tokens consumed
//   "output_tokens" — total output tokens consumed
//   "cache_read_tokens" — cache read tokens (Claude-specific)
//   "model"         — model ID used (e.g. "claude-sonnet-4-6")
//   "cost_usd"      — estimated cost in USD (consumer-calculated)
//   "agent_cli"     — which CLI was used (e.g. "claude-code", "codex", "aider")

type StatusProgress struct {
    Current int `json:"current"`
    Total   int `json:"total"`
}

// Steering written by orchestrator → read by agent
type SteeringAction string
const (
    SteeringActionApprove SteeringAction = "approve"
    SteeringActionReject  SteeringAction = "reject"
    SteeringActionCancel  SteeringAction = "cancel"
    SteeringActionSteer   SteeringAction = "steer"
)

type SteeringInstruction struct {
    Action    SteeringAction `json:"action"`
    Prompt    string         `json:"prompt,omitempty"`
    Iteration int            `json:"iteration"`
}
```

#### `agent` package (primitives — consumer builds their own main loop)

```go
// Deps interfaces — unchanged
type FileSystem interface { ... }
type CommandExecutor interface { ... }
type CommandOpts struct { ... }
type CommandResult struct { ... }

// Config for file-based protocol I/O
type ProtocolConfig struct {
    BasePath             string        // default: protocol.DefaultBasePath
    ManifestPollInterval time.Duration // default: 500ms
    SteeringPollInterval time.Duration // default: 2s
}

// Primitives — consumer calls these from their own main loop
type Protocol struct { ... }
func NewProtocol(cfg ProtocolConfig, fs FileSystem, logger *slog.Logger) *Protocol

// Manifest: block until manifest.json appears, return raw bytes
func (p *Protocol) WaitForManifest(ctx context.Context) (json.RawMessage, error)

// Status: atomic write of status.json
func (p *Protocol) WriteStatus(status protocol.AgentStatus)

// Result: atomic write of result.json
func (p *Protocol) WriteResult(result json.RawMessage) error

// Steering: block until steering.json appears, consume atomically
func (p *Protocol) WaitForSteering(ctx context.Context) (*protocol.SteeringInstruction, error)
```

**Consumer usage pattern** (e.g. fleetlift-agent `main.go`):
```go
func main() {
    proto := agent.NewProtocol(agent.ProtocolConfig{BasePath: basePath}, fs, logger)

    raw, _ := proto.WaitForManifest(ctx)
    var manifest fleetproto.TaskManifest
    json.Unmarshal(raw, &manifest)

    proto.WriteStatus(protocol.AgentStatus{Phase: protocol.PhaseExecuting})
    // ... consumer does whatever it wants: clone, run claude, run codex, etc.

    if manifest.RequireApproval {
        proto.WriteStatus(protocol.AgentStatus{Phase: protocol.PhaseAwaitingInput})
        steering, _ := proto.WaitForSteering(ctx)
        // ... handle steering action
    }

    proto.WriteResult(resultBytes)
}
```

This gives consumers **full control** over their execution flow while agentbox handles the file protocol mechanics (atomic writes, polling, rename-based consumption).

#### `temporalkit` package (Temporal activity helpers)

```go
type AgentActivities struct { provider sandbox.AgentProvider }
func NewAgentActivities(provider sandbox.AgentProvider) *AgentActivities

// All use []byte — consumer parses into their own types
type SubmitManifestInput struct {
    SandboxID string `json:"sandbox_id"`
    Manifest  []byte `json:"manifest"`
}
func (a *AgentActivities) SubmitManifest(ctx context.Context, input SubmitManifestInput) error

type WaitForPhaseInput struct {
    SandboxID    string   `json:"sandbox_id"`
    TargetPhases []string `json:"target_phases"`
}
func (a *AgentActivities) WaitForPhase(ctx context.Context, input WaitForPhaseInput) (*protocol.AgentStatus, error)

type ReadResultInput struct {
    SandboxID string `json:"sandbox_id"`
}
func (a *AgentActivities) ReadResult(ctx context.Context, input ReadResultInput) ([]byte, error)

type SubmitSteeringInput struct {
    SandboxID   string `json:"sandbox_id"`
    Instruction []byte `json:"instruction"`
}
func (a *AgentActivities) SubmitSteering(ctx context.Context, input SubmitSteeringInput) error

// Sandbox lifecycle activities
type SandboxActivities struct { provider sandbox.Provider }
func NewSandboxActivities(provider sandbox.Provider) *SandboxActivities

type ProvisionInput struct {
    TaskID    string              `json:"task_id"`
    Image     string              `json:"image"`
    Cmd       []string            `json:"cmd,omitempty"`
    Env       map[string]string   `json:"env"`
    Resources sandbox.ResourceLimits `json:"resources"`
    Volumes   []sandbox.VolumeMount  `json:"volumes,omitempty"`
    // K8s fields
    RuntimeClass  string                  `json:"runtime_class,omitempty"`
    NodeSelector  map[string]string       `json:"node_selector,omitempty"`
    UserNamespace bool                    `json:"user_namespace,omitempty"`
    OwnerRef      *sandbox.OwnerReference `json:"owner_ref,omitempty"`
}

type SandboxInfo struct {
    ContainerID   string `json:"container_id"`
    WorkspacePath string `json:"workspace_path"`
}
func (a *SandboxActivities) Provision(ctx context.Context, input ProvisionInput) (*SandboxInfo, error)
func (a *SandboxActivities) Cleanup(ctx context.Context, containerID string) error
```

### Gap Analysis: Current Code → Agentbox

| Gap | Current State | Required Change | Effort |
|-----|--------------|-----------------|--------|
| **Protocol is fleetlift-specific** | `protocol.TaskManifest` has Mode, PullRequest, Repositories, Transformation, ForEach, etc. | Split into generic `agentbox/protocol` (phases, steering, paths) and fleetlift-specific manifest/result types | Medium |
| **`PollStatus` returns typed struct** | `AgentProvider.PollStatus` returns `*protocol.AgentStatus` | Return `[]byte`; parse in activity layer | Small |
| **`UseAgentMode` boolean** | `ProvisionOptions.UseAgentMode` | Replace with explicit `Cmd []string` | Small |
| **No volume mount support** | Hardcoded workspace; no persistent volumes | Add `[]VolumeMount` to `ProvisionOptions` | Small |
| **Pipeline is monolithic** | `pipeline.go` hardcodes clone→transform→verify→collect→PR sequence | Refactor to step-registration model; consumers define their own step sequence | Medium |
| **Agent binary is fleetlift-specific** | `cmd/agent/main.go` runs the fleetlift pipeline directly | Agentbox provides pipeline framework; fleetlift provides its own `cmd/agent` that registers steps | Medium |
| **Activity inputs use typed protocol structs** | `SubmitTaskManifestInput.Manifest` is `protocol.TaskManifest` | Change to `[]byte` | Small |
| **Hardcoded container naming** | `claude-sandbox-{taskID}` | Configurable prefix or consumer-provided name | Trivial |
| **No K8s security features** | No RuntimeClass, UserNamespace, dedicated node pools, ownerReferences | Add fields to `ProvisionOptions`; implement in K8s provider | Medium |
| **No ephemeral storage limits** | Only memory + CPU | Add `EphemeralStorage` to `ResourceLimits` | Small |
| **Secret management undefined** | `ANTHROPIC_API_KEY` / `GITHUB_TOKEN` injected by activity code | Platform provides `Env map[string]string` passthrough; K8s provider supports `SecretRef` for env-from-secret | Medium |
| **No orphan cleanup** | Worker crash leaves sandbox jobs running | K8s: `ownerReferences` on Jobs; Docker: cleanup-on-startup scan | Medium |
| **K8s `Exec` ignores User/Env/Timeout** | Silently ignores these fields | Implement properly or document as unsupported | Small |
| **Metrics are fleetlift-branded** | `fleetlift_activity_*` metric names | Rename to `agentbox_*`; make prefix configurable | Small |
| **No metadata on status/result** | No opaque KV field for consumer data | Add `Metadata map[string]string` to `AgentStatus`; consumers write token counts, cost, model info | Small |
| **No configurable base path** | Hardcoded `/workspace/.fleetlift` | `DefaultBasePath` + config override; providers accept base path | Small |
| **No network isolation** | Sandbox has full network access | NetworkPolicy support + proxy pattern for credential shielding | Large (v2) |
| **No OTel tracing** | Only Prometheus metrics | Trace spans for sandbox lifecycle + steering iterations | Medium (v2) |

### K8s Expert Feedback — Response Matrix

| Feedback | Resolution | Where |
|---|---|---|
| **Secret management across namespaces** | `SecretEnvFrom []SecretRef` on `ProvisionOptions`; document same-namespace constraint; recommend External Secrets Operator for cross-namespace sync | Security Model |
| **gVisor not trivial outside GKE** | `RuntimeClass` field is optional; document setup requirements; don't assume gVisor availability | Security Model — Isolation Tiers |
| **Dedicated node pools for sandboxes** | `NodeSelector` + `Tolerations` on `ProvisionOptions`; document taint pattern | Security Model — Node Isolation |
| **Multiple sandboxes per node safety** | Safe with gVisor/Kata (separate kernels); runc requires pod anti-affinity for full isolation | Security Model — Node Isolation |
| **User Namespaces** | `UserNamespace` bool → `pod.spec.hostUsers: false` | Security Model |
| **Resource sizing (cpu, mem, ephemeral)** | `ResourceLimits` requires all three; requests=limits for Guaranteed QoS; sizing presets documented | Security Model — Resource Sizing |
| **Orphan sandbox Jobs on worker crash** | `OwnerRef` for same-namespace GC; label-based TTL sweep as fallback for cross-namespace | Security Model — Orphan Cleanup |
| **ownerReferences cross-namespace limitation** | Accept constraint: recommend same-namespace for owner+sandbox; fallback to label-based cleanup | Security Model — Orphan Cleanup |

### Security Model (incorporating K8s expert feedback + research)

**Isolation Tiers (consumer chooses based on trust level):**

| Tier | Isolation | When to use | ProvisionOptions |
|------|-----------|-------------|------------------|
| **Standard** | runc container, shared kernel | Trusted deterministic tasks (linters, compilers) | Default — no extra fields |
| **Hardened** | gVisor runtime class, user namespaces, dedicated node pool | Untrusted agentic workloads (Claude Code, LLM agents) | `RuntimeClass: "gvisor"`, `UserNamespace: true`, `NodeSelector` + `Tolerations` |
| **Maximum** | Firecracker microVM (via Kata Containers) | Multi-tenant, hostile code execution | `RuntimeClass: "kata"` + dedicated nodes |

**Container Runtime Isolation (`RuntimeClass`):**
- Field on `ProvisionOptions` — consumers specify `"gvisor"` or `"kata"` where available
- gVisor: native on GKE; requires manual install elsewhere — platform documents setup
- Default: no RuntimeClass override (use cluster default runc)

**Node Isolation (`NodeSelector` + `Tolerations`):**
- Recommended pattern: taint sandbox nodes with `agentbox.io/sandbox=true:NoSchedule`
- **Multi-sandbox-per-node safety** (K8s expert question): safe with gVisor/Kata (separate kernels). With runc, sandboxes share a kernel — accept risk or use 1-sandbox-per-node via pod anti-affinity
- Platform provides `PodAntiAffinity` option for consumers who want 1-sandbox-per-node

**User Namespaces:**
- `UserNamespace` bool → sets `pod.spec.hostUsers: false` in K8s Job spec
- Isolates container UID from host UID (K8s 1.25+ with `UserNamespacesSupport` feature gate, GA in 1.30)
- Prevents container escape from mapping to host root

**Network Isolation — Proxy Pattern:**
- Platform supports a `NetworkPolicy` option: `NetworkIsolation` enum (`none`, `proxy_only`, `egress_allowlist`)
- `proxy_only`: sandbox gets `--network none` (Docker) or NetworkPolicy deny-all-egress (K8s); all traffic routed through a sidecar proxy
- Proxy injects credentials into outbound requests — agent never sees raw secrets
- Proxy enforces domain allowlist — prevents data exfiltration
- Proxy logs all requests for audit trail
- **Not in v1** — document as future enhancement; v1 uses `Env` passthrough for secrets

**Secret Management:**
- **v1 (simple):** `Env map[string]string` passthrough — consumer injects secrets at activity level
- **v1 (K8s-native):** `SecretEnvFrom []SecretRef` on `ProvisionOptions` — reference K8s Secrets directly as env vars without exposing values in Temporal history
- Cross-namespace: sandbox Jobs and Secrets MUST share a namespace (K8s limitation). Document this clearly.
- Recommended: External Secrets Operator to sync secrets from Vault/AWS SM into sandbox namespace
- **Future (v2):** proxy-based credential injection (agent never touches secrets)

**Orphan Cleanup:**
- K8s primary: `OwnerRef` field on `ProvisionOptions` — K8s GC auto-deletes sandbox Job when owner is deleted
- Limitation: owner and sandbox must be same namespace (K8s cross-namespace ownerRef not supported)
- K8s fallback: label-based cleanup sweep on worker startup (`agentbox.io/task-id` label, `agentbox.io/created-at` annotation, age > configurable TTL)
- Docker: scan for `agentbox-*` containers older than configurable TTL on provider init
- Both: `Cleanup()` called in deferred activity; TTL sweep catches crashes

**Resource Sizing:**
- `ResourceLimits` includes memory, CPU, ephemeral storage — platform requires all three for K8s
- K8s: set requests = limits (Guaranteed QoS class) to prevent node resource exhaustion and noisy-neighbor issues
- Platform provides recommended sizing presets:
  - `Small`: 2GB RAM, 1 CPU, 10GB ephemeral — simple deterministic tasks
  - `Medium`: 4GB RAM, 2 CPU, 20GB ephemeral — single-repo agentic tasks
  - `Large`: 8GB RAM, 4 CPU, 50GB ephemeral — multi-repo agentic tasks with large codebases
- Consumer overrides presets as needed; platform validates minimums

**Storage Architecture:**

| Volume Type | Use Case | agentbox Support |
|---|---|---|
| `emptyDir` | Default workspace, agent binary injection | Built-in (current) |
| `VolumeMount` (hostPath/PVC) | Repo cache, shared model weights, persistent scratch | NEW: `[]VolumeMount` on `ProvisionOptions` |
| RWX PVC (Longhorn/NFS) | Multi-agent shared workspace | Consumer provisions PVC; passes as `VolumeMount` with `ReadOnly: false` |

### Observability

**Metrics (configurable prefix, default `agentbox_`):**
- `{prefix}_sandbox_provision_duration_seconds` (histogram)
- `{prefix}_sandbox_active_count` (gauge)
- `{prefix}_agent_phase_transitions_total` (counter, labels: `from_phase`, `to_phase`)
- `{prefix}_agent_steering_iterations_total` (counter)
- `{prefix}_agent_execution_duration_seconds` (histogram)
- Consumer adds domain-specific metrics (e.g. `fleetlift_prs_created_total`)

**OpenTelemetry Tracing (future, v2):**
- Each sandbox lifecycle gets a trace span: provision → manifest → execute → result → cleanup
- Steering iterations are child spans
- Consumer injects trace context via `Env` for end-to-end correlation
- OTel collector config documented, not bundled

### HITL / Autonomy Spectrum

The steering protocol supports the full autonomy spectrum (maps to REACT matrix):

| Level | Steering Config | Behavior |
|---|---|---|
| **Fully supervised** | `RequireApproval: true`, consumer workflow waits for human signal | Agent pauses at `awaiting_input`; human must approve/reject/steer |
| **Conditional autonomy** | Consumer workflow auto-approves unless edge case detected | Automated `SteeringProvider` evaluates result; escalates to human on failure |
| **Monitored autonomy** | `RequireApproval: false`, consumer monitors via status polling | Agent runs to completion; consumer alerts if metrics breach thresholds |
| **Full autonomy** | `RequireApproval: false`, no steering | Fire-and-forget; consumer reads result when done |

Platform provides the **mechanism** (steering protocol). Consumer decides the **policy** (when to use human vs automated steering).

---

## 2. Fleetlift Specification

### Vision

A managed code transformation platform ("managed turbolift") that uses Agentbox to run AI-powered research and refactoring campaigns across repositories. Fleetlift owns the domain model (tasks, repos, PRs, knowledge), orchestration (Temporal workflows), and all integrations (GitHub, Slack, Claude Code).

### What Fleetlift IS (post-split)
- Task model: transform/report modes, agentic/deterministic execution, grouped repos
- Temporal workflows: Transform, TransformV2, TransformGroup
- AI execution: Claude Code CLI integration, prompt engineering, knowledge capture/enrichment
- Git operations: cloning, PR creation, credential management
- Integrations: GitHub API, Slack notifications
- Web UI + CLI
- The fleetlift-specific agent binary (extends agentbox pipeline with clone→transform→verify→collect→PR steps)

### What Fleetlift DELEGATES to Agentbox
- Container provisioning and cleanup
- File-based agent protocol (manifest delivery, status polling, result retrieval, steering)
- Temporal activity helpers for sandbox operations
- Pipeline framework for the agent binary

### Architecture (post-split)

```
fleetlift (Go module, imports agentbox)
├── cmd/
│   ├── agent/          # fleetlift-agent binary (registers steps with agentbox.Pipeline)
│   ├── cli/            # fleetlift CLI
│   ├── server/         # web UI server
│   └── worker/         # Temporal worker
├── internal/
│   ├── activity/       # fleetlift Temporal activities (wraps agentbox activities + domain logic)
│   ├── agent/          # fleetlift agent steps (clone, transform, verify, collect, PR)
│   │   └── fleetproto/ # fleetlift-specific manifest/result types (extends agentbox protocol)
│   ├── client/
│   ├── config/
│   ├── knowledge/
│   ├── model/          # domain model (Task, TaskResult, etc.)
│   ├── server/
│   ├── state/
│   └── workflow/       # Temporal workflows
├── docker/
├── deploy/
├── web/
└── go.mod              # depends on github.com/tinkerloft/agentbox
```

### Gap Analysis: Current Code → Fleetlift (post-split)

| Gap | Current State | Required Change | Effort |
|-----|--------------|-----------------|--------|
| **Manifest building uses `protocol.TaskManifest`** | `activity/manifest.go` builds `protocol.TaskManifest` | Build `fleetproto.TaskManifest` (fleetlift-owned type), marshal to `[]byte` for agentbox | Small |
| **Agent binary runs monolithic pipeline** | `cmd/agent/main.go` → `agent.Pipeline.Run()` | Import agentbox pipeline framework; register fleetlift steps (clone, transform, verify, collect, PR) | Medium |
| **Activities import `sandbox` directly** | `activity/sandbox.go` holds `sandbox.Provider` | Import `agentbox/sandbox`; wrap `agentbox/temporalkit` activities with domain logic (secret injection, etc.) | Small |
| **Workflow uses `protocol.Phase*` constants** | `transform_v2.go` references `protocol.PhaseFailed`, etc. | Import `agentbox/protocol` for generic phases; define fleetlift-specific phases (e.g. `PhaseCreatingPRs`) locally | Small |
| **Agent code mixed generic + domain** | `agent/transform.go` has both env filtering (generic) and Claude CLI invocation (domain) | Keep domain parts in `fleetlift/internal/agent/`; generic helpers come from agentbox | Medium |
| **Metrics reference activity constants** | `metrics/interceptor.go` imports `internal/activity` | Update imports to local activity constants | Trivial |
| **Docker images need rebuild** | `Dockerfile.sandbox` builds fleetlift-agent | Keep fleetlift-specific Dockerfiles; agentbox provides base agent image or none | Small |
| **Knowledge system unchanged** | `internal/knowledge/`, `activity/knowledge.go` | Stays entirely in fleetlift — no agentbox dependency | None |

### Fleetlift-Specific Protocol Types (`fleetproto`)

These types currently live in `internal/agent/protocol/types.go` but are fleetlift-domain:

```go
package fleetproto

import "github.com/tinkerloft/agentbox/protocol"

// Fleetlift task manifest — serialized to JSON, passed as []byte to agentbox
type TaskManifest struct {
    TaskID               string             `json:"task_id"`
    Mode                 string             `json:"mode"`  // "transform" | "report"
    Title                string             `json:"title"`
    Repositories         []ManifestRepo     `json:"repositories"`
    Transformation       *ManifestRepo      `json:"transformation,omitempty"`
    Targets              []ManifestRepo     `json:"targets,omitempty"`
    ForEach              []ForEachTarget    `json:"for_each,omitempty"`
    Execution            ManifestExecution  `json:"execution"`
    Verifiers            []ManifestVerifier `json:"verifiers,omitempty"`
    TimeoutSeconds       int                `json:"timeout_seconds"`
    RequireApproval      bool               `json:"require_approval"`
    MaxSteeringIterations int               `json:"max_steering_iterations"`
    PullRequest          *ManifestPRConfig  `json:"pull_request,omitempty"`
    GitConfig            ManifestGitConfig  `json:"git_config"`
}

// ... ManifestRepo, ManifestExecution, ManifestVerifier, ManifestPRConfig,
//     ManifestGitConfig, ForEachTarget — all moved here from protocol/types.go

// Fleetlift agent result
type AgentResult struct {
    Status           protocol.Phase     `json:"status"`
    Repositories     []RepoResult       `json:"repositories,omitempty"`
    AgentOutput      string             `json:"agent_output,omitempty"`
    SteeringHistory  []SteeringRecord   `json:"steering_history,omitempty"`
}

// ... RepoResult, DiffEntry, VerifierResult, ReportResult, ForEachResult,
//     PRInfo, SteeringRecord — all moved here
```

---

## 3. Split Plan

### Repository Setup

- `github.com/tinkerloft/agentbox` — new repo, already created at `../agentbox` (Go module, v0.x)
- `github.com/tinkerloft/fleetlift` — existing repo at `../fleetlift`, refactored to import agentbox

### Phase 1: Create agentbox repo with generic protocol + sandbox (no agent pipeline yet)

**Goal:** Extract the clean, well-separated parts first. Fleetlift keeps working throughout.

**Steps:**

1. Create `github.com/tinkerloft/agentbox` repo with initial structure:
   ```
   agentbox/
   ├── protocol/          # generic phases, steering, file paths, metadata
   ├── sandbox/            # Provider, AgentProvider interfaces + types
   │   ├── docker/         # Docker implementation (ported from fleetlift)
   │   ├── k8s/            # K8s implementation (ported from fleetlift)
   │   └── opensandbox/    # OpenSandbox adapter (new — Alibaba OSS, K8s+Docker unified API)
   ├── agent/              # Protocol primitives (WaitForManifest, WriteStatus, etc.)
   ├── temporalkit/        # Temporal activity helpers
   └── go.mod
   ```

2. **Move `protocol` (generic subset):**
   - Copy `Phase`, `AgentStatus`, `StatusProgress`, `SteeringAction`, `SteeringInstruction` from `internal/agent/protocol/types.go`
   - Change `BasePath` to `DefaultBasePath = "/workspace/.agentbox"`; make path functions accept a base path parameter
   - Remove: `TaskManifest`, `AgentResult`, `RepoResult`, `DiffEntry`, `VerifierResult`, `ReportResult`, `ForEachTarget`, `ForEachResult`, `PRInfo`, `SteeringRecord`, `ManifestRepo`, `ManifestExecution`, `ManifestVerifier`, `ManifestPRConfig`, `ManifestGitConfig`, `MaxDiffLinesPerFile`

3. **Move `sandbox` package:**
   - Copy `provider.go` → change `AgentProvider.PollStatus` return to `([]byte, error)`
   - Copy `factory.go` unchanged
   - Copy `docker/provider.go` + `docker/register.go` — update `PollStatus` to return raw bytes; replace `protocol.BasePath` etc. with configurable base path
   - Copy `k8s/provider.go`, `k8s/job.go`, `k8s/exec.go`, `k8s/wait.go`, `k8s/register.go` — same changes
   - Add new fields to `ProvisionOptions`: `Cmd`, `Volumes`, `RuntimeClass`, `NodeSelector`, `Tolerations`, `UserNamespace`, `OwnerRef`, `EphemeralStorage`
   - Implement `OwnerRef` in K8s `job.go` (set `ownerReferences` on Job metadata)
   - Implement `UserNamespace` in K8s `job.go` (set `pod.spec.hostUsers: false`)
   - Implement `RuntimeClass` in K8s `job.go` (set `pod.spec.runtimeClassName`)
   - Implement `NodeSelector` + `Tolerations` in K8s `job.go`
   - Rename container prefix from `claude-sandbox-` to `agentbox-`

4. **Copy tests** for all moved code. Adapt imports.

5. **Verify:** `go build ./...` && `go test ./...` in agentbox repo.

**Files created in agentbox:**
| Agentbox path | Source |
|---|---|
| `protocol/protocol.go` | Generic subset of `internal/agent/protocol/types.go` |
| `sandbox/provider.go` | `internal/sandbox/provider.go` (modified) |
| `sandbox/factory.go` | `internal/sandbox/factory.go` |
| `sandbox/docker/provider.go` | `internal/sandbox/docker/provider.go` (modified) |
| `sandbox/docker/register.go` | `internal/sandbox/docker/register.go` |
| `sandbox/k8s/provider.go` | `internal/sandbox/k8s/provider.go` (modified) |
| `sandbox/k8s/job.go` | `internal/sandbox/k8s/job.go` (modified — new K8s security features) |
| `sandbox/k8s/exec.go` | `internal/sandbox/k8s/exec.go` |
| `sandbox/k8s/wait.go` | `internal/sandbox/k8s/wait.go` |
| `sandbox/k8s/register.go` | `internal/sandbox/k8s/register.go` |

### Phase 2: Add agent primitives, temporalkit, and OpenSandbox adapter

**Steps:**

1. **Add `temporalkit` package:**
   - `AgentActivities` — all `[]byte` interfaces (no typed manifests)
   - `SandboxActivities` — `Provision` + `Cleanup` with generic `ProvisionInput`/`SandboxInfo`
   - Port staleness detection, heartbeat logic from current `activity/agent.go`

2. **Add `agent` package (primitives only — no step registration):**
   - Move `deps.go` (FileSystem, CommandExecutor) as-is
   - Move `constants.go` (poll intervals, truncation limits)
   - Create `protocol.go` — the `Protocol` struct with primitives:
     - `WaitForManifest(ctx) (json.RawMessage, error)` — polls for manifest.json
     - `WriteStatus(status AgentStatus)` — atomic write of status.json
     - `WriteResult(result json.RawMessage) error` — atomic write of result.json
     - `WaitForSteering(ctx) (*SteeringInstruction, error)` — polls + atomic consume
   - Consumer builds their own `main()` loop using these primitives

3. **Add `sandbox/opensandbox/` adapter:**
   - Implement `sandbox.AgentProvider` backed by OpenSandbox SDK (Go SDK planned; use REST API initially)
   - OpenSandbox provides unified Docker+K8s runtime — single provider that works in both environments
   - Map `ProvisionOptions` → OpenSandbox sandbox creation API
   - Map file protocol operations → OpenSandbox exec/file APIs
   - Self-registers via `init()` as `"opensandbox"` provider

4. **Verify:** `go build ./...` && `go test ./...`

**Files created in agentbox:**
| Agentbox path | Source |
|---|---|
| `temporalkit/agent_activities.go` | Derived from `internal/activity/agent.go` (generified) |
| `temporalkit/sandbox_activities.go` | Derived from `internal/activity/sandbox.go` (generic subset) |
| `agent/deps.go` | `internal/agent/deps.go` (as-is) |
| `agent/constants.go` | `internal/agent/constants.go` (as-is) |
| `agent/protocol.go` | New — primitives extracted from `internal/agent/pipeline.go` |
| `sandbox/opensandbox/provider.go` | New — OpenSandbox adapter |
| `sandbox/opensandbox/register.go` | New — self-registration |

### Phase 3: Refactor fleetlift to import agentbox

**Goal:** Replace fleetlift's internal sandbox/protocol/agent code with agentbox imports. Keep all tests passing.

**Steps:**

1. **Add `go.mod` dependency:** `require github.com/tinkerloft/agentbox v0.1.0`

2. **Create `internal/agent/fleetproto/` package:**
   - Move fleetlift-specific types from `internal/agent/protocol/types.go`: `TaskManifest`, `AgentResult`, `RepoResult`, `DiffEntry`, `VerifierResult`, `ReportResult`, `ForEachTarget`, `ForEachResult`, `PRInfo`, `SteeringRecord`, `ManifestRepo`, `ManifestExecution`, `ManifestVerifier`, `ManifestPRConfig`, `ManifestGitConfig`, `MaxDiffLinesPerFile`
   - Import `agentbox/protocol` for `Phase`, `AgentStatus`, `SteeringAction`, `SteeringInstruction`

3. **Update `internal/agent/protocol/types.go`:**
   - Delete it (all types now live in either `agentbox/protocol` or `fleetproto`)
   - Or: keep as a thin re-export shim temporarily to reduce diff size, then delete

4. **Delete `internal/sandbox/` entirely:**
   - Replace all imports of `internal/sandbox` → `agentbox/sandbox`
   - Replace `internal/sandbox/docker` → `agentbox/sandbox/docker`
   - Replace `internal/sandbox/k8s` → `agentbox/sandbox/k8s`

5. **Refactor `internal/activity/agent.go`:**
   - Import `agentbox/temporalkit` for base activities
   - Wrap with fleetlift-specific logic (parsing `fleetproto.AgentResult` from `[]byte`, converting `fleetproto.TaskManifest` to `[]byte`)
   - `SubmitTaskManifestInput.Manifest` changes from `protocol.TaskManifest` to `fleetproto.TaskManifest` (marshaled to `[]byte` before calling agentbox)

6. **Refactor `internal/activity/sandbox.go`:**
   - Import `agentbox/temporalkit` for `Provision`/`Cleanup`
   - Keep fleetlift-specific logic: secret injection (`ANTHROPIC_API_KEY`, `GITHUB_TOKEN`), resource parsing, config defaults
   - `CloneRepositories`, `RunVerifiers` stay here — they use `agentbox/sandbox.Provider` for exec but contain fleetlift domain logic

7. **Refactor `internal/activity/manifest.go`:**
   - Change output type from `protocol.TaskManifest` to `fleetproto.TaskManifest`
   - Imports change: `agentbox/protocol` for generic types, `fleetproto` for manifest types

8. **Refactor `internal/workflow/transform_v2.go`:**
   - Import `agentbox/protocol` for `Phase*` constants and `SteeringAction*` constants
   - Import `fleetproto` for `AgentResult` parsing
   - `PhaseCreatingPRs` defined locally (fleetlift-specific phase)

9. **Refactor agent binary (`internal/agent/` + `cmd/agent/`):**
   - Import `agentbox/agent` for `Protocol` primitives and deps interfaces
   - Keep fleetlift step implementations in `internal/agent/`: `clone.go`, `transform.go`, `verify.go`, `collect.go`, `pr.go`, `validate.go`
   - `cmd/agent/main.go` builds its own main loop using agentbox primitives:
     ```
     proto.WaitForManifest → parse fleetproto.TaskManifest
       → clone repos → run transformation → run verifiers → collect results
       → if RequireApproval: proto.WriteStatus(AwaitingInput) → proto.WaitForSteering → handle
       → create PRs → proto.WriteResult
     ```
   - All step functions take `*fleetproto.TaskManifest` (parsed from `json.RawMessage` at entry)

10. **Migrate legacy workflows to agentbox model:**
    - `Transform` (v1): currently exec's Claude Code directly into sandbox via `sandbox.Provider.ExecShell`. Refactor to use sidecar agent pattern (same as TransformV2) — provision with agentbox, submit manifest, poll status, read result
    - `TransformGroup`: refactor child workflow invocations to use TransformV2 path
    - This eliminates the `RunClaudeCode`, `ExecuteDeterministic`, `CloneRepositories` (legacy path), `RunVerifiers` (exec-based) activities that directly exec into sandboxes
    - Activities to remove after migration: `RunClaudeCode`, `ExecuteDeterministic`, `CollectReport` (exec-based), `GetDiff` (exec-based), `GetVerifierOutput` (exec-based)
    - Activities to keep: `CreatePullRequest` (GitHub API, not sandbox exec), `NotifySlack`, `CaptureKnowledge`, `EnrichPrompt`, `ValidateSchema`

11. **Update imports across all remaining files:**
    - `internal/metrics/interceptor.go` — activity constant names stay local
    - `cmd/worker/main.go` — import `agentbox/sandbox/docker`, `agentbox/sandbox/k8s` for registration side effects
    - `internal/logging/` — stays in fleetlift (or duplicated; it's 40 lines)

12. **Update Dockerfiles:**
    - `Dockerfile.agent` — no change (builds `cmd/agent` which now imports agentbox)
    - `Dockerfile.sandbox` — change `FLEETLIFT_BASE_PATH` references if any

13. **Verify:** `make lint` && `go test ./...` && `go build ./...`

### Phase 4: Cleanup + documentation

1. Delete dead code (old `internal/sandbox/`, `internal/agent/protocol/`)
2. Update `CLAUDE.md` project structure section
3. Update `docs/IMPLEMENTATION_PLAN.md`
4. Verify all tests pass, lint clean, builds succeed

### Dependency Direction (enforced)

```
agentbox (no domain knowledge)
    ↑
fleetlift (imports agentbox; adds domain model, workflows, integrations)
```

Agentbox MUST NOT import fleetlift. The dependency is strictly one-way.

### Estimated Effort

| Phase | Scope | Estimate |
|-------|-------|----------|
| Phase 1 | agentbox: protocol + sandbox | ~2 days |
| Phase 2 | agentbox: temporalkit + pipeline framework | ~2 days |
| Phase 3 | fleetlift: refactor to import agentbox | ~3 days |
| Phase 4 | Cleanup + docs | ~1 day |

### Decisions (resolved)

| Question | Decision |
|---|---|
| Module path | `github.com/tinkerloft/agentbox` |
| Versioning | v0.x during initial development |
| Repo strategy | Separate repos (new repo to be created in tinkerloft org) |
| Sandbox providers | Docker + K8s (ported from fleetlift) + **OpenSandbox adapter** (new) |
| Legacy workflows | Migrate `Transform` (v1) and `TransformGroup` to agentbox model |
| Pipeline design | Agentbox provides **primitives only** (manifest polling, status writing, steering loop); consumer builds their own main loop |
| K8s namespace | Same namespace for worker + sandbox Jobs (enables ownerReferences GC) |
