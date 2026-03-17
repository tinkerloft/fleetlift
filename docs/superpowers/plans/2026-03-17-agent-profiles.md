# Agent Profile Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable fleetlift workflows to declare an `agent_profile` that installs plugins, skills, and MCPs into the Claude agent sandbox before execution, and support eval-time plugin injection via GitHub URL parameters.

**Architecture:** Agent profiles are stored in the DB, resolved at DAG dispatch time via a new Temporal activity, and materialised in `StepWorkflow` by running a generated pre-flight shell script (`claude plugin install`, `claude mcp add`). Eval plugins bypass the marketplace and are injected as `--plugin-dir` flags on the `claude` invocation. `RunPreflightInput/Output` live in the `workflow` package (alongside `StepInput`, `ResolvedStepOpts`) to avoid the existing `activity → workflow` import direction being reversed.

**Tech Stack:** Go, PostgreSQL (golang-migrate), Temporal SDK, `claude` CLI plugin/mcp subcommands, chi router

**Spec:** `docs/superpowers/specs/2026-03-17-agent-profile-design.md`

---

## Import Architecture

```
workflow  ←── activity  (activity imports workflow — this direction is established)
workflow  ←── agent     (agent types used by activity via workflow input structs)
```

Types shared between `workflow` and `activity` (input/output structs) live in **`workflow`**. Types private to `activity` (DB interfaces, implementations) live in `activity`.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `internal/db/migrations/004_agent_profiles.up.sql` | Create | `marketplaces` + `agent_profiles` tables; `agent_profile` column on `workflow_templates` |
| `internal/model/agent_profile.go` | Create | `Marketplace`, `AgentProfile`, `AgentProfileBody`, `PluginSource`, `SkillSource`, `MCPConfig`, `Header`, `MergeProfiles` |
| `internal/model/agent_profile_test.go` | Create | Validation + merge logic tests |
| `internal/model/workflow.go` | Modify | Add `AgentProfile string` to `WorkflowDef`; `EvalPlugins []string` to `ExecutionDef` |
| `internal/workflow/step.go` | Modify | Add `EffectiveProfile`, `EvalPluginURLs` to `ResolvedStepOpts`; `EvalPluginDirs` to `ExecuteStepInput`; add `RunPreflightInput`, `RunPreflightOutput` types |
| `internal/activity/profiles.go` | Create | `ProfileStore` interface; `ResolveAgentProfile` activity; `ResolveProfileInput` |
| `internal/activity/profiles_db.go` | Create | `DBProfileStore` — SQL implementation of `ProfileStore` |
| `internal/activity/profiles_test.go` | Create | Merge + resolution unit tests (using stub store) |
| `internal/activity/preflight.go` | Create | `RunPreflight` activity; `BuildPreflightScript`; `BuildEvalCloneCommands`; `parseGitHubTreeURL` |
| `internal/activity/preflight_test.go` | Create | Script generation tests |
| `internal/activity/activities.go` | Modify | Add `ProfileStore ProfileStore` field |
| `internal/activity/constants.go` | Modify | Add `ResolveAgentProfileActivity`, `RunPreflightActivity` constants |
| `internal/workflow/dag.go` | Modify | Call `ResolveAgentProfileActivity`; include MCP credentials in preflight |
| `internal/workflow/step.go` (StepWorkflow) | Modify | Call `RunPreflightActivity` after provision; pass `EvalPluginDirs` to `ExecuteStepInput` |
| `internal/agent/runner.go` | Modify | Add `EvalPluginDirs []string` to `RunOpts` |
| `internal/agent/claudecode.go` | Modify | Append `--plugin-dir` flags from `opts.EvalPluginDirs` |
| `internal/activity/execute.go` | Modify | Pass `EvalPluginDirs` from `ExecuteStepInput` into `agent.RunOpts` |
| `cmd/worker/main.go` | Modify | Register new activities; wire `DBProfileStore` into `Activities` |
| `internal/server/handlers/profiles.go` | Create | CRUD handlers: marketplaces + agent profiles |
| `internal/server/handlers/profiles_test.go` | Create | Handler tests |
| `internal/server/router.go` | Modify | Register `/api/marketplaces` and `/api/agent-profiles` routes |

---

## Chunk 1: Data Model + Migration

### Task 1: DB migration

**Files:**
- Create: `internal/db/migrations/004_agent_profiles.up.sql`

- [ ] Write migration

```sql
-- internal/db/migrations/004_agent_profiles.up.sql

CREATE TABLE marketplaces (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    repo_url    TEXT NOT NULL,
    credential  TEXT NOT NULL DEFAULT '',  -- CredStore name; empty = public repo
    team_id     TEXT REFERENCES teams(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Unique name per team; system-wide names also unique (NULL != NULL in postgres partial index)
CREATE UNIQUE INDEX marketplaces_team_name_idx   ON marketplaces (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX marketplaces_system_name_idx ON marketplaces (name)          WHERE team_id IS NULL;

CREATE TABLE agent_profiles (
    id          TEXT PRIMARY KEY,
    team_id     TEXT REFERENCES teams(id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    body        JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX agent_profiles_team_name_idx   ON agent_profiles (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX agent_profiles_system_name_idx ON agent_profiles (name)          WHERE team_id IS NULL;

-- Seed the empty system baseline (no plugins/skills/mcps — safe starting point)
INSERT INTO agent_profiles (id, name, description, body)
VALUES ('system-baseline', 'baseline', 'System-wide baseline profile',
        '{"plugins":[],"skills":[],"mcps":[]}');

ALTER TABLE workflow_templates ADD COLUMN agent_profile TEXT;
```

- [ ] Apply migration and verify

```bash
docker compose up -d
go run ./cmd/server &   # migrations run on startup
psql $DATABASE_URL -c "\d marketplaces"
psql $DATABASE_URL -c "\d agent_profiles"
psql $DATABASE_URL -c "SELECT id, name FROM agent_profiles;"
# Expected: one row — id=system-baseline, name=baseline
pkill -f "go run ./cmd/server"
```

- [ ] Commit

```bash
git add internal/db/migrations/004_agent_profiles.up.sql
git commit -m "feat: migration — marketplaces and agent_profiles tables"
```

---

### Task 2: Model types + merge logic

**Files:**
- Create: `internal/model/agent_profile.go`
- Create: `internal/model/agent_profile_test.go`

