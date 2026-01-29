# EKS Scalable Workers and Sandboxes Design

## Executive Summary

This document outlines the architecture for scaling the Claude Code Orchestrator to run on Amazon EKS (Elastic Kubernetes Service). The design uses a **hybrid approach**: Temporal handles workflow orchestration while a lightweight Kubernetes operator manages sandbox pod lifecycle through Custom Resource Definitions (CRDs).

**Key Design Principles:**
- Separation of concerns between workflow logic and infrastructure management
- Pre-registered repositories with security controls
- Reusable sandbox profiles for different technology stacks
- Multi-tenant support via Kubernetes namespaces
- GitOps-friendly declarative configuration

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           EKS Cluster                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Control Plane Namespace                           │    │
│  │                                                                      │    │
│  │   ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐    │    │
│  │   │  Temporal   │    │  Sandbox    │    │  Worker Pods        │    │    │
│  │   │  Server     │◄──►│  Operator   │◄──►│  (HPA Scaled)       │    │    │
│  │   │  (or Cloud) │    │             │    │                     │    │    │
│  │   └─────────────┘    └─────────────┘    └─────────────────────┘    │    │
│  │                             │                     │                 │    │
│  │                             │ watches/manages     │ creates CRs     │    │
│  │                             ▼                     ▼                 │    │
│  │   ┌─────────────────────────────────────────────────────────────┐  │    │
│  │   │              Custom Resource Definitions                     │  │    │
│  │   │  ┌─────────────────┐ ┌────────────┐ ┌────────────────────┐ │  │    │
│  │   │  │ SandboxProfile  │ │ Repository │ │    BugFixTask      │ │  │    │
│  │   │  │ (cluster-scoped)│ │ (namespaced)│ │    (namespaced)    │ │  │    │
│  │   │  └─────────────────┘ └────────────┘ └────────────────────┘ │  │    │
│  │   └─────────────────────────────────────────────────────────────┘  │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Team Namespaces                                   │    │
│  │                                                                      │    │
│  │   team-payments/              team-platform/           team-ml/      │    │
│  │   ├── Repository CRs          ├── Repository CRs       ├── Repo CRs │    │
│  │   ├── BugFixTask CRs          ├── BugFixTask CRs       ├── Tasks    │    │
│  │   └── Sandbox Pods            └── Sandbox Pods         └── Pods     │    │
│  │                                                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Sandbox Node Pool                                 │    │
│  │                                                                      │    │
│  │   ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐       │    │
│  │   │ Sandbox   │  │ Sandbox   │  │ Sandbox   │  │ Sandbox   │       │    │
│  │   │ Pod       │  │ Pod       │  │ Pod       │  │ Pod       │       │    │
│  │   │ (task-1)  │  │ (task-2)  │  │ (task-3)  │  │ (task-4)  │       │    │
│  │   └───────────┘  └───────────┘  └───────────┘  └───────────┘       │    │
│  │                                                                      │    │
│  │   Node labels: node-type=sandbox, spot=true                         │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Component Responsibilities

### Temporal (Workflow Orchestration)

Temporal remains the workflow engine, responsible for:

| Responsibility | Description |
|----------------|-------------|
| **Workflow State** | Durable execution of BugFix workflow steps |
| **Human-in-the-Loop** | Signal handling for approve/reject/cancel |
| **Retry Logic** | Configurable retry policies with backoff |
| **Visibility** | Query handlers for status, Temporal UI for debugging |
| **Timeouts** | Activity and workflow-level timeout enforcement |
| **Scheduling** | Task queue management and worker distribution |

### Kubernetes Operator (Infrastructure Management)

The operator manages sandbox pod lifecycle:

| Responsibility | Description |
|----------------|-------------|
| **Pod Provisioning** | Create sandbox pods from BugFixTask CRs |
| **Configuration Injection** | Apply SandboxProfile settings, inject secrets |
| **Resource Enforcement** | Apply CPU/memory limits, node selectors |
| **Lifecycle Management** | Handle pod phases, cleanup on completion |
| **Garbage Collection** | Finalizers ensure cleanup even on failures |
| **Validation** | Webhook validates repo access, requester permissions |

### Temporal Workers (Activity Execution)

Workers execute activities within the Temporal framework:

| Responsibility | Description |
|----------------|-------------|
| **CR Creation** | Create BugFixTask CRs to trigger operator |
| **Pod Monitoring** | Watch for sandbox pod ready state |
| **Claude Execution** | Exec into sandbox pods to run Claude Code |
| **Output Capture** | Stream and capture Claude output |
| **PR Creation** | Create pull requests via GitHub API |
| **Notifications** | Send Slack notifications on state changes |

---

## Custom Resource Definitions

### CRD Hierarchy

