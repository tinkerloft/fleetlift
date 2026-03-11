# Fleetlift Platform Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rebuild Fleetlift as a general-purpose multi-tenant agentic workflow platform with DAG-based workflow templates, pluggable agents, real-time streaming, and HITL at any step.

**Architecture:** Fresh branch from `main`. DAGWorkflow (Temporal) orchestrates StepWorkflows; each step runs directly in an OpenSandbox container via the SDK — no sidecar agent binary, no file-based protocol. Nine builtin workflow templates cover the core DX use cases.

**Tech Stack:** Go 1.24, Temporal SDK, OpenSandbox SDK (Python patterns → Go), PostgreSQL, React 19 + TypeScript + shadcn/ui + Vite, cobra CLI, chi HTTP router, slog, Prometheus.

**Design doc:** `docs/plans/2026-03-11-platform-redesign.md`

---

## Phase 1: New Branch + Project Skeleton

### Task 1.1: Create branch and strip old architecture

**Step 1: Create branch**
```bash
git checkout -b platform-v2
```

**Step 2: Remove sidecar agent binary and file-based protocol**
- Delete: `cmd/agent/` (entire directory)
- Delete: `internal/agent/` (entire directory — sidecar, pipeline, fleetproto, etc.)
- Delete: `internal/sandbox/opensandbox/` (will replace with direct SDK calls)

**Step 3: Remove old task model and hardcoded workflows**
- Delete: `internal/model/task.go`, `internal/model/result.go`
- Delete: `internal/workflow/transform.go`, `internal/workflow/transform_v2.go`, `internal/workflow/transform_group.go`
- Delete: `internal/activity/agent.go`, `internal/activity/sandbox.go`

**Step 4: Keep (do not delete)**
- `internal/activity/knowledge.go` — keep, will adapt
- `internal/activity/slack.go` — keep, will extend
- `internal/activity/github.go` — keep, will extend
- `internal/metrics/`, `internal/logging/`, `internal/client/` — keep unchanged
- `web/` — keep React shell, wipe page content
- `cmd/cli/`, `cmd/worker/`, `cmd/server/` — keep entry points, gut contents

**Step 5: Update go.mod — add new dependencies**
```bash
go get github.com/golang-jwt/jwt/v5
go get golang.org/x/crypto
go get github.com/google/uuid
go get github.com/lib/pq
go get github.com/jmoiron/sqlx
```

**Step 6: Verify build (errors expected but no panics)**
```bash
go build ./... 2>&1 | head -40
```

**Step 7: Commit**
```bash
git add -A
git commit -m "chore: strip old architecture, start platform-v2 branch"
```

---

## Phase 2: Database Schema

### Task 2.1: PostgreSQL schema

**Files:**
- Create: `internal/db/schema.sql`
- Create: `internal/db/db.go`
- Create: `internal/db/migrations/001_initial.sql`

**Step 1: Write schema**

`internal/db/schema.sql`:
```sql
-- Teams
CREATE TABLE teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT,
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL,   -- 'github', 'okta', etc.
    provider_id     TEXT NOT NULL,
    platform_admin  BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_id)
);

-- Team membership
CREATE TABLE team_members (
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member',  -- 'member' | 'admin'
    PRIMARY KEY (team_id, user_id)
);

-- Refresh tokens
CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Credentials (team-scoped secrets)
CREATE TABLE credentials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    value_enc   BYTEA NOT NULL,   -- AES-256-GCM encrypted
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, name)
);

-- Workflow templates (team-owned; builtins stored separately in binary)
CREATE TABLE workflow_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT,
    tags        TEXT[] NOT NULL DEFAULT '{}',
    yaml_body   TEXT NOT NULL,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, slug)
);

-- Runs
CREATE TABLE runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams(id),
    workflow_id     TEXT NOT NULL,   -- slug; may reference builtin
    workflow_title  TEXT NOT NULL,
    parameters      JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    -- pending | running | awaiting_input | complete | failed | cancelled
    temporal_id     TEXT,
    triggered_by    UUID REFERENCES users(id),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX runs_team_status ON runs(team_id, status);

-- Step runs
CREATE TABLE step_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_id         TEXT NOT NULL,
    step_title      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    -- pending | cloning | running | verifying | awaiting_input | complete | failed | skipped
    sandbox_id      TEXT,
    sandbox_group   TEXT,
    output          JSONB,         -- structured output from agent
    diff            TEXT,          -- git diff (transform steps)
    pr_url          TEXT,
    branch_name     TEXT,
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX step_runs_run_id ON step_runs(run_id);

-- Streaming logs (append-only)
CREATE TABLE step_run_logs (
    id              BIGSERIAL PRIMARY KEY,
    step_run_id     UUID NOT NULL REFERENCES step_runs(id) ON DELETE CASCADE,
    seq             BIGINT NOT NULL,
    stream          TEXT NOT NULL DEFAULT 'stdout',  -- 'stdout' | 'stderr' | 'system'
    content         TEXT NOT NULL,
    ts              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX step_run_logs_step_seq ON step_run_logs(step_run_id, seq);

-- Artifacts
CREATE TABLE artifacts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    step_run_id     UUID NOT NULL REFERENCES step_runs(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    path            TEXT NOT NULL,      -- original path in sandbox
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    content_type    TEXT NOT NULL DEFAULT 'text/plain',
    storage         TEXT NOT NULL,      -- 'inline' | 'object_store'
    data            BYTEA,              -- for 'inline' storage
    object_key      TEXT,               -- for 'object_store'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Inbox items
CREATE TABLE inbox_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_run_id     UUID REFERENCES step_runs(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,  -- 'awaiting_input' | 'output_ready'
    title           TEXT NOT NULL,
    summary         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX inbox_team ON inbox_items(team_id, created_at DESC);

-- Inbox read receipts (per user)
CREATE TABLE inbox_reads (
    inbox_item_id   UUID NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (inbox_item_id, user_id)
);

-- Service account API keys
CREATE TABLE api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,
    role        TEXT NOT NULL DEFAULT 'member',
    created_by  UUID REFERENCES users(id),
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Step 2: Write db.go connection helper**

`internal/db/db.go`:
```go
package db

import (
    "context"
    "fmt"
    "os"

    "github.com/jmoiron/sqlx"
    _ "github.com/lib/pq"
)

func Connect(ctx context.Context) (*sqlx.DB, error) {
    dsn := os.Getenv("DATABASE_URL")
    if dsn == "" {
        dsn = "postgres://fleetlift:fleetlift@localhost:5432/fleetlift?sslmode=disable"
    }
    db, err := sqlx.ConnectContext(ctx, "postgres", dsn)
    if err != nil {
        return nil, fmt.Errorf("connect db: %w", err)
    }
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    return db, nil
}

func Migrate(db *sqlx.DB) error {
    _, err := db.Exec(schema)
    return err
}
```

**Step 3: Write migration SQL file** — copy schema.sql content into `001_initial.sql`.

**Step 4: Write DB connection test**

`internal/db/db_test.go`:
```go
func TestConnect(t *testing.T) {
    if os.Getenv("DATABASE_URL") == "" {
        t.Skip("DATABASE_URL not set")
    }
    db, err := Connect(context.Background())
    require.NoError(t, err)
    require.NoError(t, db.Ping())
}
```

**Step 5: Commit**
```bash
git add internal/db/
git commit -m "feat: database schema and connection"
```

---

## Phase 3: Core Models

### Task 3.1: Data model structs

**Files:**
- Create: `internal/model/team.go`
- Create: `internal/model/user.go`
- Create: `internal/model/workflow.go`
- Create: `internal/model/run.go`
- Create: `internal/model/step.go`
- Create: `internal/model/artifact.go`
- Create: `internal/model/inbox.go`

**Step 1: `internal/model/team.go`**
```go
package model

import "time"

