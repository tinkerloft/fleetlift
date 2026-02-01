# Code Transformation Platform - Design

## Overview

A platform for automated code transformations across repositories, supporting:

- **Deployment**: Local (Docker) or production (Kubernetes)
- **Scope**: Single repo or multi-repo
- **Execution**: Deterministic (Docker images) or agentic (AI prompts)

### Vision: Managed Turbolift

Think of this as **managed [Turbolift](https://github.com/Skyscanner/turbolift)** with two execution backends:

| Turbolift | This Platform |
|-----------|---------------|
| CLI on your laptop | Managed service (Temporal + K8s) |
| Script/command | Docker image OR agent prompt |
| Dies if laptop closes | Durable execution, survives failures |
| No approval flow | Human-in-the-loop before PR |
| Stateless | Status tracking, audit trail |
| Single user | Multi-tenant capable |

The platform adds the "managed" layer that Turbolift deliberately doesn't have, while supporting both deterministic transforms (like Turbolift scripts) and agentic transforms (AI-driven code changes).

## Design Principles

1. **Standards over custom** - Use existing open source solutions
2. **Pluggable by default** - Abstract infrastructure behind interfaces
3. **Local-first development** - Everything works on a laptop
4. **Incremental complexity** - Start simple, add features as needed

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Architecture                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   CLI / API                                                                 │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                         Temporal                                     │   │
│   │                                                                      │   │
│   │   - Durable task execution                                          │   │
│   │   - Retry policies                                                   │   │
│   │   - Human-in-the-loop signals                                       │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                      Sandbox Provider                                │   │
│   │                                                                      │   │
│   │   Local:      Docker containers                                     │   │
│   │   Production: Agent Sandbox (K8s SIG) or raw Kubernetes pods        │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                   Transformation Execution                           │   │
│   │                                                                      │   │
│   │   Deterministic: Docker image (OpenRewrite, Scalafix, custom)       │   │
│   │   Agentic:       Claude Code CLI + verifiers via Bash               │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Standards Used

| Concern | Solution | Why |
|---------|----------|-----|
| Workflow Orchestration | [Temporal](https://temporal.io/) | Durable execution, signals/queries, battle-tested |
| Sandbox Lifecycle (K8s) | [Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) | Purpose-built for AI agents, warm pools, gVisor |
| Sandbox Lifecycle (Local) | Docker | Simple, universal |
| Deterministic Transforms | [OpenRewrite](https://docs.openrewrite.org/), [Scalafix](https://scalacenter.github.io/scalafix/) | Mature AST-based tools |
| AI Agent | Claude Code CLI | Agentic loop, context management, tool use |

---

## Core Data Model

### CodeTransform CRD (Kubernetes-Native Interface)

The primary interface for defining transformations is a Kubernetes Custom Resource:

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: upgrade-to-slog
  namespace: transforms
spec:
  # Target repositories
  repositories:
    - url: https://github.com/org/service-a.git
      branch: main
    - url: https://github.com/org/service-b.git

  # Transformation definition (one of):
  transform:
    # Option 1: Deterministic - Docker image execution
    image:
      ref: ghcr.io/moderneinc/mod:latest
      args: ["mod", "run", "--recipe", "UpgradeLog4j"]
      env:
        LOG_LEVEL: info

    # Option 2: Agentic - AI-driven transformation
    # agent:
    #   prompt: |
    #     Migrate from log.Printf to slog package.
    #     Use structured logging with context fields.
    #   verifiers:
    #     - name: build
    #       command: ["go", "build", "./..."]
    #     - name: test
    #       command: ["go", "test", "./..."]

  # Execution settings
  resources:
    limits:
      memory: "4Gi"
      cpu: "2"
  timeout: 30m
  requireApproval: true

  # Sandbox settings
  sandbox:
    namespace: sandbox-isolated      # Where pods run
    runtimeClassName: gvisor         # Optional: enhanced isolation
    nodeSelector:
      workload-type: sandbox

  # PR settings
  pullRequest:
    branchPrefix: "auto/slog-migration"
    title: "Migrate to structured logging (slog)"
    labels: ["automated", "logging"]

status:
  phase: Running  # Pending | Running | AwaitingApproval | Completed | Failed
  workflowID: "transform-xyz123"
  repositories:
    - name: service-a
      status: Completed
      pullRequest:
        url: https://github.com/org/service-a/pull/123
        number: 123
    - name: service-b
      status: Running
  startedAt: "2026-01-31T10:00:00Z"
  completedAt: null
```

### Internal Go Types

The CRD maps to internal Go types used by the Temporal workflow:

```go
type Task struct {
    ID          string             `json:"id"`
    Title       string             `json:"title"`
    Description string             `json:"description,omitempty"`
    Repositories   []RepositoryTarget `json:"repositories"`
    Transformation Transformation     `json:"transformation"`
    Timeout        time.Duration      `json:"timeout,omitempty"`
}

type RepositoryTarget struct {
    URL    string   `json:"url"`              // Git URL
    Branch string   `json:"branch,omitempty"` // Default: "main"
    Setup  []string `json:"setup,omitempty"`  // Commands after clone
}
```

### Transformation

Either deterministic or agentic.

```go
type Transformation struct {
    Deterministic *DeterministicTransform `json:"deterministic,omitempty"`
    Agentic       *AgenticTransform       `json:"agentic,omitempty"`
}

type DeterministicTransform struct {
    Image   string            `json:"image"`
    Command []string          `json:"command,omitempty"`
    Args    []string          `json:"args,omitempty"`
    Env     map[string]string `json:"env,omitempty"`
}

type AgenticTransform struct {
    Prompt    string     `json:"prompt"`
    Verifiers []Verifier `json:"verifiers,omitempty"`
}

type Verifier struct {
    Name    string   `json:"name"`    // e.g., "build", "test"
    Command []string `json:"command"` // e.g., ["go", "test", "./..."]
}
```

### Task Result

```go
type TaskResult struct {
    TaskID       string             `json:"taskId"`
    Status       TaskStatus         `json:"status"`
    Repositories []RepositoryResult `json:"repositories"`
    StartedAt    time.Time          `json:"startedAt"`
    CompletedAt  *time.Time         `json:"completedAt,omitempty"`
    Error        *string            `json:"error,omitempty"`
}

type RepositoryResult struct {
    Repository    string           `json:"repository"`
    Status        string           `json:"status"` // "success" | "failed" | "skipped"
    FilesModified []string         `json:"filesModified,omitempty"`
    PullRequest   *PullRequestInfo `json:"pullRequest,omitempty"`
    Output        string           `json:"output,omitempty"`
    Error         *string          `json:"error,omitempty"`
}

type PullRequestInfo struct {
    URL    string `json:"url"`
    Number int    `json:"number"`
    Branch string `json:"branch"`
}
```

---

## Sandbox Provider Interface

Abstracts container runtime for local and production.

```go
type Provider interface {
    Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)
    Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)
    CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error
    CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)
    Status(ctx context.Context, id string) (*SandboxStatus, error)
    Cleanup(ctx context.Context, id string) error
    Name() string
}