```
┌─────────────────────────────────────────────────────────────────┐
│                      CRD Hierarchy                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────┐                                           │
│  │ SandboxProfile   │  "How to run sandboxes"                   │
│  │ (cluster-scoped) │  - Base image, resources, tools           │
│  └────────┬─────────┘                                           │
│           │ references                                          │
│           ▼                                                     │
│  ┌──────────────────┐                                           │
│  │ Repository       │  "What repos are allowed"                 │
│  │ (namespace-scoped)│  - URL, auth, profile, permissions       │
│  └────────┬─────────┘                                           │
│           │ references                                          │
│           ▼                                                     │
│  ┌──────────────────┐                                           │
│  │ BugFixTask       │  "What to fix" (runtime)                  │
│  │ (namespace-scoped)│  - Repo ref, prompt, requester           │
│  └──────────────────┘                                           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

### SandboxProfile (Cluster-Scoped)

Defines reusable sandbox environments for different technology stacks.

```yaml
apiVersion: claude.example.com/v1
kind: SandboxProfile
metadata:
  name: nodejs-typescript
spec:
  # Container image with required tools
  baseImage: "123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox-node:20-lts"

  # Resource allocation
  resources:
    requests:
      memory: "2Gi"
      cpu: "1"
    limits:
      memory: "4Gi"
      cpu: "2"

  # Maximum execution time
  timeout: 30m

  # Tools available to Claude Code
  allowedTools:
    - Read
    - Write
    - Edit
    - Bash
    - Glob
    - Grep

  # Node scheduling
  nodeSelector:
    node-type: sandbox
  tolerations:
    - key: "sandbox"
      operator: "Equal"
      value: "true"
      effect: "NoSchedule"

  # Setup script executed before Claude Code runs
  setupScript: |
    #!/bin/bash
    set -e
    npm install
    npm run build || true

  # Validation script executed after Claude Code completes
  validationScript: |
    #!/bin/bash
    set -e
    npm run lint
    npm test

  # Security context
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
    capabilities:
      drop: ["ALL"]
    seccompProfile:
      type: RuntimeDefault
```

**Additional Profile Examples:**

```yaml
---
apiVersion: claude.example.com/v1
kind: SandboxProfile
metadata:
  name: python-standard
spec:
  baseImage: "123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox-python:3.12"
  resources:
    limits:
      memory: "4Gi"
      cpu: "2"
  timeout: 30m
  setupScript: |
    pip install -r requirements.txt
  validationScript: |
    pytest
    ruff check .
---
apiVersion: claude.example.com/v1
kind: SandboxProfile
metadata:
  name: python-ml-gpu
spec:
  baseImage: "123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox-python:3.12-cuda"
  resources:
    limits:
      memory: "32Gi"
      cpu: "8"
      nvidia.com/gpu: "1"
  timeout: 60m
  nodeSelector:
    node-type: gpu
    nvidia.com/gpu.product: "NVIDIA-A10G"
  setupScript: |
    pip install -r requirements.txt
    python -c "import torch; print(torch.cuda.is_available())"
---
apiVersion: claude.example.com/v1
kind: SandboxProfile
metadata:
  name: go-standard
spec:
  baseImage: "123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox-go:1.22"
  resources:
    limits:
      memory: "4Gi"
      cpu: "2"
  timeout: 30m
  setupScript: |
    go mod download
    go build ./...
  validationScript: |
    go test ./...
    golangci-lint run
---
apiVersion: claude.example.com/v1
kind: SandboxProfile
metadata:
  name: java-gradle
spec:
  baseImage: "123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox-java:21"
  resources:
    limits:
      memory: "8Gi"
      cpu: "4"
  timeout: 45m
  setupScript: |
    ./gradlew build -x test
  validationScript: |
    ./gradlew test
    ./gradlew spotlessCheck
```

---

### Repository (Namespace-Scoped)

Registers repositories with their configuration and access controls.

```yaml
apiVersion: claude.example.com/v1
kind: Repository
metadata:
  name: payments-service
  namespace: team-payments
  labels:
    team: payments
    language: typescript
spec:
  # Git source configuration
  git:
    url: "https://github.com/acme-corp/payments-service.git"
    defaultBranch: "main"

    # Authentication reference
    authSecretRef:
      name: github-token
      key: token

    # Optional: specific clone depth
    depth: 1

  # Which sandbox profile to use
  sandboxProfileRef:
    name: nodejs-typescript

  # Access control: who can trigger bug fixes
  accessControl:
    allowedRequesters:
      # Slack user groups
      - type: slackGroup
        id: "S12345678"
        name: "payments-oncall"
      # Individual users
      - type: slackUser
        id: "U87654321"
        name: "jane.doe"
      # Service accounts
      - type: serviceAccount
        name: "oncall-bot"
      # GitHub teams
      - type: githubTeam
        org: "acme-corp"
        team: "payments-team"

    # Approval requirements
    approval:
      required: true
      minApprovers: 1
      approverGroups:
        - "@acme-corp/payments-reviewers"
      timeout: 24h

  # Pull request configuration
  pullRequest:
    branchPrefix: "claude-fix/"
    titlePrefix: "[Claude] "
    reviewers:
      teams:
        - "@acme-corp/payments-reviewers"
      users: []
    labels:
      - "automated"
      - "claude-code"
      - "needs-review"
    draft: false

  # Optional: repository-specific overrides
  overrides:
    # Override setup script for this repo
    setupScript: |
      npm install
      npm run db:migrate:test

    # Additional environment variables
    env:
      - name: DATABASE_URL
        value: "postgres://localhost:5432/test"
      - name: REDIS_URL
        value: "redis://localhost:6379"

    # Override resource limits
    resources:
      limits:
        memory: "6Gi"

  # Notification settings
  notifications:
    slack:
      channel: "#payments-bugs"
      onStart: true
      onComplete: true
      onFailure: true
      onApprovalNeeded: true