type Team struct {
    ID        string    `db:"id" json:"id"`
    Name      string    `db:"name" json:"name"`
    Slug      string    `db:"slug" json:"slug"`
    CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type TeamMember struct {
    TeamID string `db:"team_id" json:"team_id"`
    UserID string `db:"user_id" json:"user_id"`
    Role   string `db:"role" json:"role"` // "member" | "admin"
}
```

**Step 2: `internal/model/user.go`**
```go
package model

import "time"

type User struct {
    ID            string    `db:"id" json:"id"`
    Email         string    `db:"email" json:"email"`
    Name          string    `db:"name" json:"name"`
    Provider      string    `db:"provider" json:"provider"`
    ProviderID    string    `db:"provider_id" json:"provider_id"`
    PlatformAdmin bool      `db:"platform_admin" json:"platform_admin"`
    CreatedAt     time.Time `db:"created_at" json:"created_at"`
}
```

**Step 3: `internal/model/workflow.go`**
```go
package model

import "time"

// WorkflowTemplate is a reusable DAG definition stored in the DB or embedded as builtin.
type WorkflowTemplate struct {
    ID          string    `db:"id" json:"id"`
    TeamID      string    `db:"team_id" json:"team_id"`
    Slug        string    `db:"slug" json:"slug"`
    Title       string    `db:"title" json:"title"`
    Description string    `db:"description" json:"description"`
    Tags        []string  `db:"tags" json:"tags"`
    YAMLBody    string    `db:"yaml_body" json:"yaml_body"`
    Builtin     bool      `db:"-" json:"builtin"`
    CreatedAt   time.Time `db:"created_at" json:"created_at"`
    UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// WorkflowDef is the parsed form of a WorkflowTemplate's YAML.
type WorkflowDef struct {
    Version     int             `yaml:"version"`
    ID          string          `yaml:"id"`
    Title       string          `yaml:"title"`
    Description string          `yaml:"description"`
    Tags        []string        `yaml:"tags"`
    Parameters  []ParameterDef  `yaml:"parameters"`
    Steps       []StepDef       `yaml:"steps"`
}

type ParameterDef struct {
    Name        string `yaml:"name"`
    Type        string `yaml:"type"`        // string | int | bool | json
    Required    bool   `yaml:"required"`
    Default     any    `yaml:"default,omitempty"`
    Description string `yaml:"description,omitempty"`
}

type StepDef struct {
    ID                    string            `yaml:"id"`
    Title                 string            `yaml:"title,omitempty"`
    DependsOn             []string          `yaml:"depends_on,omitempty"`
    SandboxGroup          string            `yaml:"sandbox_group,omitempty"`
    Mode                  string            `yaml:"mode,omitempty"`       // report | transform
    Repositories          any               `yaml:"repositories,omitempty"` // []RepoRef or template string
    MaxParallel           int               `yaml:"max_parallel,omitempty"`
    FailureThreshold      int               `yaml:"failure_threshold,omitempty"`
    Execution             *ExecutionDef     `yaml:"execution,omitempty"`
    ApprovalPolicy        string            `yaml:"approval_policy,omitempty"` // always|never|agent|on_changes
    AllowMidExecPause     bool              `yaml:"allow_mid_execution_pause,omitempty"`
    PullRequest           *PRDef            `yaml:"pull_request,omitempty"`
    Condition             string            `yaml:"condition,omitempty"`
    Optional              bool              `yaml:"optional,omitempty"`
    Outputs               *StepOutputsDef   `yaml:"outputs,omitempty"`
    Inputs                *StepInputsDef    `yaml:"inputs,omitempty"`
    Action                *ActionDef        `yaml:"action,omitempty"`
    Sandbox               *SandboxSpec      `yaml:"sandbox,omitempty"`
    Timeout               string            `yaml:"timeout,omitempty"`
}

// SandboxSpec declares the infrastructure requirements for a step's sandbox.
// Merged with workflow-level defaults and platform defaults at provision time
// (step > workflow defaults > platform defaults).
type SandboxSpec struct {
    Image         string           `yaml:"image,omitempty"`
    Resources     SandboxResources `yaml:"resources,omitempty"`
    Egress        EgressPolicy     `yaml:"egress,omitempty"`
    Timeout       string           `yaml:"timeout,omitempty"`
    WorkspaceSize string           `yaml:"workspace_size,omitempty"`
}

type SandboxResources struct {
    CPU    string `yaml:"cpu,omitempty"`    // e.g. "2"
    Memory string `yaml:"memory,omitempty"` // e.g. "4Gi"
    GPU    bool   `yaml:"gpu,omitempty"`
}

type EgressPolicy struct {
    Allow            []string `yaml:"allow,omitempty"`
    DenyAllByDefault bool     `yaml:"deny_all_by_default,omitempty"`
}

type ExecutionDef struct {
    Agent       string          `yaml:"agent"`       // "claude-code" | "codex"
    Prompt      string          `yaml:"prompt"`
    Verifiers   any             `yaml:"verifiers,omitempty"`
    Credentials []string        `yaml:"credentials,omitempty"`
    Output      *OutputSchemaDef `yaml:"output,omitempty"`
}

type OutputSchemaDef struct {
    Schema map[string]any `yaml:"schema"`
}

type PRDef struct {
    BranchPrefix string   `yaml:"branch_prefix"`
    Title        string   `yaml:"title"`
    Body         string   `yaml:"body,omitempty"`
    Labels       []string `yaml:"labels,omitempty"`
    Draft        bool     `yaml:"draft,omitempty"`
}

type ActionDef struct {
    Type   string         `yaml:"type"`
    Config map[string]any `yaml:"config"`
}

type StepOutputsDef struct {
    Artifacts []ArtifactRef `yaml:"artifacts,omitempty"`
}

type StepInputsDef struct {
    Artifacts []ArtifactMount `yaml:"artifacts,omitempty"`
}

type ArtifactRef struct {
    Path string `yaml:"path"`
    Name string `yaml:"name"`
}

type ArtifactMount struct {
    Name      string `yaml:"name"`
    MountPath string `yaml:"mount_path"`
}

type RepoRef struct {
    URL    string `yaml:"url"`
    Branch string `yaml:"branch,omitempty"`
    Name   string `yaml:"name,omitempty"`
}
```

**Step 4: `internal/model/run.go`**
```go
package model

import "time"

type RunStatus string

const (
    RunStatusPending       RunStatus = "pending"
    RunStatusRunning       RunStatus = "running"
    RunStatusAwaitingInput RunStatus = "awaiting_input"
    RunStatusComplete      RunStatus = "complete"
    RunStatusFailed        RunStatus = "failed"
    RunStatusCancelled     RunStatus = "cancelled"
)

type Run struct {
    ID             string            `db:"id" json:"id"`
    TeamID         string            `db:"team_id" json:"team_id"`
    WorkflowID     string            `db:"workflow_id" json:"workflow_id"`
    WorkflowTitle  string            `db:"workflow_title" json:"workflow_title"`
    Parameters     map[string]any    `db:"parameters" json:"parameters"`
    Status         RunStatus         `db:"status" json:"status"`
    TemporalID     string            `db:"temporal_id" json:"temporal_id,omitempty"`
    TriggeredBy    string            `db:"triggered_by" json:"triggered_by,omitempty"`
    StartedAt      *time.Time        `db:"started_at" json:"started_at,omitempty"`
    CompletedAt    *time.Time        `db:"completed_at" json:"completed_at,omitempty"`
    CreatedAt      time.Time         `db:"created_at" json:"created_at"`
}
```

**Step 5: `internal/model/step.go`**
```go
package model

import "time"

type StepStatus string

const (
    StepStatusPending       StepStatus = "pending"
    StepStatusCloning       StepStatus = "cloning"
    StepStatusRunning       StepStatus = "running"
    StepStatusVerifying     StepStatus = "verifying"
    StepStatusAwaitingInput StepStatus = "awaiting_input"
    StepStatusComplete      StepStatus = "complete"
    StepStatusFailed        StepStatus = "failed"
    StepStatusSkipped       StepStatus = "skipped"
)

type StepRun struct {
    ID           string         `db:"id" json:"id"`
    RunID        string         `db:"run_id" json:"run_id"`
    StepID       string         `db:"step_id" json:"step_id"`
    StepTitle    string         `db:"step_title" json:"step_title"`
    Status       StepStatus     `db:"status" json:"status"`
    SandboxID    string         `db:"sandbox_id" json:"sandbox_id,omitempty"`
    SandboxGroup string         `db:"sandbox_group" json:"sandbox_group,omitempty"`
    Output       map[string]any `db:"output" json:"output,omitempty"`
    Diff         string         `db:"diff" json:"diff,omitempty"`
    PRUrl        string         `db:"pr_url" json:"pr_url,omitempty"`
    BranchName   string         `db:"branch_name" json:"branch_name,omitempty"`
    ErrorMessage string         `db:"error_message" json:"error_message,omitempty"`
    StartedAt    *time.Time     `db:"started_at" json:"started_at,omitempty"`
    CompletedAt  *time.Time     `db:"completed_at" json:"completed_at,omitempty"`
    CreatedAt    time.Time      `db:"created_at" json:"created_at"`
}

type StepRunLog struct {
    ID         int64     `db:"id"`
    StepRunID  string    `db:"step_run_id"`
    Seq        int64     `db:"seq"`
    Stream     string    `db:"stream"` // stdout | stderr | system
    Content    string    `db:"content"`
    Ts         time.Time `db:"ts"`
}

// StepOutput is the in-memory result passed between DAG steps via template resolution.
type StepOutput struct {
    StepID     string         `json:"step_id"`
    Status     StepStatus     `json:"status"`
    Output     map[string]any `json:"output,omitempty"`    // structured agent output
    Diff       string         `json:"diff,omitempty"`
    PRUrl      string         `json:"pr_url,omitempty"`
    BranchName string         `json:"branch_name,omitempty"`
    Outputs    []StepOutput   `json:"outputs,omitempty"`   // fan-out: per-repo results
    Error      string         `json:"error,omitempty"`
}
```

**Step 6: Write model tests for WorkflowDef YAML parsing**

`internal/model/workflow_test.go`:
```go
func TestWorkflowDefParse(t *testing.T) {
    yaml := `
version: 1
id: test-wf
title: Test
parameters:
  - name: repo_url
    type: string
    required: true
steps:
  - id: analyze
    mode: report
    repositories:
      - url: "{{ .Params.repo_url }}"
    execution:
      agent: claude-code
      prompt: "Analyze the code"
`
    var def WorkflowDef
    err := parseWorkflowYAML([]byte(yaml), &def)
    require.NoError(t, err)
    assert.Equal(t, "test-wf", def.ID)
    assert.Len(t, def.Parameters, 1)
    assert.Len(t, def.Steps, 1)
    assert.Equal(t, "analyze", def.Steps[0].ID)
}
```

**Step 7: Run tests**
```bash
go test ./internal/model/...
```

**Step 8: Commit**
```bash
git add internal/model/
git commit -m "feat: core model types"
```

---

## Phase 4: Auth Layer

### Task 4.1: JWT + GitHub OAuth

**Files:**
- Create: `internal/auth/jwt.go`
- Create: `internal/auth/github.go`
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/provider.go`
- Test: `internal/auth/jwt_test.go`

**Step 1: Auth provider interface** (`internal/auth/provider.go`)
```go
package auth

type ExternalIdentity struct {
    ProviderID string
    Email      string
    Name       string
    Provider   string
}

type Provider interface {
    Name() string
    AuthURL(state string) string
    Exchange(ctx context.Context, code string) (*ExternalIdentity, error)
}
```

**Step 2: JWT claims and issue/validate** (`internal/auth/jwt.go`)
```go
package auth

import (
    "time"
    "github.com/golang-jwt/jwt/v5"
)

type Claims struct {
    UserID        string            `json:"user_id"`
    TeamRoles     map[string]string `json:"team_roles"` // team_id -> role
    PlatformAdmin bool              `json:"platform_admin"`
    jwt.RegisteredClaims
}

func IssueToken(secret []byte, userID string, teamRoles map[string]string, platformAdmin bool) (string, error) {
    claims := Claims{
        UserID:        userID,
        TeamRoles:     teamRoles,
        PlatformAdmin: platformAdmin,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(secret)
}

func ValidateToken(secret []byte, tokenStr string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
        if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method")
        }
        return secret, nil
    })
    if err != nil {
        return nil, err
    }
    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, fmt.Errorf("invalid token")
    }
    return claims, nil
}
```

**Step 3: Write JWT tests** (`internal/auth/jwt_test.go`)
```go
func TestIssueAndValidate(t *testing.T) {
    secret := []byte("test-secret")
    token, err := IssueToken(secret, "user-1", map[string]string{"team-1": "admin"}, false)
    require.NoError(t, err)
    require.NotEmpty(t, token)

    claims, err := ValidateToken(secret, token)
    require.NoError(t, err)
    assert.Equal(t, "user-1", claims.UserID)
    assert.Equal(t, "admin", claims.TeamRoles["team-1"])
}

func TestExpiredToken(t *testing.T) {
    // issue a token that already expired
    claims := Claims{
        UserID: "user-1",
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    tokenStr, _ := token.SignedString([]byte("test-secret"))

    _, err := ValidateToken([]byte("test-secret"), tokenStr)
    require.Error(t, err)
}
```

**Step 4: Run tests**
```bash
go test ./internal/auth/...
```

**Step 5: GitHub OAuth provider** (`internal/auth/github.go`)
```go
package auth

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "golang.org/x/oauth2"
    "golang.org/x/oauth2/github"
)

type GitHubProvider struct {
    config *oauth2.Config
}

func NewGitHubProvider(clientID, clientSecret, callbackURL string) *GitHubProvider {
    return &GitHubProvider{
        config: &oauth2.Config{
            ClientID:     clientID,
            ClientSecret: clientSecret,
            RedirectURL:  callbackURL,
            Scopes:       []string{"user:email"},
            Endpoint:     github.Endpoint,
        },
    }
}

func (g *GitHubProvider) Name() string { return "github" }

func (g *GitHubProvider) AuthURL(state string) string {
    return g.config.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (g *GitHubProvider) Exchange(ctx context.Context, code string) (*ExternalIdentity, error) {
    tok, err := g.config.Exchange(ctx, code)
    if err != nil {
        return nil, fmt.Errorf("exchange code: %w", err)
    }
    client := g.config.Client(ctx, tok)
    resp, err := client.Get("https://api.github.com/user")
    if err != nil {
        return nil, fmt.Errorf("fetch github user: %w", err)
    }
    defer resp.Body.Close()
    var ghUser struct {
        ID    int64  `json:"id"`
        Login string `json:"login"`
        Email string `json:"email"`
        Name  string `json:"name"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&ghUser); err != nil {
        return nil, fmt.Errorf("decode github user: %w", err)
    }
    name := ghUser.Name
    if name == "" {
        name = ghUser.Login
    }
    return &ExternalIdentity{
        Provider:   "github",
        ProviderID: fmt.Sprintf("%d", ghUser.ID),
        Email:      ghUser.Email,
        Name:       name,
    }, nil
}
```

**Step 6: HTTP middleware** (`internal/auth/middleware.go`)
```go
package auth