type ProvisionOptions struct {
    TaskID     string
    Image      string
    WorkingDir string
    Env        map[string]string
    Resources  ResourceLimits
    Timeout    time.Duration
    Labels     map[string]string

    // Kubernetes-specific (ignored by Docker)
    Namespace      string
    ServiceAccount string
    NodeSelector   map[string]string
    RuntimeClass   string // gvisor, kata
}

type Sandbox struct {
    ID         string
    Provider   string // "docker" | "kubernetes" | "agent-sandbox"
    WorkingDir string
    Status     SandboxPhase
}

type ExecCommand struct {
    Command    []string
    WorkingDir string
    Env        map[string]string
    Stdin      io.Reader
    Timeout    time.Duration
}

type ExecResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
}
```

### Provider Selection

```go
func NewProvider() (Provider, error) {
    switch os.Getenv("SANDBOX_PROVIDER") {
    case "kubernetes":
        return kubernetes.NewProvider()
    case "agent-sandbox":
        return agentsandbox.NewProvider()
    default:
        return docker.NewProvider()
    }
}
```

---

## Execution Flow

```
1. Task Received
   │
   ▼
2. For each repository:
   │
   ├──▶ 3. Provision Sandbox
   │       │
   │       ▼
   │    4. Clone Repository
   │       │
   │       ▼
   │    5. Run Setup (if defined)
   │       │
   │       ▼
   │    6. Execute Transformation
   │       │
   │       ├── Deterministic: Run Docker image
   │       │
   │       └── Agentic: Run Claude Code
   │           │
   │           ├──▶ Agent makes changes
   │           │       │
   │           │       ▼
   │           │    Agent runs verifiers via Bash
   │           │       │
   │           │       ├── Pass: Continue
   │           │       └── Fail: Agent fixes and retries
   │           │
   │           └── Until: all verifiers pass
   │       │
   │       ▼
   │    7. Create PR
   │       │
   │       ▼
   └──▶ 8. Cleanup Sandbox