status:
  # Operator-managed status
  lastValidated: "2024-01-15T10:00:00Z"
  validationStatus: Valid
  lastSuccessfulTask: "fix-null-pointer-abc123"
  totalTasksCompleted: 42
```

---

### BugFixTask (Namespace-Scoped, Runtime)

Created at runtime to trigger a bug fix. This is the primary interface between Temporal and the operator.

```yaml
apiVersion: claude.example.com/v1
kind: BugFixTask
metadata:
  name: fix-null-pointer-abc123
  namespace: team-payments
  labels:
    task-id: abc123
    repository: payments-service
  annotations:
    temporal.io/workflow-id: "bugfix-abc123"
    temporal.io/run-id: "run-xyz789"
spec:
  # Reference to pre-registered repository (NOT a URL)
  repositoryRef:
    name: payments-service

  # Optional: override branch for this task
  branch: "main"

  # Task description
  task:
    title: "Fix null pointer in checkout flow"
    description: |
      Users are seeing null pointer exceptions when checking out with
      an expired payment method.

      Error: TypeError: Cannot read property 'id' of null
      File: src/checkout/handler.ts:142

      Stack trace:
      - handler.ts:142 processPayment
      - handler.ts:89 handleCheckout
      - router.ts:45 POST /checkout

    # Optional: AGENTS.md content to guide Claude
    agentInstructions: |
      Focus on defensive null checks. Do not modify the database schema.
      Run tests before committing.

  # External ticket reference
  ticketRef:
    type: jira
    id: "PAY-1234"
    url: "https://acme.atlassian.net/browse/PAY-1234"

  # Who requested this fix
  requester:
    type: slackUser
    id: "U12345678"
    name: "jane.doe"
    timestamp: "2024-01-15T09:30:00Z"

  # Execution settings (can override repository defaults)
  execution:
    timeout: 30m
    approval:
      required: true
      timeout: 24h

status:
  # Current phase
  phase: Running

  # Phase history
  conditions:
    - type: Provisioned
      status: "True"
      lastTransitionTime: "2024-01-15T10:00:00Z"
      reason: PodCreated
      message: "Sandbox pod created successfully"
    - type: CloneComplete
      status: "True"
      lastTransitionTime: "2024-01-15T10:00:30Z"
      reason: RepositoryCloned
      message: "Repository cloned to /workspace/payments-service"
    - type: ClaudeRunning
      status: "True"
      lastTransitionTime: "2024-01-15T10:01:00Z"
      reason: ExecutionStarted
      message: "Claude Code execution in progress"

  # Sandbox pod reference
  sandbox:
    podName: sandbox-fix-null-pointer-abc123-7f8d9
    nodeName: ip-10-0-1-42.ec2.internal
    startTime: "2024-01-15T10:00:00Z"

  # Claude execution results
  claudeResult:
    success: true
    output: |
      I found the issue in src/checkout/handler.ts:142. The payment method
      object can be null when the user's saved payment method expires...
    filesModified:
      - src/checkout/handler.ts
      - src/checkout/handler.test.ts
    tokensUsed: 15234

  # Pull request (when created)
  pullRequest:
    url: "https://github.com/acme-corp/payments-service/pull/456"
    number: 456
    branch: "claude-fix/abc123"
    state: open

  # Timing
  timing:
    createdAt: "2024-01-15T10:00:00Z"
    provisionedAt: "2024-01-15T10:00:15Z"
    claudeStartedAt: "2024-01-15T10:01:00Z"
    claudeCompletedAt: "2024-01-15T10:05:30Z"
    completedAt: null
    durationSeconds: null
```

---

### Multi-Repository Tasks

For tasks spanning multiple repositories:

```yaml
apiVersion: claude.example.com/v1
kind: BugFixTask
metadata:
  name: fix-api-contract-xyz789
  namespace: team-platform