import (
    "context"
    "net/http"
    "strings"
)

type contextKey string
const claimsKey contextKey = "claims"

func Middleware(secret []byte) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := extractToken(r)
            if token == "" {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
            claims, err := ValidateToken(secret, token)
            if err != nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
            ctx := context.WithValue(r.Context(), claimsKey, claims)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func ClaimsFromContext(ctx context.Context) *Claims {
    c, _ := ctx.Value(claimsKey).(*Claims)
    return c
}

func extractToken(r *http.Request) string {
    // Bearer token
    if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
        return strings.TrimPrefix(h, "Bearer ")
    }
    // Cookie
    if c, err := r.Cookie("fl_token"); err == nil {
        return c.Value
    }
    return ""
}
```

**Step 7: Commit**
```bash
git add internal/auth/
git commit -m "feat: JWT auth and GitHub OAuth provider"
```

---

## Phase 5: Template Provider System

### Task 5.1: TemplateProvider interface + BuiltinProvider

**Files:**
- Create: `internal/template/provider.go`
- Create: `internal/template/builtin.go`
- Create: `internal/template/db.go`
- Create: `internal/template/render.go`
- Create: `internal/template/workflows/fleet-research.yaml`
- Create: `internal/template/workflows/fleet-transform.yaml`
- Create: `internal/template/workflows/bug-fix.yaml`
- Create: `internal/template/workflows/dependency-update.yaml`
- Create: `internal/template/workflows/pr-review.yaml`
- Create: `internal/template/workflows/migration.yaml`
- Create: `internal/template/workflows/triage.yaml`
- Create: `internal/template/workflows/audit.yaml`
- Create: `internal/template/workflows/incident-response.yaml`

**Step 1: Provider interface** (`internal/template/provider.go`)
```go
package template