- [ ] Write failing tests

```go
// internal/model/agent_profile_test.go
package model_test

import (
	"encoding/json"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestPluginSourceValidate_MarketplaceSource(t *testing.T) {
	p := model.PluginSource{Plugin: "plugins/miro-helm-doctor"}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestPluginSourceValidate_GitHubURLSource(t *testing.T) {
	p := model.PluginSource{GitHubURL: "https://github.com/org/repo/tree/main/plugins/foo"}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestPluginSourceValidate_BothSet(t *testing.T) {
	p := model.PluginSource{Plugin: "plugins/foo", GitHubURL: "https://github.com/org/repo"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error when both fields set")
	}
}

func TestPluginSourceValidate_NeitherSet(t *testing.T) {
	p := model.PluginSource{}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error when neither field set")
	}
}

func TestPluginSourceValidate_RejectsNonHTTPS(t *testing.T) {
	p := model.PluginSource{GitHubURL: "git://github.com/org/repo"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for non-https scheme")
	}
}

func TestAgentProfileBodyJSONRoundTrip(t *testing.T) {
	orig := model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo"}},
		MCPs:    []model.MCPConfig{{Name: "my-mcp", Type: "remote", Transport: "sse", URL: "https://mcp.example.com/sse"}},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got model.AgentProfileBody
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Plugin != "plugins/foo" {
		t.Errorf("plugins mismatch: %+v", got.Plugins)
	}
}

func TestMergeProfiles_Accumulates(t *testing.T) {
	baseline := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/base"}},
		MCPs:    []model.MCPConfig{{Name: "base-mcp"}},
	}
	wp := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/extra"}},
	}
	merged := model.MergeProfiles(baseline, wp)
	if len(merged.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(merged.Plugins))
	}
	if len(merged.MCPs) != 1 {
		t.Errorf("expected 1 MCP from baseline, got %d", len(merged.MCPs))
	}
}

func TestMergeProfiles_LaterLayerWins(t *testing.T) {
	baseline := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo", Marketplace: "old"}},
	}
	wp := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo", Marketplace: "new"}},
	}
	merged := model.MergeProfiles(baseline, wp)
	if len(merged.Plugins) != 1 {
		t.Errorf("expected dedup to 1 plugin, got %d", len(merged.Plugins))
	}
	if merged.Plugins[0].Marketplace != "new" {
		t.Errorf("expected later layer to win, got marketplace=%q", merged.Plugins[0].Marketplace)
	}
}

func TestMergeProfiles_NilBaseline(t *testing.T) {
	wp := &model.AgentProfileBody{Plugins: []model.PluginSource{{Plugin: "plugins/foo"}}}
	merged := model.MergeProfiles(nil, wp)
	if len(merged.Plugins) != 1 {
		t.Errorf("expected 1 plugin from workflow layer, got %d", len(merged.Plugins))
	}
}

func TestMergeProfiles_BothNil(t *testing.T) {
	merged := model.MergeProfiles(nil, nil)
	if len(merged.Plugins) != 0 || len(merged.MCPs) != 0 {
		t.Errorf("expected empty merged profile, got: %+v", merged)
	}
}
```

- [ ] Run tests — confirm fail

```bash
go test ./internal/model/... -run "TestPlugin|TestAgentProfile|TestMerge" -v
# Expected: FAIL — types not defined
```

- [ ] Implement `internal/model/agent_profile.go`

```go
package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Marketplace is a GitHub repository that hosts plugins and skills.
type Marketplace struct {
	ID         string    `db:"id"         json:"id"`
	Name       string    `db:"name"       json:"name"`
	RepoURL    string    `db:"repo_url"   json:"repo_url"`
	Credential string    `db:"credential" json:"credential"` // CredStore name; empty = public
	TeamID     *string   `db:"team_id"    json:"team_id,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

// AgentProfile is the DB row. Body is stored as JSONB.
type AgentProfile struct {
	ID          string           `db:"id"          json:"id"`
	TeamID      *string          `db:"team_id"     json:"team_id,omitempty"`
	Name        string           `db:"name"        json:"name"`
	Description string           `db:"description" json:"description"`
	Body        AgentProfileBody `db:"body"        json:"body"`
	CreatedAt   time.Time        `db:"created_at"  json:"created_at"`
	UpdatedAt   time.Time        `db:"updated_at"  json:"updated_at"`
}

// AgentProfileBody is the JSONB payload — the list of plugins, skills, and MCPs.
type AgentProfileBody struct {
	Plugins []PluginSource `json:"plugins,omitempty"`
	Skills  []SkillSource  `json:"skills,omitempty"`
	MCPs    []MCPConfig    `json:"mcps,omitempty"`
}

// PluginSource identifies a plugin from a marketplace or a direct GitHub URL.
// Exactly one of Plugin (marketplace lookup) or GitHubURL (eval/direct) must be set.
// GitHubURL must use https:// scheme.
type PluginSource struct {
	Marketplace string `json:"marketplace,omitempty"` // registered marketplace name; omit = default
	Plugin      string `json:"plugin,omitempty"`      // path within repo, e.g. "plugins/miro-helm-doctor"
	GitHubURL   string `json:"github_url,omitempty"`  // direct URL, bypasses marketplace
}

func (p PluginSource) Validate() error {
	hasPlugin := p.Plugin != ""
	hasURL := p.GitHubURL != ""
	if hasPlugin && hasURL {
		return errors.New("plugin_source: only one of plugin or github_url may be set")
	}
	if !hasPlugin && !hasURL {
		return errors.New("plugin_source: one of plugin or github_url must be set")
	}
	if hasURL && !strings.HasPrefix(p.GitHubURL, "https://") {
		return fmt.Errorf("plugin_source: github_url must use https:// scheme, got %q", p.GitHubURL)
	}
	return nil
}

// DeduplicationKey returns the merge dedup key (plugin path or URL).
func (p PluginSource) DeduplicationKey() string {
	if p.GitHubURL != "" {
		return "url:" + p.GitHubURL
	}
	return "plugin:" + p.Plugin
}

// SkillSource identifies a standalone skill (not bundled in a plugin).
// Same one-of invariant as PluginSource.
type SkillSource struct {
	Marketplace string `json:"marketplace,omitempty"`
	Skill       string `json:"skill,omitempty"`
	GitHubURL   string `json:"github_url,omitempty"`
}