spec:
  # Multiple repository references
  repositories:
    - ref:
        name: payments-service
        namespace: team-payments
      role: primary        # Where the main fix happens
      branch: "main"

    - ref:
        name: api-contracts
        namespace: team-platform
      role: reference      # Read-only context
      branch: "main"

    - ref:
        name: shared-types
        namespace: team-platform
      role: secondary      # May also need changes
      branch: "main"

  task:
    title: "Update PaymentResponse type with refundId field"
    description: |
      Add new 'refundId' field to PaymentResponse type.
      This requires changes to:
      1. api-contracts (type definition)
      2. payments-service (implementation)
      3. shared-types (if applicable)

    agentInstructions: |
      1. First, examine the current PaymentResponse type in api-contracts
      2. Update the type definition
      3. Implement the change in payments-service
      4. Ensure all tests pass in both repos

status:
  phase: Running
  repositories:
    - name: payments-service
      cloned: true
      path: /workspace/payments-service
      hasChanges: true
    - name: api-contracts
      cloned: true
      path: /workspace/api-contracts
      hasChanges: true
    - name: shared-types
      cloned: true
      path: /workspace/shared-types
      hasChanges: false

  pullRequests:
    - repository: payments-service
      url: "https://github.com/acme-corp/payments-service/pull/456"
      number: 456
    - repository: api-contracts
      url: "https://github.com/acme-corp/api-contracts/pull/123"
      number: 123
```

---

## Operator Design

### Controller Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Sandbox Operator                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Controller Manager                                │    │
│  │                                                                      │    │
│  │   ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐    │    │
│  │   │ SandboxProfile  │  │   Repository    │  │   BugFixTask    │    │    │
│  │   │   Controller    │  │   Controller    │  │   Controller    │    │    │
│  │   │                 │  │                 │  │                 │    │    │
│  │   │ - Validate      │  │ - Validate URL  │  │ - Create pods   │    │    │
│  │   │ - Update status │  │ - Check auth    │  │ - Monitor exec  │    │    │
│  │   │                 │  │ - Update status │  │ - Cleanup       │    │    │
│  │   └─────────────────┘  └─────────────────┘  └─────────────────┘    │    │
│  │                                                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Webhook Server                                    │    │
│  │                                                                      │    │
│  │   ┌─────────────────────────────────────────────────────────────┐   │    │
│  │   │              Validating Webhooks                             │   │    │
│  │   │                                                              │   │    │
│  │   │  - BugFixTask: Validate requester permissions                │   │    │
│  │   │  - BugFixTask: Validate repository reference exists          │   │    │
│  │   │  - Repository: Validate SandboxProfile reference             │   │    │
│  │   │  - Repository: Validate git URL format                       │   │    │
│  │   │                                                              │   │    │
│  │   └─────────────────────────────────────────────────────────────┘   │    │
│  │                                                                      │    │
│  │   ┌─────────────────────────────────────────────────────────────┐   │    │
│  │   │              Mutating Webhooks                               │   │    │
│  │   │                                                              │   │    │
│  │   │  - BugFixTask: Inject default timeout from Repository        │   │    │
│  │   │  - BugFixTask: Add finalizer for cleanup                     │   │    │
│  │   │  - BugFixTask: Set owner references                          │   │    │
│  │   │                                                              │   │    │
│  │   └─────────────────────────────────────────────────────────────┘   │    │
│  │                                                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### BugFixTask Controller Reconciliation Loop

```go
func (r *BugFixTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    task := &claudev1.BugFixTask{}
    if err := r.Get(ctx, req.NamespacedName, task); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Handle deletion
    if !task.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, task)
    }

    // State machine
    switch task.Status.Phase {
    case "":
        return r.initializeTask(ctx, task)
    case PhasePending:
        return r.provisionSandbox(ctx, task)
    case PhaseProvisioning:
        return r.waitForPodReady(ctx, task)
    case PhaseRunning:
        return r.monitorExecution(ctx, task)
    case PhaseAwaitingApproval:
        return r.waitForApproval(ctx, task)
    case PhaseCreatingPR:
        return r.monitorPRCreation(ctx, task)
    case PhaseCompleted, PhaseFailed:
        return r.cleanup(ctx, task)
    }

    return ctrl.Result{}, nil
}
```

### Sandbox Pod Template

The operator creates pods based on this template:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: sandbox-${TASK_ID}-${RANDOM}
  namespace: ${NAMESPACE}
  labels:
    app: claude-sandbox
    task-id: ${TASK_ID}
    repository: ${REPO_NAME}
  annotations:
    cluster-autoscaler.kubernetes.io/safe-to-evict: "false"
  ownerReferences:
    - apiVersion: claude.example.com/v1
      kind: BugFixTask
      name: ${TASK_NAME}
      uid: ${TASK_UID}
      controller: true
  finalizers:
    - claude.example.com/sandbox-cleanup
spec:
  restartPolicy: Never
  serviceAccountName: claude-sandbox

  nodeSelector:
    node-type: sandbox

  tolerations:
    - key: "sandbox"
      operator: "Equal"
      value: "true"
      effect: "NoSchedule"

  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault

  containers:
    - name: sandbox
      image: ${SANDBOX_IMAGE}
      command: ["tail", "-f", "/dev/null"]

      resources:
        requests:
          memory: ${MEMORY_REQUEST}
          cpu: ${CPU_REQUEST}
        limits:
          memory: ${MEMORY_LIMIT}
          cpu: ${CPU_LIMIT}

      env:
        - name: TASK_ID
          value: ${TASK_ID}
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: claude-api-keys
              key: anthropic-api-key
        - name: GITHUB_TOKEN
          valueFrom:
            secretKeyRef:
              name: ${GITHUB_SECRET_NAME}
              key: ${GITHUB_SECRET_KEY}

      volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: output
          mountPath: /output
        - name: agents-config
          mountPath: /workspace/AGENTS.md
          subPath: AGENTS.md

      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
        readOnlyRootFilesystem: false

  volumes:
    - name: workspace
      emptyDir:
        sizeLimit: 10Gi
    - name: output
      emptyDir:
        sizeLimit: 1Gi
    - name: agents-config
      configMap:
        name: agents-config-${TASK_ID}

  # Timeout enforcement
  activeDeadlineSeconds: ${TIMEOUT_SECONDS}
```