import (
    "context"
    "github.com/tinkerloft/fleetlift/internal/model"
)

type Provider interface {
    Name() string
    Writable() bool
    List(ctx context.Context, teamID string) ([]*model.WorkflowTemplate, error)
    Get(ctx context.Context, teamID, slug string) (*model.WorkflowTemplate, error)
    Save(ctx context.Context, teamID string, t *model.WorkflowTemplate) error
    Delete(ctx context.Context, teamID, slug string) error
}

// Registry merges multiple providers, higher-index providers override lower-index.
type Registry struct {
    providers []Provider
}

func NewRegistry(providers ...Provider) *Registry {
    return &Registry{providers: providers}
}

func (r *Registry) List(ctx context.Context, teamID string) ([]*model.WorkflowTemplate, error) {
    seen := map[string]*model.WorkflowTemplate{}
    for _, p := range r.providers {
        items, err := p.List(ctx, teamID)
        if err != nil {
            return nil, err
        }
        for _, t := range items {
            seen[t.Slug] = t // later providers override
        }
    }
    out := make([]*model.WorkflowTemplate, 0, len(seen))
    for _, t := range seen {
        out = append(out, t)
    }
    return out, nil
}

func (r *Registry) Get(ctx context.Context, teamID, slug string) (*model.WorkflowTemplate, error) {
    // Search providers in reverse (highest priority first)
    for i := len(r.providers) - 1; i >= 0; i-- {
        t, err := r.providers[i].Get(ctx, teamID, slug)
        if err == nil && t != nil {
            return t, nil
        }
    }
    return nil, ErrNotFound
}

func (r *Registry) WritableProvider() Provider {
    for i := len(r.providers) - 1; i >= 0; i-- {
        if r.providers[i].Writable() {
            return r.providers[i]
        }
    }
    return nil
}

var ErrNotFound = fmt.Errorf("workflow template not found")
```

**Step 2: BuiltinProvider** (`internal/template/builtin.go`)
```go
package template

import (
    "context"
    "embed"
    "path"

    "github.com/tinkerloft/fleetlift/internal/model"
    "gopkg.in/yaml.v3"
)

//go:embed workflows/*.yaml
var workflowFiles embed.FS

type BuiltinProvider struct {
    templates []*model.WorkflowTemplate
}

func NewBuiltinProvider() (*BuiltinProvider, error) {
    entries, err := workflowFiles.ReadDir("workflows")
    if err != nil {
        return nil, err
    }
    var templates []*model.WorkflowTemplate
    for _, e := range entries {
        data, err := workflowFiles.ReadFile(path.Join("workflows", e.Name()))
        if err != nil {
            return nil, err
        }
        var def model.WorkflowDef
        if err := yaml.Unmarshal(data, &def); err != nil {
            return nil, fmt.Errorf("parse builtin %s: %w", e.Name(), err)
        }
        templates = append(templates, &model.WorkflowTemplate{
            ID:       def.ID,
            Slug:     def.ID,
            Title:    def.Title,
            Tags:     def.Tags,
            YAMLBody: string(data),
            Builtin:  true,
        })
    }
    return &BuiltinProvider{templates: templates}, nil
}

func (b *BuiltinProvider) Name() string    { return "builtin" }
func (b *BuiltinProvider) Writable() bool { return false }

func (b *BuiltinProvider) List(_ context.Context, _ string) ([]*model.WorkflowTemplate, error) {
    return b.templates, nil
}

func (b *BuiltinProvider) Get(_ context.Context, _, slug string) (*model.WorkflowTemplate, error) {
    for _, t := range b.templates {
        if t.Slug == slug {
            return t, nil
        }
    }
    return nil, ErrNotFound
}

func (b *BuiltinProvider) Save(_ context.Context, _ string, _ *model.WorkflowTemplate) error {
    return fmt.Errorf("builtin provider is read-only")
}

func (b *BuiltinProvider) Delete(_ context.Context, _, _ string) error {
    return fmt.Errorf("builtin provider is read-only")
}
```

**Step 3: Template rendering** (`internal/template/render.go`)
```go
package template

import (
    "bytes"
    "fmt"
    "text/template"

    "github.com/tinkerloft/fleetlift/internal/model"
)

type RenderContext struct {
    Params map[string]any
    Steps  map[string]*model.StepOutput
}

// RenderPrompt resolves Go template expressions in a prompt string.
func RenderPrompt(tmpl string, ctx RenderContext) (string, error) {
    t, err := template.New("prompt").
        Funcs(templateFuncs()).
        Option("missingkey=error").
        Parse(tmpl)
    if err != nil {
        return "", fmt.Errorf("parse template: %w", err)
    }
    var buf bytes.Buffer
    if err := t.Execute(&buf, ctx); err != nil {
        return "", fmt.Errorf("render template: %w", err)
    }
    return buf.String(), nil
}

func templateFuncs() template.FuncMap {
    return template.FuncMap{
        "toJSON":   toJSON,
        "truncate": truncate,
        "join":     strings.Join,
    }
}
```

**Step 4: Write tests for render**
```go
func TestRenderPrompt(t *testing.T) {
    ctx := RenderContext{
        Params: map[string]any{"issue_body": "Login broken"},
        Steps:  map[string]*model.StepOutput{},
    }
    out, err := RenderPrompt("Fix: {{ .Params.issue_body }}", ctx)
    require.NoError(t, err)
    assert.Equal(t, "Fix: Login broken", out)
}

func TestRenderUnknownVar(t *testing.T) {
    ctx := RenderContext{Params: map[string]any{}}
    _, err := RenderPrompt("{{ .Params.missing }}", ctx)
    require.Error(t, err)
}
```

**Step 5: Run tests**
```bash
go test ./internal/template/...
```

**Step 6: Write the nine builtin YAML files** (see design doc §15 for content of each)
- `internal/template/workflows/fleet-research.yaml`
- `internal/template/workflows/fleet-transform.yaml`
- `internal/template/workflows/bug-fix.yaml`
- `internal/template/workflows/dependency-update.yaml`
- `internal/template/workflows/pr-review.yaml`
- `internal/template/workflows/migration.yaml`
- `internal/template/workflows/triage.yaml`
- `internal/template/workflows/audit.yaml`
- `internal/template/workflows/incident-response.yaml`

Each file follows the YAML structure in the design doc §5.2. Use realistic prompts appropriate to each workflow type.

**Step 7: Verify BuiltinProvider loads all nine**
```go
func TestBuiltinProviderLoadsAll(t *testing.T) {
    p, err := NewBuiltinProvider()
    require.NoError(t, err)
    templates, err := p.List(context.Background(), "")
    require.NoError(t, err)
    assert.Len(t, templates, 9)
    slugs := map[string]bool{}
    for _, t := range templates {
        slugs[t.Slug] = true
    }
    for _, expected := range []string{
        "fleet-research", "fleet-transform", "bug-fix", "dependency-update",
        "pr-review", "migration", "triage", "audit", "incident-response",
    } {
        assert.True(t, slugs[expected], "missing builtin: %s", expected)
    }
}
```

**Step 8: Commit**
```bash
git add internal/template/
git commit -m "feat: template provider system with nine builtin workflows"
```

---

## Phase 6: Agent Runner Interface + ClaudeCodeRunner

### Task 6.1: AgentRunner abstraction

**Files:**
- Create: `internal/agent/runner.go`
- Create: `internal/agent/claudecode.go`
- Test: `internal/agent/claudecode_test.go`

**Step 1: Interface** (`internal/agent/runner.go`)
```go
package agent