func (s SkillSource) Validate() error {
	hasSkill := s.Skill != ""
	hasURL := s.GitHubURL != ""
	if hasSkill && hasURL {
		return errors.New("skill_source: only one of skill or github_url may be set")
	}
	if !hasSkill && !hasURL {
		return errors.New("skill_source: one of skill or github_url must be set")
	}
	if hasURL && !strings.HasPrefix(s.GitHubURL, "https://") {
		return fmt.Errorf("skill_source: github_url must use https:// scheme, got %q", s.GitHubURL)
	}
	return nil
}

func (s SkillSource) DeduplicationKey() string {
	if s.GitHubURL != "" {
		return "url:" + s.GitHubURL
	}
	return "skill:" + s.Skill
}

// MCPConfig declares a remote MCP server to register in the sandbox.
type MCPConfig struct {
	Name        string   `json:"name"`                  // logical name, key in .claude.json
	Type        string   `json:"type"`                  // "remote"
	Transport   string   `json:"transport"`             // "http" or "sse"
	URL         string   `json:"url"`                   // endpoint
	Headers     []Header `json:"headers,omitempty"`
	Credentials []string `json:"credentials,omitempty"` // CredStore names injected as env vars
}

// Header is an HTTP header name/value pair. Value may contain ${ENV_VAR} references.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MergeProfiles merges baseline and workflow profile layers.
// Plugins and skills deduplicate by path/URL key; later layer wins on conflict.
// MCPs deduplicate by Name. Either argument may be nil.
func MergeProfiles(baseline, workflowProfile *AgentProfileBody) AgentProfileBody {
	result := AgentProfileBody{}

	seen := map[string]int{}
	for _, p := range bodyPlugins(baseline) {
		k := p.DeduplicationKey()
		seen[k] = len(result.Plugins)
		result.Plugins = append(result.Plugins, p)
	}
	for _, p := range bodyPlugins(workflowProfile) {
		k := p.DeduplicationKey()
		if i, ok := seen[k]; ok {
			result.Plugins[i] = p
		} else {
			seen[k] = len(result.Plugins)
			result.Plugins = append(result.Plugins, p)
		}
	}

	seenS := map[string]int{}
	for _, s := range bodySkills(baseline) {
		k := s.DeduplicationKey()
		seenS[k] = len(result.Skills)
		result.Skills = append(result.Skills, s)
	}
	for _, s := range bodySkills(workflowProfile) {
		k := s.DeduplicationKey()
		if i, ok := seenS[k]; ok {
			result.Skills[i] = s
		} else {
			seenS[k] = len(result.Skills)
			result.Skills = append(result.Skills, s)
		}
	}

	seenM := map[string]int{}
	for _, m := range bodyMCPs(baseline) {
		seenM[m.Name] = len(result.MCPs)
		result.MCPs = append(result.MCPs, m)
	}
	for _, m := range bodyMCPs(workflowProfile) {
		if i, ok := seenM[m.Name]; ok {
			result.MCPs[i] = m
		} else {
			seenM[m.Name] = len(result.MCPs)
			result.MCPs = append(result.MCPs, m)
		}
	}

	return result
}

func bodyPlugins(b *AgentProfileBody) []PluginSource {
	if b == nil {
		return nil
	}
	return b.Plugins
}

func bodySkills(b *AgentProfileBody) []SkillSource {
	if b == nil {
		return nil
	}
	return b.Skills
}

func bodyMCPs(b *AgentProfileBody) []MCPConfig {
	if b == nil {
		return nil
	}
	return b.MCPs
}
```

- [ ] Run tests — confirm pass

```bash
go test ./internal/model/... -run "TestPlugin|TestAgentProfile|TestMerge" -v
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/model/agent_profile.go internal/model/agent_profile_test.go
git commit -m "feat: AgentProfile model types with merge logic"
```

---

### Task 3: WorkflowDef and step.go additions

**Files:**
- Modify: `internal/model/workflow.go`
- Modify: `internal/workflow/step.go`

- [ ] Add `AgentProfile` to `WorkflowDef` in `internal/model/workflow.go`

In `WorkflowDef`, after the `Steps` field:
```go
AgentProfile string `yaml:"agent_profile,omitempty"`
```

- [ ] Add `EvalPlugins` to `ExecutionDef` in `internal/model/workflow.go`

In `ExecutionDef`, after the `Output` field:
```go
EvalPlugins []string `yaml:"eval_plugins,omitempty"`
```

- [ ] Add profile fields to `ResolvedStepOpts` in `internal/workflow/step.go`

In `ResolvedStepOpts`, after the `Agent` field:
```go
// EffectiveProfile is the resolved merged agent profile for this step's sandbox.
// nil means no profile was declared — pre-flight will be skipped.
// New optional field: in-flight workflows without it will skip pre-flight correctly.
EffectiveProfile *model.AgentProfileBody `json:"effective_profile,omitempty"`
EvalPluginURLs   []string                `json:"eval_plugin_urls,omitempty"`
```

- [ ] Add `EvalPluginDirs` to `ExecuteStepInput` in `internal/workflow/step.go`

In `ExecuteStepInput`, after `ConversationHistory`:
```go
// EvalPluginDirs holds local sandbox paths cloned from EvalPluginURLs by RunPreflightActivity.
EvalPluginDirs []string `json:"eval_plugin_dirs,omitempty"`
```

- [ ] Add `RunPreflightInput` and `RunPreflightOutput` to `internal/workflow/step.go`

These live in the `workflow` package (not `activity`) to avoid reversing the import direction.

```go
// RunPreflightInput is the input to the RunPreflightActivity.
// MarketplaceURL is read by the activity from FLEETLIFT_MARKETPLACE_URL env var —
// not included here to keep workflow functions free of os.Getenv calls.
type RunPreflightInput struct {
	SandboxID      string                 `json:"sandbox_id"`
	TeamID         string                 `json:"team_id"`
	Profile        model.AgentProfileBody `json:"profile"`
	EvalPluginURLs []string               `json:"eval_plugin_urls,omitempty"`
}