---

## Temporal Integration

### Updated Workflow

The BugFix workflow is updated to work with Kubernetes:

```go
func BugFix(ctx workflow.Context, task model.BugFixTask) (*model.BugFixResult, error) {
    // ... signal handlers, query handlers ...

    // 1. Create BugFixTask CR (triggers operator)
    var taskCR *model.BugFixTaskCR
    err := workflow.ExecuteActivity(ctx, "CreateBugFixTaskCR", task).Get(ctx, &taskCR)
    if err != nil {
        return failedResult(task.TaskID, err), nil
    }

    // 2. Wait for sandbox pod to be ready
    var sandbox *model.SandboxInfo
    err = workflow.ExecuteActivity(ctx, "WaitForSandboxReady", taskCR.Name, taskCR.Namespace).Get(ctx, &sandbox)
    if err != nil {
        return failedResult(task.TaskID, err), nil
    }

    // 3. Clone repositories (via kubectl exec)
    err = workflow.ExecuteActivity(ctx, "CloneRepositories", sandbox, task.Repositories).Get(ctx, nil)
    if err != nil {
        return failedResult(task.TaskID, err), nil
    }

    // 4. Run Claude Code (via kubectl exec)
    var claudeResult *model.ClaudeCodeResult
    err = workflow.ExecuteActivity(ctx, "RunClaudeCode", sandbox, buildPrompt(task)).Get(ctx, &claudeResult)
    if err != nil {
        return failedResult(task.TaskID, err), nil
    }

    // 5. Wait for approval if required
    if task.RequireApproval && claudeResult.Success {
        // ... approval logic (unchanged) ...
    }

    // 6. Create pull requests
    // ... PR creation logic (unchanged) ...

    // 7. Update BugFixTask CR status
    err = workflow.ExecuteActivity(ctx, "UpdateBugFixTaskStatus", taskCR, model.PhaseCompleted).Get(ctx, nil)

    return &model.BugFixResult{...}, nil
}
```

### Updated Activities

```go
// internal/activity/kubernetes.go

type KubernetesActivities struct {
    clientset kubernetes.Interface
    dynamic   dynamic.Interface
}

func (a *KubernetesActivities) CreateBugFixTaskCR(ctx context.Context, task model.BugFixTask) (*model.BugFixTaskCR, error) {
    // Create the BugFixTask CR which triggers the operator
    taskCR := &unstructured.Unstructured{
        Object: map[string]interface{}{
            "apiVersion": "claude.example.com/v1",
            "kind":       "BugFixTask",
            "metadata": map[string]interface{}{
                "name":      fmt.Sprintf("fix-%s", task.TaskID),
                "namespace": task.Namespace,
            },
            "spec": map[string]interface{}{
                "repositoryRef": map[string]interface{}{
                    "name": task.RepositoryName,
                },
                "task": map[string]interface{}{
                    "title":       task.Title,
                    "description": task.Description,
                },
                // ... other fields
            },
        },
    }

    result, err := a.dynamic.Resource(bugFixTaskGVR).Namespace(task.Namespace).
        Create(ctx, taskCR, metav1.CreateOptions{})
    if err != nil {
        return nil, err
    }

    return &model.BugFixTaskCR{
        Name:      result.GetName(),
        Namespace: result.GetNamespace(),
    }, nil
}

func (a *KubernetesActivities) WaitForSandboxReady(ctx context.Context, name, namespace string) (*model.SandboxInfo, error) {
    // Watch for pod to become ready
    watcher, err := a.clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
        LabelSelector: fmt.Sprintf("task-id=%s", name),
    })
    if err != nil {
        return nil, err
    }
    defer watcher.Stop()

    for event := range watcher.ResultChan() {
        pod := event.Object.(*corev1.Pod)
        if pod.Status.Phase == corev1.PodRunning {
            return &model.SandboxInfo{
                PodName:   pod.Name,
                Namespace: pod.Namespace,
                NodeName:  pod.Spec.NodeName,
            }, nil
        }
    }

    return nil, fmt.Errorf("timeout waiting for sandbox pod")
}

func (a *KubernetesActivities) ExecInSandbox(ctx context.Context, sandbox model.SandboxInfo, command string) (*model.ExecResult, error) {
    // Execute command in sandbox pod via kubernetes exec API
    req := a.clientset.CoreV1().RESTClient().Post().
        Resource("pods").
        Name(sandbox.PodName).
        Namespace(sandbox.Namespace).
        SubResource("exec").
        VersionedParams(&corev1.PodExecOptions{
            Container: "sandbox",
            Command:   []string{"bash", "-c", command},
            Stdout:    true,
            Stderr:    true,
        }, scheme.ParameterCodec)

    exec, err := remotecommand.NewSPDYExecutor(a.config, "POST", req.URL())
    if err != nil {
        return nil, err
    }

    var stdout, stderr bytes.Buffer
    err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
        Stdout: &stdout,
        Stderr: &stderr,
    })

    return &model.ExecResult{
        Stdout:   stdout.String(),
        Stderr:   stderr.String(),
        ExitCode: getExitCode(err),
    }, nil
}
```