import "context"

type Event struct {
    Type    string         // "stdout" | "stderr" | "complete" | "error" | "needs_input"
    Content string
    Output  map[string]any // on "complete": structured output parsed from agent
}

type RunOpts struct {
    Prompt      string
    WorkDir     string
    MaxTurns    int
    Environment map[string]string
}

type Runner interface {
    Name() string
    // Run executes the agent in the given sandbox (by ID) and streams events.
    // The channel is closed when the agent completes or errors.
    Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error)
    // Interrupt kills a running agent.
    Interrupt(ctx context.Context, sandboxID string) error
}
```

**Step 2: ClaudeCodeRunner** (`internal/agent/claudecode.go`)
```go
package agent

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"

    "github.com/tinkerloft/fleetlift/internal/sandbox"
)

type ClaudeCodeRunner struct {
    sandbox sandbox.Client
}

func NewClaudeCodeRunner(sb sandbox.Client) *ClaudeCodeRunner {
    return &ClaudeCodeRunner{sandbox: sb}
}

func (r *ClaudeCodeRunner) Name() string { return "claude-code" }

func (r *ClaudeCodeRunner) Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error) {
    cmd := fmt.Sprintf("claude -p %q --output-format stream-json --max-turns %d",
        opts.Prompt, max(opts.MaxTurns, 20))

    ch := make(chan Event, 64)
    go func() {
        defer close(ch)
        err := r.sandbox.ExecStream(ctx, sandboxID, cmd, opts.WorkDir, func(line string) {
            event := parseClaudeEvent(line)
            select {
            case ch <- event:
            case <-ctx.Done():
            }
        })
        if err != nil {
            ch <- Event{Type: "error", Content: err.Error()}
        }
    }()
    return ch, nil
}

// parseClaudeEvent parses a single line of claude --output-format stream-json output.
func parseClaudeEvent(line string) Event {
    var raw map[string]any
    if err := json.Unmarshal([]byte(line), &raw); err != nil {
        return Event{Type: "stdout", Content: line}
    }
    typ, _ := raw["type"].(string)
    switch typ {
    case "result":
        return Event{Type: "complete", Output: raw}
    case "needs_input":
        return Event{Type: "needs_input", Content: fmt.Sprintf("%v", raw["message"])}
    default:
        content, _ := raw["content"].(string)
        return Event{Type: "stdout", Content: content}
    }
}
```

**Step 3: Sandbox client interface** (`internal/sandbox/client.go`)
```go
package sandbox

import "context"

type Client interface {
    Create(ctx context.Context, opts CreateOpts) (string, error)  // returns sandbox ID
    ExecStream(ctx context.Context, id, cmd, workDir string, onLine func(string)) error
    Exec(ctx context.Context, id, cmd, workDir string) (stdout, stderr string, err error)
    WriteFile(ctx context.Context, id, path, content string) error
    ReadFile(ctx context.Context, id, path string) (string, error)
    ReadBytes(ctx context.Context, id, path string) ([]byte, error)
    Kill(ctx context.Context, id string) error
    RenewExpiration(ctx context.Context, id string) error
}

type CreateOpts struct {
    Image       string
    Env         map[string]string
    TimeoutMins int
}
```

**Step 4: Commit**
```bash
git add internal/agent/ internal/sandbox/
git commit -m "feat: AgentRunner interface and ClaudeCodeRunner"
```

---

## Phase 7: OpenSandbox Client Implementation

### Task 7.1: OpenSandbox SDK wrapper in Go

**Files:**
- Create: `internal/sandbox/opensandbox/client.go`
- Test: `internal/sandbox/opensandbox/client_test.go`

**Step 1: Implement `sandbox.Client` using OpenSandbox REST API**

Key operations to implement against OpenSandbox:
- `Create` → `POST /v1/sandboxes` with image + env
- `ExecStream` → `POST /command` (background=false, SSE stream) then read lines
- `Exec` → same but collect all output synchronously
- `WriteFile` → `POST /files/upload` (multipart)
- `ReadFile` → `GET /files/download?path=...`
- `ReadBytes` → same as ReadFile but return bytes
- `Kill` → `DELETE /sandboxes/{id}`
- `RenewExpiration` → `POST /sandboxes/{id}/renew-expiration`

Config via env: `OPENSANDBOX_DOMAIN`, `OPENSANDBOX_API_KEY`.

**Step 2: Integration test (skipped without OPENSANDBOX_DOMAIN)**
```go
func TestClientRoundTrip(t *testing.T) {
    if os.Getenv("OPENSANDBOX_DOMAIN") == "" {
        t.Skip("OPENSANDBOX_DOMAIN not set")
    }
    c := opensandbox.New(os.Getenv("OPENSANDBOX_DOMAIN"), os.Getenv("OPENSANDBOX_API_KEY"))
    id, err := c.Create(ctx, sandbox.CreateOpts{
        Image: "ubuntu:22.04",
        Env:   map[string]string{"TEST": "1"},
    })
    require.NoError(t, err)
    defer c.Kill(ctx, id)

    stdout, _, err := c.Exec(ctx, id, "echo hello", "/")
    require.NoError(t, err)
    assert.Equal(t, "hello\n", stdout)

    err = c.WriteFile(ctx, id, "/tmp/test.txt", "content")
    require.NoError(t, err)

    content, err := c.ReadFile(ctx, id, "/tmp/test.txt")
    require.NoError(t, err)
    assert.Equal(t, "content", content)
}
```

**Step 3: Commit**
```bash
git add internal/sandbox/opensandbox/
git commit -m "feat: OpenSandbox client implementation"
```

---

## Phase 8: DAG Workflow (Temporal)

### Task 8.1: StepWorkflow

**Files:**
- Create: `internal/workflow/step.go`
- Create: `internal/activity/step.go`
- Create: `internal/activity/constants.go`
- Test: `internal/workflow/step_test.go`

**Step 1: StepWorkflow signals and queries**

`internal/workflow/step.go`:
```go
package workflow

import (
    "go.temporal.io/sdk/workflow"
    "github.com/tinkerloft/fleetlift/internal/model"
)

type StepInput struct {
    RunID        string
    StepRunID    string
    StepDef      model.StepDef
    ResolvedOpts ResolvedStepOpts  // templates already rendered by DAGWorkflow
    SandboxID    string            // non-empty if sandbox_group reuse
}

type ResolvedStepOpts struct {
    Prompt      string
    Repos       []model.RepoRef
    Verifiers   []model.VerifierDef
    Credentials []string
    PRConfig    *model.PRDef
    Agent       string
}

type StepSignal string
const (
    SignalApprove StepSignal = "approve"
    SignalReject  StepSignal = "reject"
    SignalSteer   StepSignal = "steer"
    SignalCancel  StepSignal = "cancel"
)

type SteerPayload struct {
    Prompt string
}