// RunPreflightOutput is the output of RunPreflightActivity.
type RunPreflightOutput struct {
	// EvalPluginDirs contains the local sandbox paths of cloned eval plugins,
	// in the same order as RunPreflightInput.EvalPluginURLs.
	EvalPluginDirs []string `json:"eval_plugin_dirs,omitempty"`
}
```

- [ ] Add activity name constants to `internal/workflow/step.go` alongside the existing `var` block (lines 60–72)

```go
// Add to the existing var block in step.go:
RunPreflightActivity        = "RunPreflight"
ResolveAgentProfileActivity = "ResolveAgentProfile"
```

These constants live in the `workflow` package (not `activity`) because `dag.go` and `step.go` reference them unqualified — and `workflow` cannot import `activity`.

- [ ] Build to verify compilation

```bash
go build ./...
# Expected: no errors
```

- [ ] Run existing tests

```bash
go test ./internal/model/... ./internal/workflow/...
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/model/workflow.go internal/workflow/step.go
git commit -m "feat: add AgentProfile/EvalPlugins to WorkflowDef, RunPreflight types and constants to workflow package"
```

---

## Chunk 2: Profile Resolution Activity + DB Store

### Task 4: ProfileStore interface + ResolveAgentProfile activity

**Files:**
- Create: `internal/activity/profiles.go`
- Create: `internal/activity/profiles_db.go`
- Create: `internal/activity/profiles_test.go`
- Modify: `internal/activity/activities.go`

- [ ] Write failing tests

```go
// internal/activity/profiles_test.go
package activity_test

import (
	"context"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// stubProfileStore satisfies activity.ProfileStore for tests.
type stubProfileStore struct {
	// key: "team:<teamID>:<name>" or "system:<name>"
	profiles map[string]*model.AgentProfile
}

func (s *stubProfileStore) GetProfile(ctx context.Context, teamID, name string) (*model.AgentProfile, error) {
	if p, ok := s.profiles["team:"+teamID+":"+name]; ok {
		return p, nil
	}
	if p, ok := s.profiles["system:"+name]; ok {
		return p, nil
	}
	return nil, nil
}

func TestResolveProfile_BaselineOnly(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{
		"system:baseline": {Body: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/base"}},
		}},
	}}
	acts := &activity.Activities{ProfileStore: store}
	result, err := acts.ResolveAgentProfile(context.Background(), activity.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plugins) != 1 || result.Plugins[0].Plugin != "plugins/base" {
		t.Errorf("unexpected plugins: %+v", result.Plugins)
	}
}

func TestResolveProfile_WorkflowProfileMerged(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{
		"system:baseline": {Body: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/base"}},
		}},
		"system:helm-auditor": {Body: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/miro-helm-doctor"}},
		}},
	}}
	acts := &activity.Activities{ProfileStore: store}
	result, err := acts.ResolveAgentProfile(context.Background(), activity.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "helm-auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d: %+v", len(result.Plugins), result.Plugins)
	}
}

func TestResolveProfile_TeamScopedWinsOverSystem(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{
		"system:baseline": {Body: model.AgentProfileBody{
			MCPs: []model.MCPConfig{{Name: "system-mcp"}},
		}},
		"team:team-1:baseline": {Body: model.AgentProfileBody{
			MCPs: []model.MCPConfig{{Name: "team-mcp"}},
		}},
	}}
	acts := &activity.Activities{ProfileStore: store}
	result, err := acts.ResolveAgentProfile(context.Background(), activity.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPs) != 1 || result.MCPs[0].Name != "team-mcp" {
		t.Errorf("expected team-scoped baseline to win, got: %+v", result.MCPs)
	}
}

func TestResolveProfile_MissingProfileErrors(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{}}
	acts := &activity.Activities{ProfileStore: store}
	_, err := acts.ResolveAgentProfile(context.Background(), activity.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}
```

- [ ] Run tests — confirm fail

```bash
go test ./internal/activity/... -run TestResolveProfile -v
# Expected: FAIL
```

- [ ] Add `ProfileStore` to `Activities` in `internal/activity/activities.go`

```go
// In the Activities struct:
ProfileStore ProfileStore
```

- [ ] Implement `internal/activity/profiles.go`

```go
package activity