---

## Security Model

### RBAC Configuration

```yaml
---
# Worker ServiceAccount
apiVersion: v1
kind: ServiceAccount
metadata:
  name: claude-worker
  namespace: claude-system
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/claude-worker-role
---
# Worker Role - manages CRs and pods
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: claude-worker-role
rules:
  # Manage BugFixTask CRs
  - apiGroups: ["claude.example.com"]
    resources: ["bugfixtasks"]
    verbs: ["create", "get", "list", "watch", "update", "patch"]
  - apiGroups: ["claude.example.com"]
    resources: ["bugfixtasks/status"]
    verbs: ["get", "update", "patch"]

  # Read Repository and SandboxProfile CRs
  - apiGroups: ["claude.example.com"]
    resources: ["repositories", "sandboxprofiles"]
    verbs: ["get", "list", "watch"]

  # Execute in sandbox pods
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]

  # Read secrets for API keys
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["claude-api-keys"]
---
# Operator ServiceAccount
apiVersion: v1
kind: ServiceAccount
metadata:
  name: claude-operator
  namespace: claude-system
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/claude-operator-role
---
# Operator Role - full control over sandbox pods
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: claude-operator-role
rules:
  # Full control over all Claude CRDs
  - apiGroups: ["claude.example.com"]
    resources: ["*"]
    verbs: ["*"]

  # Manage sandbox pods
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["create", "get", "list", "watch", "delete"]

  # Manage ConfigMaps for AGENTS.md
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "get", "delete"]

  # Read secrets
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]

  # Events for status reporting
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
---
# Sandbox ServiceAccount (minimal permissions)
apiVersion: v1
kind: ServiceAccount
metadata:
  name: claude-sandbox
  namespace: ${TEAM_NAMESPACE}
# No RBAC bindings - sandbox pods have no K8s API access
```

### Network Policies

```yaml
---
# Sandbox pods: outbound HTTPS only
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-network-policy
  namespace: ${TEAM_NAMESPACE}
spec:
  podSelector:
    matchLabels:
      app: claude-sandbox
  policyTypes:
    - Ingress
    - Egress
  ingress: []  # No inbound traffic
  egress:
    # GitHub API and git
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - port: 443
          protocol: TCP
        - port: 22
          protocol: TCP
    # DNS
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - port: 53
          protocol: UDP
---
# Worker pods: cluster internal + external APIs
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: worker-network-policy
  namespace: claude-system
spec:
  podSelector:
    matchLabels:
      app: claude-worker
  policyTypes:
    - Ingress
    - Egress
  ingress: []
  egress:
    # Temporal server
    - to:
        - podSelector:
            matchLabels:
              app: temporal
      ports:
        - port: 7233
          protocol: TCP
    # Kubernetes API
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - port: 443
          protocol: TCP
    # DNS
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - port: 53
          protocol: UDP
```