func StepWorkflow(ctx workflow.Context, input StepInput) (*model.StepOutput, error) {
    logger := workflow.GetLogger(ctx)

    // 1. Provision sandbox (unless reusing from group)
    var sandboxID string
    if input.SandboxID != "" {
        sandboxID = input.SandboxID
    } else {
        ao := workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute}
        err := workflow.ExecuteActivity(
            workflow.WithActivityOptions(ctx, ao),
            ProvisionSandbox, input,
        ).Get(ctx, &sandboxID)
        if err != nil {
            return nil, fmt.Errorf("provision sandbox: %w", err)
        }
    }

    // 2. Execute step (may loop for steer)
    var output *model.StepOutput
    prompt := input.ResolvedOpts.Prompt
    conversationHistory := ""

    for {
        ao := workflow.ActivityOptions{
            StartToCloseTimeout: 90 * time.Minute,
            HeartbeatTimeout:    2 * time.Minute,
        }
        err := workflow.ExecuteActivity(
            workflow.WithActivityOptions(ctx, ao),
            ExecuteStep, ExecuteStepInput{
                StepInput:           input,
                SandboxID:           sandboxID,
                Prompt:              prompt,
                ConversationHistory: conversationHistory,
            },
        ).Get(ctx, &output)
        if err != nil {
            return nil, err
        }

        // 3. Evaluate approval policy
        if !shouldPause(input.StepDef, output) {
            break
        }

        // 4. Signal: awaiting_input
        _ = workflow.ExecuteActivity(ctx, UpdateStepStatus, input.StepRunID, model.StepStatusAwaitingInput).Get(ctx, nil)

        // 5. Wait for signal
        var steerPayload SteerPayload
        selector := workflow.NewSelector(ctx)
        var approved, rejected, cancelled bool

        selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalApprove)), func(c workflow.ReceiveChannel, _ bool) {
            c.Receive(ctx, nil); approved = true
        })
        selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalReject)), func(c workflow.ReceiveChannel, _ bool) {
            c.Receive(ctx, nil); rejected = true
        })
        selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalSteer)), func(c workflow.ReceiveChannel, _ bool) {
            c.Receive(ctx, &steerPayload)
        })
        selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalCancel)), func(c workflow.ReceiveChannel, _ bool) {
            c.Receive(ctx, nil); cancelled = true
        })
        selector.Select(ctx)

        if approved {
            break
        }
        if rejected || cancelled {
            return &model.StepOutput{StepID: input.StepDef.ID, Status: model.StepStatusFailed,
                Error: "rejected by user"}, nil
        }
        // Steer: rebuild prompt with history and new instruction
        conversationHistory = fmt.Sprintf("%s\n\nPrevious attempt output:\n%s\n\nSteering instruction:\n%s",
            conversationHistory, output.Diff, steerPayload.Prompt)
        prompt = input.ResolvedOpts.Prompt
    }

    // 6. Create PR if transform mode
    if input.StepDef.Mode == "transform" && input.StepDef.PullRequest != nil {
        _ = workflow.ExecuteActivity(ctx, CreatePR, sandboxID, input).Get(ctx, &output.PRUrl)
    }

    // 7. Cleanup (unless sandbox_group — DAGWorkflow handles that)
    if input.StepDef.SandboxGroup == "" && input.SandboxID == "" {
        _ = workflow.ExecuteActivity(ctx, CleanupSandbox, sandboxID).Get(ctx, nil)
    }

    return output, nil
}

func shouldPause(def model.StepDef, output *model.StepOutput) bool {
    switch def.ApprovalPolicy {
    case "always":
        return true
    case "never", "":
        return false
    case "agent":
        v, _ := output.Output["needs_review"].(bool)
        return v
    case "on_changes":
        return output.Diff != ""
    }
    return false
}
```

**Step 2: Commit**
```bash
git add internal/workflow/step.go
git commit -m "feat: StepWorkflow with HITL signals"
```

### Task 8.2: DAGWorkflow

**Files:**
- Create: `internal/workflow/dag.go`
- Test: `internal/workflow/dag_test.go`

**Step 1: DAGWorkflow** (`internal/workflow/dag.go`)
```go
package workflow

import (
    "fmt"
    "time"

    "go.temporal.io/sdk/workflow"
    "github.com/tinkerloft/fleetlift/internal/model"
    "github.com/tinkerloft/fleetlift/internal/template"
)

type DAGInput struct {
    RunID      string
    WorkflowDef model.WorkflowDef
    Parameters  map[string]any
}

func DAGWorkflow(ctx workflow.Context, input DAGInput) error {
    logger := workflow.GetLogger(ctx)
    steps := input.WorkflowDef.Steps
    outputs := map[string]*model.StepOutput{}
    sandboxes := map[string]string{} // sandbox_group -> sandbox_id
    pending := make(map[string]model.StepDef, len(steps))
    for _, s := range steps {
        pending[s.ID] = s
    }

    for len(pending) > 0 {
        ready := findReady(pending, outputs)
        if len(ready) == 0 {
            return fmt.Errorf("DAG deadlock: circular dependency or all steps blocked")
        }

        // Provision sandbox groups for ready steps that need new sandboxes
        for _, step := range ready {
            if step.SandboxGroup != "" && sandboxes[step.SandboxGroup] == "" {
                ao := workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute}
                var sandboxID string
                err := workflow.ExecuteActivity(
                    workflow.WithActivityOptions(ctx, ao),
                    ProvisionSandbox, step,
                ).Get(ctx, &sandboxID)
                if err != nil {
                    return fmt.Errorf("provision sandbox group %s: %w", step.SandboxGroup, err)
                }
                sandboxes[step.SandboxGroup] = sandboxID
                logger.Info("provisioned sandbox group", "group", step.SandboxGroup, "sandbox_id", sandboxID)
            }
        }

        // Launch ready steps in parallel
        wg := workflow.NewWaitGroup(ctx)
        results := make([]*model.StepOutput, len(ready))

        for i, step := range ready {
            i, step := i, step
            wg.Add(1)
            workflow.Go(ctx, func(ctx workflow.Context) {
                defer wg.Done()

                // Resolve templates with current outputs + params
                resolved, err := resolveStep(step, input.Parameters, outputs)
                if err != nil {
                    results[i] = &model.StepOutput{StepID: step.ID, Status: model.StepStatusFailed,
                        Error: err.Error()}
                    return
                }

                // Check condition
                if step.Condition != "" && !evalCondition(step.Condition, input.Parameters, outputs) {
                    results[i] = &model.StepOutput{StepID: step.ID, Status: model.StepStatusSkipped}
                    return
                }

                // Action step — no sandbox
                if step.Action != nil {
                    results[i] = executeAction(ctx, step, resolved)
                    return
                }

                // Agent step — run as child StepWorkflow
                cwo := workflow.ChildWorkflowOptions{
                    WorkflowID: fmt.Sprintf("%s-%s", input.RunID, step.ID),
                }
                var out model.StepOutput
                err = workflow.ExecuteChildWorkflow(
                    workflow.WithChildOptions(ctx, cwo),
                    StepWorkflow,
                    StepInput{
                        RunID:        input.RunID,
                        StepDef:      step,
                        ResolvedOpts: resolved,
                        SandboxID:    sandboxes[step.SandboxGroup],
                    },
                ).Get(ctx, &out)
                if err != nil {
                    results[i] = &model.StepOutput{StepID: step.ID, Status: model.StepStatusFailed,
                        Error: err.Error()}
                    return
                }
                results[i] = &out
            })
        }
        wg.Wait(ctx)

        // Collect results
        for _, r := range results {
            outputs[r.StepID] = r
            delete(pending, r.StepID)

            if r.Status == model.StepStatusFailed && !isOptional(steps, r.StepID) {
                skipDownstream(pending, r.StepID, steps, outputs)
            }
        }
    }

    // Cleanup sandbox groups
    for group, sandboxID := range sandboxes {
        ao := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute}
        _ = workflow.ExecuteActivity(
            workflow.WithActivityOptions(ctx, ao),
            CleanupSandbox, sandboxID,
        ).Get(ctx, nil)
        logger.Info("cleaned up sandbox group", "group", group)
    }

    return nil
}

