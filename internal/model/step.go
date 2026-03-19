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
	ID                   string     `db:"id" json:"id"`
	RunID                string     `db:"run_id" json:"run_id"`
	StepID               string     `db:"step_id" json:"step_id"`
	StepTitle            *string    `db:"step_title" json:"step_title,omitempty"`
	Status               StepStatus `db:"status" json:"status"`
	SandboxID            *string    `db:"sandbox_id" json:"sandbox_id,omitempty"`
	SandboxGroup         *string    `db:"sandbox_group" json:"sandbox_group,omitempty"`
	Output               JSONMap    `db:"output" json:"output,omitempty"`
	Diff                 *string    `db:"diff" json:"diff,omitempty"`
	PRUrl                *string    `db:"pr_url" json:"pr_url,omitempty"`
	BranchName           *string    `db:"branch_name" json:"branch_name,omitempty"`
	ErrorMessage         *string    `db:"error_message" json:"error_message,omitempty"`
	TemporalWorkflowID   *string    `db:"temporal_workflow_id" json:"temporal_workflow_id,omitempty"`
	ParentStepRunID      *string    `db:"parent_step_run_id" json:"parent_step_run_id,omitempty"`
	CheckpointBranch     *string    `db:"checkpoint_branch" json:"checkpoint_branch,omitempty"`
	CheckpointArtifactID *string    `db:"checkpoint_artifact_id" json:"checkpoint_artifact_id,omitempty"`
	StartedAt            *time.Time `db:"started_at" json:"started_at,omitempty"`
	CompletedAt          *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	CostUSD              *float64   `db:"cost_usd" json:"cost_usd,omitempty"`
	Input                JSONMap    `db:"input" json:"input,omitempty"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
}

type StepRunLog struct {
	ID        int64     `db:"id" json:"id"`
	StepRunID string    `db:"step_run_id" json:"step_run_id"`
	Seq       int64     `db:"seq" json:"seq"`
	Stream    string    `db:"stream" json:"stream"` // stdout | stderr | system
	Content   string    `db:"content" json:"content"`
	Ts        time.Time `db:"ts" json:"ts"`
}

// StepOutput is the in-memory result passed between DAG steps via template resolution.
type StepOutput struct {
	StepID     string         `json:"step_id"`
	Status     StepStatus     `json:"status"`
	Output     map[string]any `json:"output,omitempty"`
	Diff       string         `json:"diff,omitempty"`
	PRUrl      string         `json:"pr_url,omitempty"`
	BranchName string         `json:"branch_name,omitempty"`
	Outputs    []StepOutput   `json:"outputs,omitempty"` // fan-out: per-repo results
	Error      string         `json:"error,omitempty"`
	CostUSD    float64        `json:"cost_usd,omitempty"`
	// Fields used when Status == "awaiting_input"
	InboxItemID      string `json:"inbox_item_id,omitempty"`
	Question         string `json:"question,omitempty"`
	CheckpointBranch string `json:"checkpoint_branch,omitempty"`
	StateArtifactID  string `json:"state_artifact_id,omitempty"`
}