### AWS IAM (IRSA)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ECRPull",
      "Effect": "Allow",
      "Action": [
        "ecr:GetDownloadUrlForLayer",
        "ecr:BatchGetImage",
        "ecr:BatchCheckLayerAvailability"
      ],
      "Resource": [
        "arn:aws:ecr:us-west-2:123456789:repository/claude-sandbox-*"
      ]
    },
    {
      "Sid": "ECRAuth",
      "Effect": "Allow",
      "Action": "ecr:GetAuthorizationToken",
      "Resource": "*"
    },
    {
      "Sid": "SecretsRead",
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": [
        "arn:aws:secretsmanager:us-west-2:123456789:secret:claude-orchestrator/*"
      ]
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:us-west-2:123456789:log-group:/eks/claude-orchestrator/*"
    }
  ]
}
```

---

## Scaling Configuration

### Horizontal Pod Autoscaler for Workers

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: claude-worker-hpa
  namespace: claude-system
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: claude-worker
  minReplicas: 2
  maxReplicas: 20
  metrics:
    # Scale based on Temporal task queue depth (custom metric)
    - type: External
      external:
        metric:
          name: temporal_task_queue_depth
          selector:
            matchLabels:
              queue: claude-code-tasks
        target:
          type: AverageValue
          averageValue: "5"
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Pods
          value: 4
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Pods
          value: 2
          periodSeconds: 120
```

### Cluster Autoscaler Node Pool

```yaml
# EKS Managed Node Group for sandboxes
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: claude-orchestrator
  region: us-west-2

managedNodeGroups:
  - name: sandbox-nodes
    instanceType: m5.xlarge
    desiredCapacity: 2
    minSize: 0
    maxSize: 50

    labels:
      node-type: sandbox

    taints:
      - key: sandbox
        value: "true"
        effect: NoSchedule

    # Use spot instances for cost savings
    spot: true
    instanceTypes:
      - m5.xlarge
      - m5a.xlarge
      - m5n.xlarge

    # Scaling configuration
    iam:
      withAddonPolicies:
        autoScaler: true

    tags:
      k8s.io/cluster-autoscaler/enabled: "true"
      k8s.io/cluster-autoscaler/claude-orchestrator: "owned"
```

### Pod Disruption Budget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: claude-worker-pdb
  namespace: claude-system
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: claude-worker
```

---

## Request Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            End-to-End Flow                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  1. Request Received (Slack/API)                                            │
│     "Fix null pointer in payments-service"                                  │
│     Requester: jane.doe                                                     │
│              │                                                              │
│              ▼                                                              │
│  2. Temporal Workflow Starts                                                │
│     - Workflow ID: bugfix-abc123                                            │
│     - Task queue: claude-code-tasks                                         │
│              │                                                              │
│              ▼                                                              │
│  3. Activity: CreateBugFixTaskCR                                            │
│     - Looks up Repository CR (payments-service)                             │
│     - Validates jane.doe is in allowedRequesters                            │
│     - Creates BugFixTask CR in team-payments namespace                      │
│              │                                                              │
│              ▼                                                              │
│  4. Operator Reconciles BugFixTask                                          │
│     - Reads SandboxProfile (nodejs-typescript)                              │
│     - Creates sandbox pod with profile settings                             │
│     - Injects secrets (ANTHROPIC_API_KEY, GITHUB_TOKEN)                     │
│     - Creates ConfigMap with AGENTS.md                                      │
│              │                                                              │
│              ▼                                                              │
│  5. Activity: WaitForSandboxReady                                           │
│     - Watches pod until Running phase                                       │
│     - Returns pod name and node                                             │
│              │                                                              │
│              ▼                                                              │
│  6. Activity: CloneRepositories                                             │
│     - kubectl exec: git clone payments-service                              │
│     - Reports progress via heartbeat                                        │
│              │                                                              │
│              ▼                                                              │
│  7. Activity: RunSetupScript                                                │
│     - kubectl exec: npm install && npm run build                            │
│              │                                                              │
│              ▼                                                              │
│  8. Activity: RunClaudeCode                                                 │
│     - kubectl exec: claude -p "Fix null pointer..."                         │
│     - Streams output, captures result                                       │
│     - Updates BugFixTask status.claudeResult                                │
│              │                                                              │
│              ▼                                                              │
│  9. Activity: RunValidationScript                                           │
│     - kubectl exec: npm test && npm run lint                                │
│     - Fails workflow if validation fails                                    │
│              │                                                              │
│              ▼                                                              │
│  10. Approval (if required)                                                 │
│      - Slack notification to #payments-bugs                                 │
│      - Temporal signal: wait for approve/reject                             │
│      - Timeout: 24 hours                                                    │
│              │                                                              │
│              ▼                                                              │
│  11. Activity: CreatePullRequest                                            │
│      - Uses Repository.pullRequest settings                                 │
│      - Adds reviewers, labels                                               │
│      - Returns PR URL                                                       │
│              │                                                              │
│              ▼                                                              │
│  12. Activity: UpdateBugFixTaskStatus                                       │
│      - Sets phase: Completed                                                │
│      - Records PR URL in status                                             │
│              │                                                              │
│              ▼                                                              │
│  13. Operator Cleanup                                                       │
│      - Deletes sandbox pod                                                  │
│      - Deletes AGENTS.md ConfigMap                                          │
│      - Retains BugFixTask CR for audit                                      │
│              │                                                              │
│              ▼                                                              │
│  14. Notification                                                           │
│      - Slack: "PR created: github.com/acme/payments-service/pull/456"       │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Implementation Phases

### Phase 1: Foundation (Week 1-2)

| Task | Description |
|------|-------------|
| CRD Schemas | Define SandboxProfile, Repository, BugFixTask OpenAPI schemas |
| Operator Scaffold | Use kubebuilder to generate operator structure |
| Basic Controller | Implement BugFixTask controller with pod creation |
| K8s Activities | Replace Docker client with Kubernetes client in activities |
| Local Testing | Test with kind/minikube cluster |

### Phase 2: Core Features (Week 3-4)

| Task | Description |
|------|-------------|
| Webhook Validation | Add validating webhooks for access control |
| Multi-Repo Support | Implement multi-repository task handling |
| Status Tracking | Rich status updates on BugFixTask CR |
| Cleanup Logic | Finalizers and garbage collection |
| Integration Tests | End-to-end tests with Temporal |

### Phase 3: Production Readiness (Week 5-6)

| Task | Description |
|------|-------------|
| EKS Deployment | Terraform/CDK for EKS cluster setup |
| IRSA Configuration | IAM roles for service accounts |
| Network Policies | Implement security boundaries |
| HPA Setup | Configure autoscaling for workers |
| Monitoring | Prometheus metrics, Grafana dashboards |
| Helm Charts | Package operator and workers for deployment |

### Phase 4: Advanced Features (Week 7-8)

| Task | Description |
|------|-------------|
| GPU Support | GPU node pools for ML workloads |
| Cost Tracking | Track API usage and compute costs per task |
| Audit Logging | CloudWatch integration for compliance |
| GitOps | ArgoCD integration for CRD management |
| Documentation | Runbooks, onboarding guides |

---

## Directory Structure

```
claude-code-orchestrator/
├── cmd/
│   ├── worker/           # Temporal worker (updated for K8s)
│   ├── cli/              # CLI tool
│   └── operator/         # Kubernetes operator entrypoint
├── internal/
│   ├── model/            # Data models (existing)
│   ├── workflow/         # Temporal workflows (existing)
│   ├── activity/
│   │   ├── kubernetes.go # NEW: K8s activities
│   │   ├── claudecode.go # Updated for K8s exec
│   │   └── github.go     # Existing
│   ├── client/           # Temporal client (existing)
│   └── operator/         # NEW: Operator controllers
│       ├── controllers/
│       │   ├── bugfixtask_controller.go
│       │   ├── repository_controller.go
│       │   └── sandboxprofile_controller.go
│       └── webhooks/
│           ├── bugfixtask_webhook.go
│           └── repository_webhook.go
├── api/                  # NEW: CRD definitions
│   └── v1/
│       ├── sandboxprofile_types.go
│       ├── repository_types.go
│       ├── bugfixtask_types.go
│       └── groupversion_info.go
├── config/               # NEW: Kubernetes manifests
│   ├── crd/
│   │   └── bases/
│   ├── rbac/
│   ├── manager/
│   └── samples/
├── deploy/               # NEW: Deployment configurations
│   ├── helm/
│   │   └── claude-orchestrator/
│   ├── terraform/
│   │   └── eks/
│   └── kustomize/
├── docker/
│   ├── Dockerfile.sandbox
│   ├── Dockerfile.worker
│   └── Dockerfile.operator
└── docs/
    └── plans/
        ├── PROTOTYPE_PLAN.md
        └── EKS_SCALABILITY_PLAN.md  # This document
```

---

## Benefits Summary

| Aspect | Benefit |
|--------|---------|
| **Scalability** | Workers auto-scale based on queue depth; sandboxes scale with node pools |
| **Security** | Pre-registered repos, requester validation, network isolation, least-privilege RBAC |
| **Multi-tenancy** | Namespace isolation per team, separate Repository CRs |
| **Configurability** | SandboxProfiles for different stacks, overrides at repo level |
| **Auditability** | BugFixTask CRs provide full history, CloudWatch integration |
| **GitOps** | All CRDs can be managed via ArgoCD/Flux |
| **Cost Efficiency** | Spot instances for sandboxes, auto-scaling down when idle |
| **Reliability** | Temporal durability + K8s self-healing + finalizers for cleanup |

---

## Trade-offs

| Trade-off | Mitigation |
|-----------|------------|
| Added complexity (two orchestration layers) | Clear separation: Temporal = workflow, Operator = infrastructure |
| Learning curve for Kubernetes operators | Use kubebuilder, follow controller-runtime patterns |
| CRD management overhead | GitOps with ArgoCD, self-service onboarding |
| Cold start latency for sandboxes | Pre-warmed pod pool (future optimization) |
| Multi-repo coordination complexity | Start with single-repo, add multi-repo incrementally |