func findReady(pending map[string]model.StepDef, done map[string]*model.StepOutput) []model.StepDef {
    var ready []model.StepDef
    for _, step := range pending {
        allDone := true
        for _, dep := range step.DependsOn {
            if _, ok := done[dep]; !ok {
                allDone = false
                break
            }
        }
        if allDone {
            ready = append(ready, step)
        }
    }
    return ready
}
```

**Step 2: Write DAG topology tests**
```go
func TestFindReady(t *testing.T) {
    steps := map[string]model.StepDef{
        "a": {ID: "a", DependsOn: nil},
        "b": {ID: "b", DependsOn: []string{"a"}},
        "c": {ID: "c", DependsOn: []string{"a"}},
        "d": {ID: "d", DependsOn: []string{"b", "c"}},
    }
    done := map[string]*model.StepOutput{}

    ready := findReady(steps, done)
    assert.Len(t, ready, 1)
    assert.Equal(t, "a", ready[0].ID)

    done["a"] = &model.StepOutput{}
    ready = findReady(steps, done)
    assert.Len(t, ready, 2)
}
```

**Step 3: Run tests**
```bash
go test ./internal/workflow/...
```

**Step 4: Commit**
```bash
git add internal/workflow/dag.go internal/workflow/dag_test.go
git commit -m "feat: DAGWorkflow Temporal orchestrator"
```

---

## Phase 9: Activities

### Task 9.1: Core execution activities

**Files:**
- Create: `internal/activity/provision.go`
- Create: `internal/activity/execute.go`
- Create: `internal/activity/verify.go`
- Create: `internal/activity/collect.go`
- Create: `internal/activity/pr.go`
- Create: `internal/activity/status.go`

**Step 1: ProvisionSandbox** (`internal/activity/provision.go`)
```go
// ProvisionSandbox creates a sandbox and injects team credentials as env vars.
// Returns the sandbox ID.
func ProvisionSandbox(ctx context.Context, input workflow.StepInput, credStore CredentialStore) (string, error) {
    env := resolveCredentials(ctx, input.ResolvedOpts.Credentials, credStore, input.TeamID)
    env["GIT_USER_EMAIL"] = os.Getenv("GIT_USER_EMAIL")
    env["GIT_USER_NAME"] = os.Getenv("GIT_USER_NAME")

    return sandboxClient.Create(ctx, sandbox.CreateOpts{
        Image:       agentImage(input.ResolvedOpts.Agent),
        Env:         env,
        TimeoutMins: 120,
    })
}

func agentImage(agent string) string {
    switch agent {
    case "codex":
        return os.Getenv("CODEX_IMAGE")
    default: // claude-code
        return os.Getenv("AGENT_IMAGE") // default: sandbox with claude CLI installed
    }
}
```

**Step 2: ExecuteStep** (`internal/activity/execute.go`)

This is the core long-running activity. It:
1. Clones all repos into the sandbox
2. Runs the agent with streaming output
3. Heartbeats Temporal every 30s
4. Writes log lines to DB as they arrive
5. Extracts diff and structured output on completion

```go
func ExecuteStep(ctx context.Context, input ExecuteStepInput) (*model.StepOutput, error) {
    sb := sandboxClient // injected

    // 1. Clone repos
    for _, repo := range input.ResolvedOpts.Repos {
        cloneCmd := fmt.Sprintf("git clone --depth 50 %s /workspace/%s", repo.URL, repo.Name)
        if repo.Branch != "" {
            cloneCmd += fmt.Sprintf(" --branch %s", repo.Branch)
        }
        activity.RecordHeartbeat(ctx, "cloning "+repo.Name)
        updateStepStatus(ctx, input.StepRunID, model.StepStatusCloning)
        if _, _, err := sb.Exec(ctx, input.SandboxID, cloneCmd, "/"); err != nil {
            return nil, fmt.Errorf("clone %s: %w", repo.URL, err)
        }
    }

    // 2. Run agent with streaming output
    activity.RecordHeartbeat(ctx, "running agent")
    updateStepStatus(ctx, input.StepRunID, model.StepStatusRunning)

    runner := agentRunners[input.ResolvedOpts.Agent]
    events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
        Prompt:  input.Prompt,
        WorkDir: "/workspace",
    })
    if err != nil {
        return nil, err
    }

    var seq int64
    var lastOutput map[string]any
    for event := range events {
        activity.RecordHeartbeat(ctx, "agent running: "+event.Type)
        // Write log line to DB
        writeLogLine(ctx, input.StepRunID, seq, "stdout", event.Content)
        seq++
        if event.Type == "complete" {
            lastOutput = event.Output
        }
        if event.Type == "error" {
            return nil, fmt.Errorf("agent error: %s", event.Content)
        }
    }

    // 3. Extract git diff
    diff, _, _ := sb.Exec(ctx, input.SandboxID, "git -C /workspace diff", "/")

    // 4. Extract structured output from agent
    structured := extractStructuredOutput(lastOutput, input.StepDef.Execution)

    return &model.StepOutput{
        StepID: input.StepDef.ID,
        Status: model.StepStatusComplete,
        Output: structured,
        Diff:   diff,
    }, nil
}
```

**Step 3: Action activities** (`internal/activity/actions.go`)

Implement each action type from the catalog as a separate function:
```go
func ActionNotifySlack(ctx context.Context, config map[string]any, renderCtx template.RenderContext) error
func ActionGitHubPostReviewComment(ctx context.Context, config map[string]any, renderCtx template.RenderContext) error
func ActionGitHubAssignIssue(ctx context.Context, config map[string]any, renderCtx template.RenderContext) error
func ActionGitHubAddLabel(ctx context.Context, config map[string]any, renderCtx template.RenderContext) error
func ActionGitHubPostIssueComment(ctx context.Context, config map[string]any, renderCtx template.RenderContext) error
func ActionWebhook(ctx context.Context, config map[string]any, renderCtx template.RenderContext) error
```

Each function renders config values via `template.RenderPrompt`, then calls the relevant API.

**Step 4: Commit**
```bash
git add internal/activity/
git commit -m "feat: core execution activities"
```

---

## Phase 10: Worker

### Task 10.1: Register workflows and activities

**File:** `cmd/worker/main.go`

```go
func main() {
    c := client.New(os.Getenv("TEMPORAL_ADDRESS"))
    defer c.Close()

    db, _ := db.Connect(context.Background())
    sbClient := opensandbox.New(os.Getenv("OPENSANDBOX_DOMAIN"), os.Getenv("OPENSANDBOX_API_KEY"))
    credStore := credential.NewStore(db, os.Getenv("CREDENTIAL_ENCRYPTION_KEY"))

    acts := &activity.Activities{
        Sandbox:    sbClient,
        DB:         db,
        CredStore:  credStore,
        AgentRunners: map[string]agent.Runner{
            "claude-code": agent.NewClaudeCodeRunner(sbClient),
        },
    }

    w := worker.New(c, "fleetlift", worker.Options{})
    w.RegisterWorkflow(workflow.DAGWorkflow)
    w.RegisterWorkflow(workflow.StepWorkflow)
    w.RegisterActivity(acts)

    if err := w.Run(worker.InterruptCh()); err != nil {
        log.Fatal(err)
    }
}
```

**Step 2: Run worker locally**
```bash
make temporal-dev   # start Temporal
make build          # build binaries
./bin/fleetlift-worker
```

**Step 3: Commit**
```bash
git add cmd/worker/
git commit -m "feat: worker registration"
```

---

## Phase 11: API Server

### Task 11.1: Core REST API

**Files:**
- `cmd/server/main.go`
- `internal/server/router.go`
- `internal/server/handlers/auth.go`
- `internal/server/handlers/workflows.go`
- `internal/server/handlers/runs.go`
- `internal/server/handlers/inbox.go`
- `internal/server/handlers/reports.go`

**Step 1: Router** (`internal/server/router.go`)
```go
func NewRouter(deps Deps) http.Handler {
    r := chi.NewRouter()
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(cors.Handler(corsOptions()))

    // Auth (public)
    r.Get("/auth/github", deps.Auth.HandleGitHubRedirect)
    r.Get("/auth/github/callback", deps.Auth.HandleGitHubCallback)
    r.Post("/auth/refresh", deps.Auth.HandleRefresh)

    // Authenticated API
    r.Group(func(r chi.Router) {
        r.Use(auth.Middleware(deps.JWTSecret))

        // Workflows (templates)
        r.Get("/api/workflows", deps.Workflows.List)
        r.Get("/api/workflows/{id}", deps.Workflows.Get)
        r.Post("/api/workflows", deps.Workflows.Create)
        r.Put("/api/workflows/{id}", deps.Workflows.Update)
        r.Delete("/api/workflows/{id}", deps.Workflows.Delete)
        r.Post("/api/workflows/{id}/fork", deps.Workflows.Fork)

        // Runs
        r.Post("/api/runs", deps.Runs.Create)           // start a run
        r.Get("/api/runs", deps.Runs.List)
        r.Get("/api/runs/{id}", deps.Runs.Get)
        r.Get("/api/runs/{id}/logs", deps.Runs.Logs)
        r.Get("/api/runs/{id}/diff", deps.Runs.Diff)
        r.Get("/api/runs/{id}/output", deps.Runs.Output)
        r.Get("/api/runs/{id}/events", deps.Runs.Stream) // SSE
        r.Post("/api/runs/{id}/approve", deps.Runs.Approve)
        r.Post("/api/runs/{id}/reject", deps.Runs.Reject)
        r.Post("/api/runs/{id}/steer", deps.Runs.Steer)
        r.Post("/api/runs/{id}/cancel", deps.Runs.Cancel)

        // Inbox
        r.Get("/api/inbox", deps.Inbox.List)
        r.Post("/api/inbox/{id}/read", deps.Inbox.MarkRead)

        // Reports
        r.Get("/api/reports", deps.Reports.List)
        r.Get("/api/reports/{runID}", deps.Reports.Get)
        r.Get("/api/reports/{runID}/export", deps.Reports.Export)

        // Credentials
        r.Get("/api/credentials", deps.Credentials.List)
        r.Post("/api/credentials", deps.Credentials.Set)
        r.Delete("/api/credentials/{name}", deps.Credentials.Delete)
    })

    // Serve embedded React SPA
    r.Handle("/*", spaHandler())

    return r
}
```

**Step 2: SSE streaming handler** (`internal/server/handlers/runs.go`)
```go
func (h *RunsHandler) Stream(w http.ResponseWriter, r *http.Request) {
    runID := chi.URLParam(r, "id")
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", 500)
        return
    }

    var cursor int64
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            logs, newCursor, err := h.db.GetLogsSince(r.Context(), runID, cursor)
            if err != nil {
                continue
            }
            for _, log := range logs {
                fmt.Fprintf(w, "data: %s\n\n", mustJSON(log))
            }
            if len(logs) > 0 {
                cursor = newCursor
                flusher.Flush()
            }
            // Also send run/step status updates
            status, _ := h.db.GetRunStatus(r.Context(), runID)
            fmt.Fprintf(w, "event: status\ndata: %s\n\n", mustJSON(status))
            flusher.Flush()
        }
    }
}
```

**Step 3: Commit**
```bash
git add internal/server/ cmd/server/
git commit -m "feat: REST API server with SSE streaming"
```

---

## Phase 12: CLI

### Task 12.1: Command structure

**File:** `cmd/cli/main.go` and subcommand files

Implement all commands from design doc §13 using cobra. Key implementation notes:

- `fleetlift auth login` — opens browser to `/auth/github`, polls for token, writes to `~/.fleetlift/auth.json`
- `fleetlift run <workflow-id>` — `POST /api/runs`, polls for status, streams logs to terminal
- `fleetlift run logs <id>` — `GET /api/runs/{id}/events` SSE, prints to stdout
- `fleetlift inbox` — `GET /api/inbox`, table output
- `fleetlift run approve <id>` — `POST /api/runs/{id}/approve`, auto-targets active HITL step

All commands accept `--output json` for machine-readable output.

```bash
git add cmd/cli/
git commit -m "feat: CLI implementation"
```

---

## Phase 13: Web UI

### Task 13.1: Update React SPA

**Wipe existing pages, rebuild with new routes:**

```
/                          → redirect to /runs
/workflows                 → Workflow Library
/workflows/:id             → Workflow detail + DAG preview + Run button
/runs                      → Run list
/runs/:id                  → Run Detail (DAG live, logs, HITL panel)
/inbox                     → Inbox
/reports                   → Reports list
/reports/:runId            → Report viewer
```

**DAG visualisation:** use `reactflow` (already popular, MIT licensed) to render step nodes and edges. Node colour by status: grey=pending, blue=running, green=complete, red=failed, yellow=awaiting\_input. Active node pulses via CSS animation.

**Install reactflow:**
```bash
cd web && npm install reactflow
```

**Key components:**
- `<DAGGraph steps={steps} stepRuns={stepRuns} />` — renders the workflow DAG
- `<StepPanel stepRun={stepRun} />` — logs, diff, output for selected step
- `<HITLPanel stepRun={stepRun} onApprove onReject onSteer />` — approval controls
- `<LogStream stepRunId={id} />` — SSE-connected live log view
- `<ReportViewer runId={id} />` — collated report output

```bash
cd web && npm run build
git add web/
git commit -m "feat: web UI with DAG visualisation and run management"
```

---

## Phase 14: Integration Tests

### Task 14.1: End-to-end test with real Temporal + OpenSandbox

**File:** `tests/integration/dag_test.go`

```go
// +build integration