9. Report Results
```

---

## Verifiers

Verifiers are commands the agent runs via Bash to validate changes. The orchestrator appends them to the prompt:

```
After making changes, verify your work by running these commands:
- build: go build ./...
- test: go test ./...
- lint: golangci-lint run

Fix any errors before completing the task.
```

The agent runs these directly using its Bash tool, sees output, and iterates until all pass.

The orchestrator also runs verifiers as a **final gate** before creating the PR.

---

## Temporal Workflow

```go
func ExecuteTask(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
    result := &model.TaskResult{
        TaskID:    task.ID,
        Status:    model.TaskStatusRunning,
        StartedAt: workflow.Now(ctx),
    }

    for _, repo := range task.Repositories {
        repoResult := processRepository(ctx, task, repo)
        result.Repositories = append(result.Repositories, repoResult)
    }

    result.Status = determineOverallStatus(result.Repositories)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}

func processRepository(ctx workflow.Context, task model.Task, repo model.RepositoryTarget) model.RepositoryResult {
    // 1. Provision sandbox
    sandbox, err := workflow.ExecuteActivity(ctx, "ProvisionSandbox", task.ID).Get(ctx, nil)
    if err != nil {
        return failedResult(repo, err)
    }
    defer cleanupSandbox(ctx, sandbox)

    // 2. Clone repository
    err = workflow.ExecuteActivity(ctx, "CloneRepository", sandbox, repo).Get(ctx, nil)
    if err != nil {
        return failedResult(repo, err)
    }

    // 3. Run setup
    if len(repo.Setup) > 0 {
        err = workflow.ExecuteActivity(ctx, "RunSetup", sandbox, repo.Setup).Get(ctx, nil)
        if err != nil {
            return failedResult(repo, err)
        }
    }

    // 4. Execute transformation
    var output string
    if task.Transformation.Deterministic != nil {
        output, err = executeDeterministic(ctx, sandbox, task.Transformation.Deterministic)
    } else {
        output, err = executeAgentic(ctx, sandbox, task.Transformation.Agentic)
    }
    if err != nil {
        return failedResult(repo, err)
    }

    // 5. Create PR
    pr, err := workflow.ExecuteActivity(ctx, "CreatePullRequest", sandbox, repo, task).Get(ctx, nil)
    if err != nil {
        return failedResult(repo, err)
    }

    return model.RepositoryResult{
        Repository:  repo.URL,
        Status:      "success",
        Output:      output,
        PullRequest: pr,
    }
}
```

---

## CLI Interface

```bash
# Deterministic transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --image internal/log4j-upgrader:latest \
  --args "--target-version=2.21.1"

# Agentic transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --prompt "Add input validation to all API endpoints" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."

# Multi-repo
orchestrator run \
  --repo https://github.com/org/service-a.git \
  --repo https://github.com/org/service-b.git \
  --prompt "Update deprecated API calls"

# From file
orchestrator run --file task.yaml

# Status
orchestrator status <task-id>
orchestrator list
```

### Task File Format

```yaml
id: upgrade-logging
title: "Upgrade to structured logging"

repositories:
  - url: https://github.com/org/service-a.git
    setup: ["go mod download"]
  - url: https://github.com/org/service-b.git
    setup: ["go mod download"]

