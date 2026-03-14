package model

import (
	"fmt"
	"time"

	"github.com/lib/pq"
	"gopkg.in/yaml.v3"
)

// WorkflowTemplate is a reusable DAG definition stored in the DB or embedded as builtin.
type WorkflowTemplate struct {
	ID          string    `db:"id" json:"id"`
	TeamID      string    `db:"team_id" json:"team_id"`
	Slug        string    `db:"slug" json:"slug"`
	Title       string    `db:"title" json:"title"`
	Description string    `db:"description" json:"description"`
	Tags        pq.StringArray `db:"tags" json:"tags"`
	YAMLBody    string    `db:"yaml_body" json:"yaml_body"`
	Builtin     bool      `db:"-" json:"builtin"`
	CreatedBy   string    `db:"created_by" json:"created_by,omitempty"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// WorkflowDef is the parsed form of a WorkflowTemplate's YAML.
type WorkflowDef struct {
	Version     int            `yaml:"version"`
	ID          string         `yaml:"id"`
	Title       string         `yaml:"title"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Parameters  []ParameterDef `yaml:"parameters"`
	Steps       []StepDef      `yaml:"steps"`
}

type ParameterDef struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`                  // string | int | bool | json
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type StepDef struct {
	ID                string          `yaml:"id"`
	Title             string          `yaml:"title,omitempty"`
	DependsOn         []string        `yaml:"depends_on,omitempty"`
	SandboxGroup      string          `yaml:"sandbox_group,omitempty"`
	Mode              string          `yaml:"mode,omitempty"` // report | transform
	Repositories      any             `yaml:"repositories,omitempty"`
	MaxParallel       int             `yaml:"max_parallel,omitempty"`
	FailureThreshold  int             `yaml:"failure_threshold,omitempty"`
	Execution         *ExecutionDef   `yaml:"execution,omitempty"`
	ApprovalPolicy    string          `yaml:"approval_policy,omitempty"` // always|never|agent|on_changes
	AllowMidExecPause bool            `yaml:"allow_mid_execution_pause,omitempty"`
	PullRequest       *PRDef          `yaml:"pull_request,omitempty"`
	Condition         string          `yaml:"condition,omitempty"`
	Optional          bool            `yaml:"optional,omitempty"`
	Outputs           *StepOutputsDef `yaml:"outputs,omitempty"`
	Inputs            *StepInputsDef  `yaml:"inputs,omitempty"`
	Action            *ActionDef      `yaml:"action,omitempty"`
	Sandbox           *SandboxSpec    `yaml:"sandbox,omitempty"`
	Knowledge         *KnowledgeDef   `yaml:"knowledge,omitempty"`
	Timeout           string          `yaml:"timeout,omitempty"`
}

// SandboxSpec declares the infrastructure requirements for a step's sandbox.
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
	Agent       string           `yaml:"agent"` // "claude-code" | "codex" | "shell"
	Prompt      string           `yaml:"prompt"`
	Verifiers   any              `yaml:"verifiers,omitempty"`
	Credentials []string         `yaml:"credentials,omitempty"`
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

func ParseWorkflowYAML(data []byte, def *WorkflowDef) error {
	if err := yaml.Unmarshal(data, def); err != nil {
		return fmt.Errorf("parse workflow YAML: %w", err)
	}
	return nil
}