func TestBugFixWorkflow(t *testing.T) {
    // Requires: TEMPORAL_ADDRESS, OPENSANDBOX_DOMAIN, ANTHROPIC_API_KEY, GITHUB_TOKEN
    if os.Getenv("TEMPORAL_ADDRESS") == "" {
        t.Skip("integration env not set")
    }

    // Load bug-fix builtin template
    // Start a run with a test repo and a simple issue
    // Poll until awaiting_input
    // Approve
    // Assert PR URL is returned
}
```

---

## Phase 15: Linting, Tests, and Docs

**Step 1: Run full test suite**
```bash
go test ./...
```

**Step 2: Run linter**
```bash
make lint
```

**Step 3: Build all binaries**
```bash
make build
```

**Step 4: Fix any lint errors**

**Step 5: Update CLAUDE.md with new binary names and env vars**

**Step 6: Final commit**
```bash
git add -A
git commit -m "chore: lint fixes, full test pass, docs update"
```

---

## Environment Variables Reference

| Variable | Purpose | Default |
|---|---|---|
| `DATABASE_URL` | PostgreSQL DSN | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` |
| `TEMPORAL_ADDRESS` | Temporal server | `localhost:7233` |
| `OPENSANDBOX_DOMAIN` | OpenSandbox API | `http://localhost:8080` |
| `OPENSANDBOX_API_KEY` | OpenSandbox auth | — |
| `AGENT_IMAGE` | Default sandbox image (Claude Code) | — |
| `ANTHROPIC_API_KEY` | Injected into sandboxes via credentials | — |
| `GITHUB_TOKEN` | Injected into sandboxes via credentials | — |
| `JWT_SECRET` | Server JWT signing key | — |
| `CREDENTIAL_ENCRYPTION_KEY` | AES-256 key for credential store | — |
| `GITHUB_CLIENT_ID` | OAuth app client ID | — |
| `GITHUB_CLIENT_SECRET` | OAuth app client secret | — |
| `GIT_USER_EMAIL` | Git commit identity for agent | — |
| `GIT_USER_NAME` | Git commit identity for agent | — |
| `LOG_LEVEL` | Logging level | `info` |
| `METRICS_ADDR` | Prometheus metrics address | `:9090` |

---

## Local Dev Quick Start

```bash
# 1. Dependencies
make temporal-dev        # Temporal in Docker
make opensandbox-up      # OpenSandbox in Docker
make db-up               # PostgreSQL in Docker

# 2. Migrate
make db-migrate

# 3. Build
make build

# 4. Run worker
./bin/fleetlift-worker

# 5. Run server
./bin/fleetlift-server

# 6. Use CLI
./bin/fleetlift auth login
./bin/fleetlift workflow list
./bin/fleetlift run fleet-research \
  --param repos='[{"url":"https://github.com/example/repo"}]' \
  --param prompt="Find all TODO comments"
```