transformation:
  agentic:
    prompt: |
      Migrate from log.Printf to slog package.
      - Replace all log.Printf calls with slog equivalents
      - Add context fields where appropriate
      - Ensure log levels are correct

    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 30m
```

---

## Configuration

### Local (Docker)

```yaml
sandbox:
  provider: docker
  image: claude-sandbox:latest
  resources:
    memoryMB: 4096
    cpuCores: 2

temporal:
  address: localhost:7233
  namespace: default
  taskQueue: orchestrator-tasks

github:
  tokenEnvVar: GITHUB_TOKEN

claude:
  apiKeyEnvVar: ANTHROPIC_API_KEY
```

### Production (Kubernetes)

```yaml
sandbox:
  provider: agent-sandbox  # or "kubernetes"
  namespace: orchestrator-sandboxes
  image: 123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox:latest
  warmPool: claude-warm-pool
  runtimeClass: gvisor
  resources:
    memoryMB: 4096
    cpuCores: 2
  nodeSelector:
    workload-type: sandbox

temporal:
  address: temporal.internal:7233
  namespace: orchestrator
  taskQueue: orchestrator-tasks

github:
  tokenSecretRef:
    name: github-credentials
    key: token

claude:
  apiKeySecretRef:
    name: claude-credentials
    key: api-key
```

---

## Kubernetes Production Architecture

### Recommended: Plain Kubernetes Jobs

For most use cases, plain Kubernetes Jobs provide sufficient functionality without additional complexity:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           EKS Cluster                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Control Plane Namespace                           │    │
│  │                                                                      │    │
│  │   ┌─────────────┐    ┌─────────────────────┐    ┌────────────────┐  │    │
│  │   │  Temporal   │    │  Worker Pods        │    │  Controller    │  │    │
│  │   │  Server     │◄──►│  (HPA Scaled)       │    │  (CRD watch)   │  │    │
│  │   │  (or Cloud) │    │                     │    │                │  │    │
│  │   └─────────────┘    └─────────────────────┘    └────────────────┘  │    │
│  │                                                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Sandbox Node Pool                                 │    │
│  │                                                                      │    │
│  │   ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐       │    │
│  │   │ Job/Pod   │  │ Job/Pod   │  │ Job/Pod   │  │ Job/Pod   │       │    │
│  │   │ (task-1)  │  │ (task-2)  │  │ (task-3)  │  │ (task-4)  │       │    │
│  │   └───────────┘  └───────────┘  └───────────┘  └───────────┘       │    │
│  │                                                                      │    │
│  │   Labels: node-type=sandbox, spot=true                              │    │
│  │   RuntimeClass: gvisor (optional)                                   │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Job-Based Sandbox Execution

The Temporal worker creates a Kubernetes Job for each transformation:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: transform-task-abc123
  namespace: sandbox-isolated
  labels:
    codetransform.io/task-id: abc123
    codetransform.io/type: agentic
spec:
  ttlSecondsAfterFinished: 3600
  backoffLimit: 0  # No retries - Temporal handles retry logic
  template:
    spec:
      runtimeClassName: gvisor  # Optional: kernel-level isolation
      serviceAccountName: sandbox-runner
      nodeSelector:
        workload-type: sandbox
      containers:
      - name: sandbox
        image: your-org/claude-sandbox:latest
        resources:
          limits:
            memory: "4Gi"
            cpu: "2"
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: claude-credentials
              key: api-key
      restartPolicy: Never
```

The worker then:
1. Creates the Job via client-go
2. Waits for the pod to be Running
3. Execs commands into it (clone, transform, verify)
4. Deletes the Job when complete (or lets TTL clean it up)

### Optional: Agent Sandbox Integration

For advanced use cases requiring warm pools or faster provisioning, [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) can be used:

```yaml
apiVersion: agents.x-k8s.io/v1alpha1
kind: SandboxTemplate
metadata:
  name: go-standard
spec:
  runtimeClassName: gvisor
  containers:
    - name: sandbox
      image: claude-sandbox-go:1.22
      resources:
        limits:
          memory: 4Gi
          cpu: "2"
---
apiVersion: agents.x-k8s.io/v1alpha1
kind: SandboxWarmPool
metadata:
  name: go-warm-pool
spec:
  templateRef:
    name: go-standard
  minSize: 2
  maxSize: 10
```

**When to use Agent Sandbox:**
- You need sub-second sandbox provisioning (warm pools)
- You want standardized sandbox lifecycle management across multiple platforms
- You have complex multi-container sandbox requirements

**When plain Jobs are sufficient:**
- Transformation tasks take minutes (cold start overhead is negligible)
- You don't need warm pools
- You want simpler operational overhead

### Security

- **RBAC**: Workers can create Jobs and exec into sandbox pods; sandboxes have no K8s API access
- **Network Policies**: Sandboxes allow egress to GitHub, package registries (npm, PyPI, Maven Central), and AI APIs
- **gVisor/Kata**: Optional kernel-level isolation for defense-in-depth
- **IRSA**: IAM roles for service accounts (ECR pull, secrets access)
- **Namespace Isolation**: Each team/tenant can have dedicated sandbox namespace with ResourceQuotas

### Network Policy Example

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-egress
  namespace: sandbox-isolated
spec:
  podSelector:
    matchLabels:
      codetransform.io/sandbox: "true"
  policyTypes:
  - Egress
  egress:
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: TCP
      port: 443  # HTTPS only
```

### Scaling

- **Workers**: HPA based on Temporal task queue depth
- **Sandboxes**: Cluster Autoscaler scales node pool based on pending pods
- **Spot Instances**: Cost-effective for ephemeral sandbox workloads

---

## Directory Structure

```
orchestrator/
├── cmd/
│   ├── worker/              # Temporal worker
│   │   └── main.go
│   └── cli/                 # CLI tool
│       └── main.go
├── internal/
│   ├── model/               # Data models
│   │   ├── task.go
│   │   └── result.go
│   ├── workflow/            # Temporal workflows
│   │   ├── execute.go
│   │   └── execute_test.go
│   ├── activity/            # Temporal activities
│   │   ├── sandbox.go
│   │   ├── transform.go
│   │   ├── git.go
│   │   └── github.go
│   ├── sandbox/             # Sandbox abstraction
│   │   ├── provider.go      # Interface
│   │   ├── docker/          # Docker implementation
│   │   ├── kubernetes/      # K8s implementation
│   │   └── agentsandbox/    # Agent Sandbox implementation
│   └── config/              # Configuration loading
├── config/
│   ├── local.yaml
│   └── production.yaml
└── docker/
    └── Dockerfile.sandbox
```

---

## What We Build vs. What We Adopt

| Build (Custom) | Adopt (Standard) |
|----------------|------------------|
| `CodeTransform` CRD and controller | Temporal (workflow orchestration) |
| Task data model | Kubernetes Jobs (sandbox execution) |
| Workflow logic | Claude Code (agentic execution) |
| CLI interface | OpenRewrite/Scalafix (deterministic transforms) |
| GitHub PR integration | Docker/containerd (container runtime) |
| Verifier prompt generation | gVisor/Kata (optional isolation) |
| Configuration loading | Agent Sandbox (optional, for warm pools) |
| Status synchronization (CRD ↔ Temporal) | |

**Principle**: Build the orchestration glue, adopt standards for infrastructure.

### Key Architectural Decisions

1. **CRD as primary interface**: Users define transforms as Kubernetes resources (`kubectl apply -f transform.yaml`), enabling GitOps workflows and K8s-native tooling.

2. **Temporal for durability**: The CRD controller triggers Temporal workflows. Temporal handles retries, timeouts, and human-in-the-loop signals. Status is synced back to the CRD.

3. **Jobs over Agent Sandbox**: Plain Kubernetes Jobs are simpler and sufficient for most use cases. Agent Sandbox is an optional optimization for warm pools.

4. **Two transform modes**: Deterministic (Docker images like OpenRewrite) and Agentic (Claude Code with prompts) share the same orchestration infrastructure but have different execution paths.