import (
	"context"
	"fmt"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// ProfileStore is the DB interface for agent profile lookups.
// GetProfile returns the team-scoped profile with the given name if it exists,
// otherwise the system-wide profile. Returns nil, nil if neither exists.
type ProfileStore interface {
	GetProfile(ctx context.Context, teamID, name string) (*model.AgentProfile, error)
}

// ResolveProfileInput is the input to ResolveAgentProfile.
type ResolveProfileInput struct {
	TeamID      string `json:"team_id"`
	ProfileName string `json:"profile_name"` // empty = baseline only
}

// ResolveAgentProfile resolves and merges the effective profile for a workflow run.
// It merges the system/team baseline with the named workflow profile (if any).
func (a *Activities) ResolveAgentProfile(ctx context.Context, input ResolveProfileInput) (model.AgentProfileBody, error) {
	baseline, err := a.ProfileStore.GetProfile(ctx, input.TeamID, "baseline")
	if err != nil {
		return model.AgentProfileBody{}, fmt.Errorf("fetch baseline profile: %w", err)
	}
	var baselineBody *model.AgentProfileBody
	if baseline != nil {
		baselineBody = &baseline.Body
	}

	name := input.ProfileName
	if name == "" || name == "baseline" {
		return model.MergeProfiles(baselineBody, nil), nil
	}

	wp, err := a.ProfileStore.GetProfile(ctx, input.TeamID, name)
	if err != nil {
		return model.AgentProfileBody{}, fmt.Errorf("fetch profile %q: %w", name, err)
	}
	if wp == nil {
		return model.AgentProfileBody{}, fmt.Errorf("agent profile %q not found for team %s", name, input.TeamID)
	}
	return model.MergeProfiles(baselineBody, &wp.Body), nil
}
```

- [ ] Implement `internal/activity/profiles_db.go`

```go
package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// DBProfileStore is the SQL implementation of ProfileStore.
type DBProfileStore struct {
	DB *sqlx.DB
}

// GetProfile returns the team-scoped profile first, then system-wide.
// Returns nil, nil if neither exists.
func (s *DBProfileStore) GetProfile(ctx context.Context, teamID, name string) (*model.AgentProfile, error) {
	// Try team-scoped first
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, team_id, name, description, body, created_at, updated_at
		   FROM agent_profiles
		  WHERE team_id = $1 AND name = $2`,
		teamID, name,
	)
	p, err := scanProfile(row)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query team profile: %w", err)
	}
	if p != nil {
		return p, nil
	}

	// Fall back to system-wide
	row = s.DB.QueryRowContext(ctx,
		`SELECT id, team_id, name, description, body, created_at, updated_at
		   FROM agent_profiles
		  WHERE team_id IS NULL AND name = $1`,
		name,
	)
	p, err = scanProfile(row)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query system profile: %w", err)
	}
	return p, nil
}

func scanProfile(row *sql.Row) (*model.AgentProfile, error) {
	var p model.AgentProfile
	var bodyJSON []byte
	err := row.Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &bodyJSON, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bodyJSON, &p.Body); err != nil {
		return nil, fmt.Errorf("unmarshal profile body: %w", err)
	}
	return &p, nil
}
```

- [ ] Run tests — confirm pass

```bash
go test ./internal/activity/... -run TestResolveProfile -v
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/activity/profiles.go internal/activity/profiles_db.go \
        internal/activity/profiles_test.go internal/activity/activities.go
git commit -m "feat: ResolveAgentProfile activity, ProfileStore interface, DBProfileStore"
```

---

### Task 5: Wire ResolveAgentProfileActivity into DAGWorkflow

**Files:**
- Modify: `internal/workflow/dag.go`

- [ ] Read `dag.go` lines 90–200 to find the credential preflight block and step loop

```bash
# Read the full dag.go to understand the structure before modifying
```

- [ ] In `DAGWorkflow`, resolve the agent profile **before** the credential preflight block (so MCP credentials from the profile can be included in the preflight check). Add after the initial `steps` variable assignment:

```go
// Resolve agent profile — must happen before credential preflight so MCP
// credentials are included in the validation pass.
var effectiveProfile model.AgentProfileBody
{
	ao := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
	if err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, ao),
		ResolveAgentProfileActivity,
		activity.ResolveProfileInput{
			TeamID:      input.TeamID,
			ProfileName: input.WorkflowDef.AgentProfile,
		},
	).Get(ctx, &effectiveProfile); err != nil {
		return fmt.Errorf("resolve agent profile: %w", err)
	}
}
```

- [ ] In the credential preflight block, after collecting step credentials, also collect MCP credentials from the resolved profile:

```go
// Add MCP credentials from the effective profile
for _, mcp := range effectiveProfile.MCPs {
	for _, credName := range mcp.Credentials {
		if _, ok := seen[credName]; !ok {
			seen[credName] = struct{}{}
			allCreds = append(allCreds, credName)
		}
	}
}
```

- [ ] When building `ResolvedStepOpts` for each step (find where this happens in the step dispatch loop), set the profile and render eval plugin URLs:

```go
resolvedOpts.EffectiveProfile = &effectiveProfile

// Render eval_plugins template values for this step
if step.Execution != nil {
	for _, rawURL := range step.Execution.EvalPlugins {
		rendered, err := renderTemplate(rawURL, templateData)
		if err != nil {
			return fmt.Errorf("render eval_plugin for step %q: %w", step.ID, err)
		}
		resolvedOpts.EvalPluginURLs = append(resolvedOpts.EvalPluginURLs, rendered)
	}
}
```

Note: `renderTemplate` is the existing template-rendering helper used for `Prompt` — use the same one.

- [ ] Build

```bash
go build ./...
```

- [ ] Run workflow tests

```bash
go test ./internal/workflow/... -v
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/workflow/dag.go
git commit -m "feat: resolve agent profile in DAGWorkflow, thread to step opts"
```

---

## Chunk 3: Pre-flight Script + Sandbox Wiring

### Task 6: Pre-flight script builder

**Files:**
- Create: `internal/activity/preflight.go`
- Create: `internal/activity/preflight_test.go`

- [ ] Write failing tests

```go
// internal/activity/preflight_test.go
package activity_test

import (
	"strings"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestBuildPreflightScript_EmptyProfile(t *testing.T) {
	script := activity.BuildPreflightScript(model.AgentProfileBody{}, "")
	if strings.Contains(script, "claude plugin install") {
		t.Error("expected no plugin install for empty profile")
	}
	if strings.Contains(script, "claude mcp add") {
		t.Error("expected no mcp add for empty profile")
	}
}

func TestBuildPreflightScript_WithMarketplacePlugin(t *testing.T) {
	profile := model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/miro-helm-doctor"}},
	}
	script := activity.BuildPreflightScript(profile, "https://github.com/miroapp-dev/claude-marketplace.git")
	if !strings.Contains(script, "claude plugin marketplace add") {
		t.Error("expected marketplace add command")
	}
	if !strings.Contains(script, "claude plugin install miro-helm-doctor") {
		t.Error("expected plugin install for 'miro-helm-doctor'")
	}
	// remove before install for idempotency
	if !strings.Contains(script, "claude plugin uninstall miro-helm-doctor") {
		t.Error("expected plugin uninstall before install")
	}
}

func TestBuildPreflightScript_GitHubURLPluginSkipped(t *testing.T) {
	// GitHubURL plugins go through --plugin-dir, not claude plugin install
	profile := model.AgentProfileBody{
		Plugins: []model.PluginSource{{GitHubURL: "https://github.com/org/repo/tree/main/plugins/foo"}},
	}
	script := activity.BuildPreflightScript(profile, "")
	if strings.Contains(script, "claude plugin install") {
		t.Error("GitHubURL plugins must not be installed via marketplace")
	}
}

func TestBuildPreflightScript_WithMCP(t *testing.T) {
	profile := model.AgentProfileBody{
		MCPs: []model.MCPConfig{
			{Name: "my-mcp", Transport: "sse", URL: "https://mcp.example.com/sse"},
		},
	}
	script := activity.BuildPreflightScript(profile, "")
	if !strings.Contains(script, "claude mcp remove my-mcp") {
		t.Error("expected mcp remove before add")
	}
	if !strings.Contains(script, "claude mcp add --transport sse --scope user my-mcp") {
		t.Error("expected mcp add command")
	}
}

func TestBuildPreflightScript_MCPWithHeader(t *testing.T) {
	profile := model.AgentProfileBody{
		MCPs: []model.MCPConfig{{
			Name:      "auth-mcp",
			Transport: "http",
			URL:       "https://mcp.example.com",
			Headers:   []model.Header{{Name: "Authorization", Value: "Bearer ${MY_TOKEN}"}},
		}},
	}
	script := activity.BuildPreflightScript(profile, "")
	if !strings.Contains(script, `--header`) {
		t.Errorf("expected --header in script, got:\n%s", script)
	}
	if !strings.Contains(script, "Authorization: Bearer ${MY_TOKEN}") {
		t.Errorf("expected header value in script, got:\n%s", script)
	}
}

func TestBuildEvalCloneCommands_RejectsNonHTTPS(t *testing.T) {
	_, err := activity.BuildEvalCloneCommands([]string{"git://github.com/org/repo"})
	if err == nil {
		t.Fatal("expected error for non-https eval plugin URL")
	}
}

func TestBuildEvalCloneCommands_ProducesGitClone(t *testing.T) {
	cmds, err := activity.BuildEvalCloneCommands([]string{
		"https://github.com/org/repo/tree/main/plugins/miro-helm-doctor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0], "git clone") {
		t.Error("expected git clone in command")
	}
	if !strings.Contains(cmds[0], "sparse-checkout set plugins/miro-helm-doctor") {
		t.Error("expected sparse-checkout targeting the plugin subpath")
	}
}

func TestParseGitHubTreeURL(t *testing.T) {
	repoURL, subPath, err := activity.ParseGitHubTreeURL(
		"https://github.com/org/repo/tree/main/plugins/foo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if repoURL != "https://github.com/org/repo.git" {
		t.Errorf("unexpected repoURL: %q", repoURL)
	}
	if subPath != "plugins/foo" {
		t.Errorf("unexpected subPath: %q", subPath)
	}
}
```

- [ ] Run tests — confirm fail

```bash
go test ./internal/activity/... -run "TestBuildPreflight|TestBuildEval|TestParseGitHub" -v
# Expected: FAIL
```

- [ ] Implement `internal/activity/preflight.go`

```go
package activity

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// RunPreflight installs plugins and MCPs from the effective profile, then clones
// any eval plugin URLs into the sandbox. Returns local sandbox paths for --plugin-dir.
func (a *Activities) RunPreflight(ctx context.Context, input workflow.RunPreflightInput) (workflow.RunPreflightOutput, error) {
	// Read marketplace URL from env (safe in activities — outside Temporal determinism sandbox)
	marketplaceURL := os.Getenv("FLEETLIFT_MARKETPLACE_URL")

	// Inject GITHUB_TOKEN from CredStore if marketplace credential is configured
	if credName := os.Getenv("FLEETLIFT_MARKETPLACE_CREDENTIAL"); credName != "" && a.CredStore != nil {
		token, err := a.CredStore.Get(ctx, input.TeamID, credName)
		if err != nil {
			return workflow.RunPreflightOutput{}, fmt.Errorf("fetch marketplace credential: %w", err)
		}
		// Inject into sandbox so the pre-flight script can use ${GITHUB_TOKEN}
		if _, _, err := a.Sandbox.Exec(ctx, input.SandboxID,
			fmt.Sprintf("export GITHUB_TOKEN=%s", shellquote.Quote(token)), "/"); err != nil {
			return workflow.RunPreflightOutput{}, fmt.Errorf("inject GITHUB_TOKEN: %w", err)
		}
	}

	script := BuildPreflightScript(input.Profile, marketplaceURL)
	if script != "" {
		if _, stderr, err := a.Sandbox.Exec(ctx, input.SandboxID, script, "/"); err != nil {
			return workflow.RunPreflightOutput{}, fmt.Errorf("pre-flight script: %w\nstderr: %s", err, stderr)
		}
	}

	if len(input.EvalPluginURLs) == 0 {
		return workflow.RunPreflightOutput{}, nil
	}

	cloneCmds, err := BuildEvalCloneCommands(input.EvalPluginURLs)
	if err != nil {
		return workflow.RunPreflightOutput{}, fmt.Errorf("build eval clone commands: %w", err)
	}

	var dirs []string
	for i, cmd := range cloneCmds {
		if _, stderr, err := a.Sandbox.Exec(ctx, input.SandboxID, cmd, "/"); err != nil {
			return workflow.RunPreflightOutput{}, fmt.Errorf("clone eval plugin %d: %w\nstderr: %s", i, err, stderr)
		}
		dirs = append(dirs, fmt.Sprintf("/tmp/eval-plugin-%d", i))
	}

	return workflow.RunPreflightOutput{EvalPluginDirs: dirs}, nil
}

// BuildPreflightScript generates the shell script for plugin and MCP setup.
// Exported for testing. marketplaceURL may be empty if no marketplace plugins present.
func BuildPreflightScript(profile model.AgentProfileBody, marketplaceURL string) string {
	var b strings.Builder

	// Marketplace auth + registration (only if there are marketplace plugins)
	hasMarketplacePlugins := false
	for _, p := range profile.Plugins {
		if p.Plugin != "" {
			hasMarketplacePlugins = true
			break
		}
	}
	if hasMarketplacePlugins && marketplaceURL != "" {
		b.WriteString("git config --global credential.helper store\n")
		b.WriteString("echo \"https://x-access-token:${GITHUB_TOKEN}@github.com\" > ~/.git-credentials\n")
		b.WriteString(fmt.Sprintf("claude plugin marketplace add %s\n", shellquote.Quote(marketplaceURL)))
	}

	// Install marketplace plugins (GitHubURL plugins are handled via --plugin-dir)
	for _, p := range profile.Plugins {
		if p.Plugin == "" {
			continue
		}
		pluginName := path.Base(p.Plugin)
		b.WriteString(fmt.Sprintf("claude plugin uninstall %s 2>/dev/null || true\n", shellquote.Quote(pluginName)))
		b.WriteString(fmt.Sprintf("claude plugin install %s\n", shellquote.Quote(pluginName)))
	}

	// Register MCPs (remove first for idempotency)
	for _, mcp := range profile.MCPs {
		b.WriteString(fmt.Sprintf("claude mcp remove %s 2>/dev/null || true\n", shellquote.Quote(mcp.Name)))
		transport := mcp.Transport
		if transport == "" {
			transport = "sse"
		}
		cmd := fmt.Sprintf("claude mcp add --transport %s --scope user %s %s",
			shellquote.Quote(transport),
			shellquote.Quote(mcp.Name),
			shellquote.Quote(mcp.URL),
		)
		for _, h := range mcp.Headers {
			cmd += fmt.Sprintf(" --header %s", shellquote.Quote(h.Name+": "+h.Value))
		}
		b.WriteString(cmd + "\n")
	}

	return b.String()
}

// BuildEvalCloneCommands returns one shell command per eval plugin URL.
// Each command clones the specific plugin subdirectory into /tmp/eval-plugin-N.
// Exported for testing.
func BuildEvalCloneCommands(urls []string) ([]string, error) {
	var cmds []string
	for i, rawURL := range urls {
		if !strings.HasPrefix(rawURL, "https://") {
			return nil, fmt.Errorf("eval_plugin url must use https:// scheme, got %q", rawURL)
		}
		repoURL, subPath, err := ParseGitHubTreeURL(rawURL)
		if err != nil {
			return nil, fmt.Errorf("parse eval plugin url %q: %w", rawURL, err)
		}
		dir := fmt.Sprintf("/tmp/eval-plugin-%d", i)
		cmd := fmt.Sprintf(
			"git clone --depth 1 --filter=blob:none --sparse %s %s && cd %s && git sparse-checkout set %s",
			shellquote.Quote(repoURL),
			shellquote.Quote(dir),
			shellquote.Quote(dir),
			shellquote.Quote(subPath),
		)
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}

// ParseGitHubTreeURL splits a GitHub tree URL into (cloneURL, subPath).
// e.g. "https://github.com/org/repo/tree/main/plugins/foo"
//   -> "https://github.com/org/repo.git", "plugins/foo"
// Exported for testing.
func ParseGitHubTreeURL(u string) (string, string, error) {
	trimmed := strings.TrimPrefix(u, "https://github.com/")
	parts := strings.SplitN(trimmed, "/tree/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected GitHub tree URL with /tree/ component, got %q", u)
	}
	repoPath := parts[0]
	branchAndSub := parts[1]
	subParts := strings.SplitN(branchAndSub, "/", 2)
	if len(subParts) < 2 {
		return "", "", fmt.Errorf("expected branch and subpath after /tree/ in %q", u)
	}
	return "https://github.com/" + repoPath + ".git", subParts[1], nil
}
```

- [ ] Run tests — confirm pass

```bash
go test ./internal/activity/... -run "TestBuildPreflight|TestBuildEval|TestParseGitHub" -v
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/activity/preflight.go internal/activity/preflight_test.go
git commit -m "feat: RunPreflight activity, pre-flight script builder, eval clone commands"
```

---

### Task 7: Wire RunPreflightActivity into StepWorkflow

**Files:**
- Modify: `internal/workflow/step.go` (StepWorkflow function)

- [ ] Read `StepWorkflow` in full (`internal/workflow/step.go` from line 75 onwards) to find where `ExecuteStep` is called

- [ ] In `StepWorkflow`, after `ProvisionSandbox` succeeds and before the `ExecuteStep` call, add:

```go
// Run pre-flight if the step has a profile or eval plugins.
var evalPluginDirs []string
if input.ResolvedOpts.EffectiveProfile != nil || len(input.ResolvedOpts.EvalPluginURLs) > 0 {
	profileBody := model.AgentProfileBody{}
	if input.ResolvedOpts.EffectiveProfile != nil {
		profileBody = *input.ResolvedOpts.EffectiveProfile
	}
	var preflightOut RunPreflightOutput
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
	}
	if err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, ao),
		RunPreflightActivity,
		RunPreflightInput{
			SandboxID:      sandboxID,
			TeamID:         input.TeamID,
			Profile:        profileBody,
			EvalPluginURLs: input.ResolvedOpts.EvalPluginURLs,
		},
	).Get(ctx, &preflightOut); err != nil {
		return nil, fmt.Errorf("pre-flight: %w", err)
	}
	evalPluginDirs = preflightOut.EvalPluginDirs
}
```

- [ ] When constructing `ExecuteStepInput`, pass `evalPluginDirs`:

```go
execInput := ExecuteStepInput{
	StepInput:     input,
	SandboxID:     sandboxID,
	Prompt:        input.ResolvedOpts.Prompt,
	EvalPluginDirs: evalPluginDirs,  // add this
}
```

- [ ] Build

```bash
go build ./...
```

- [ ] Run workflow tests

```bash
go test ./internal/workflow/... -v
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/workflow/step.go
git commit -m "feat: call RunPreflight in StepWorkflow, pass EvalPluginDirs to ExecuteStep"
```

---

### Task 8: Thread EvalPluginDirs through to claude invocation

**Files:**
- Modify: `internal/agent/runner.go`
- Modify: `internal/agent/claudecode.go`
- Modify: `internal/activity/execute.go`

- [ ] Add `EvalPluginDirs` to `RunOpts` in `internal/agent/runner.go`

```go
// In RunOpts:
EvalPluginDirs []string
```

- [ ] In `ClaudeCodeRunner.Run` (`internal/agent/claudecode.go`), append `--plugin-dir` flags

Find the line that builds `cmd` (currently: `claude -p %s --output-format ...`). Add plugin dir flags:

```go
pluginDirFlags := ""
for _, dir := range opts.EvalPluginDirs {
	pluginDirFlags += " --plugin-dir " + shellquote.Quote(dir)
}
cmd := fmt.Sprintf("%s && cd %s && claude -p %s%s --output-format stream-json --verbose --dangerously-skip-permissions --max-turns %d",
	mcpSetup,
	shellquote.Quote(opts.WorkDir),
	shellquote.Quote(opts.Prompt),
	pluginDirFlags,
	max(opts.MaxTurns, 20),
)
```

- [ ] In `ExecuteStep` (`internal/activity/execute.go`), pass `EvalPluginDirs` from `input` into `RunOpts`

Find where `runner.Run` is called and add:
```go
events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
	Prompt:        stepInput.ResolvedOpts.Prompt,
	WorkDir:       workDir,
	MaxTurns:      ...,
	EvalPluginDirs: input.EvalPluginDirs,  // add this
})
```

- [ ] Build

```bash
go build ./...
# Expected: no errors
```

- [ ] Run all tests

```bash
go test ./...
# Expected: all PASS
```

- [ ] Commit

```bash
git add internal/agent/runner.go internal/agent/claudecode.go internal/activity/execute.go
git commit -m "feat: thread EvalPluginDirs to claude --plugin-dir flags"
```

---

### Task 9: Register activities in worker

**Files:**
- Modify: `cmd/worker/main.go`

- [ ] Read `cmd/worker/main.go` to find where `Activities` is instantiated and activities are registered

- [ ] Wire `DBProfileStore` into `Activities`:

```go
acts := &activity.Activities{
	Sandbox:      sbClient,
	DB:           db,
	CredStore:    credStore,
	AgentRunners: runners,
	ProfileStore: &activity.DBProfileStore{DB: db},  // add this
}
```

- [ ] Register the two new activities:

```go
w.RegisterActivity(acts.ResolveAgentProfile)
w.RegisterActivity(acts.RunPreflight)
```

- [ ] Build worker

```bash
go build ./cmd/worker/...
# Expected: no errors
```

- [ ] Build all

```bash
go build ./...
make lint
# Expected: no errors
```

- [ ] Commit

```bash
git add cmd/worker/main.go
git commit -m "feat: register ResolveAgentProfile and RunPreflight activities in worker"
```

---

## Chunk 4: CRUD API

### Task 10: Agent profile + marketplace handlers

**Files:**
- Create: `internal/server/handlers/profiles.go`
- Create: `internal/server/handlers/profiles_test.go`
- Modify: `internal/server/router.go`

- [ ] Read one existing handler (e.g. `internal/server/handlers/credentials.go`) to understand the handler pattern, how team ID is extracted from context, and how DB errors are mapped to status codes

- [ ] Write failing tests

```go
// internal/server/handlers/profiles_test.go
package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
)

// Tests follow the same pattern as existing handler tests in this package.
// Use the existing test helpers (withTeamID, etc.) already present in the test package.

func TestListAgentProfiles_ReturnsOK(t *testing.T) {
	h := handlers.NewProfilesHandler(newStubProfileDB())
	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles", nil)
	req = withTeamID(req, "team-1")
	rr := httptest.NewRecorder()
	h.ListProfiles(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateAgentProfile_ValidBody(t *testing.T) {
	h := handlers.NewProfilesHandler(newStubProfileDB())
	body := map[string]any{
		"name": "my-profile",
		"body": model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/foo"}},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent-profiles", bytes.NewReader(b))
	req = withTeamID(req, "team-1")
	rr := httptest.NewRecorder()
	h.CreateProfile(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateAgentProfile_InvalidPluginSource_Returns400(t *testing.T) {
	h := handlers.NewProfilesHandler(newStubProfileDB())
	body := map[string]any{
		"name": "bad",
		"body": model.AgentProfileBody{
			Plugins: []model.PluginSource{
				{Plugin: "plugins/foo", GitHubURL: "https://github.com/org/repo"},
			},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent-profiles", bytes.NewReader(b))
	req = withTeamID(req, "team-1")
	rr := httptest.NewRecorder()
	h.CreateProfile(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteAgentProfile_Returns204(t *testing.T) {
	h := handlers.NewProfilesHandler(newStubProfileDB())
	req := httptest.NewRequest(http.MethodDelete, "/api/agent-profiles/profile-1", nil)
	req = withTeamID(req, "team-1")
	rr := httptest.NewRecorder()
	h.DeleteProfile(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
}
```

- [ ] Run tests — confirm fail

```bash
go test ./internal/server/handlers/... -run "TestAgentProfile\|TestMarketplace" -v
# Expected: FAIL
```

- [ ] Implement `internal/server/handlers/profiles.go`

The handler exposes:
- `GET    /api/agent-profiles`     → list for team
- `POST   /api/agent-profiles`     → create (validate all PluginSource/SkillSource)
- `GET    /api/agent-profiles/{id}` → get one
- `PUT    /api/agent-profiles/{id}` → update (re-validate body)
- `DELETE /api/agent-profiles/{id}` → 204
- `GET    /api/marketplaces`       → list
- `POST   /api/marketplaces`       → create
- `DELETE /api/marketplaces/{id}`  → 204

Validation on create/update: call `p.Validate()` for each `PluginSource` and `s.Validate()` for each `SkillSource`. Return 400 with error message on failure.

- [ ] Run handler tests — confirm pass

```bash
go test ./internal/server/handlers/... -run "TestAgentProfile\|TestMarketplace" -v
# Expected: all PASS
```

- [ ] Register routes in `internal/server/router.go` inside the authenticated `r.Group` block

```go
// Agent profiles
r.Get("/api/agent-profiles", deps.Profiles.ListProfiles)
r.Post("/api/agent-profiles", deps.Profiles.CreateProfile)
r.Get("/api/agent-profiles/{id}", deps.Profiles.GetProfile)
r.Put("/api/agent-profiles/{id}", deps.Profiles.UpdateProfile)
r.Delete("/api/agent-profiles/{id}", deps.Profiles.DeleteProfile)

// Marketplaces
r.Get("/api/marketplaces", deps.Profiles.ListMarketplaces)
r.Post("/api/marketplaces", deps.Profiles.CreateMarketplace)
r.Delete("/api/marketplaces/{id}", deps.Profiles.DeleteMarketplace)
```

- [ ] Add `Profiles` to the `Deps` struct in `router.go`

- [ ] Build

```bash
go build ./...
```

- [ ] Full test suite + lint

```bash
go test ./...
make lint
# Expected: all PASS, no errors
```

- [ ] Commit

```bash
git add internal/server/handlers/profiles.go internal/server/handlers/profiles_test.go \
        internal/server/router.go
git commit -m "feat: CRUD API for agent profiles and marketplaces"
```

---

## Final Verification

- [ ] Full test suite

```bash
go test ./...
# Expected: all PASS
```

- [ ] Lint

```bash
make lint
# Expected: no errors
```

- [ ] Build all binaries

```bash
go build ./...
# Expected: clean
```

- [ ] Integration smoke test — start the stack, dispatch a workflow with `agent_profile` declared, confirm pre-flight log lines appear

```bash
scripts/integration/start.sh --build
scripts/integration/logs.sh
# Expected: pre-flight script output visible in step logs
```
