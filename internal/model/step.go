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
	Output       JSONMap        `db:"output" json:"output,omitempty"`
	Diff         string         `db:"diff" json:"diff,omitempty"`
	PRUrl        string         `db:"pr_url" json:"pr_url,omitempty"`
	BranchName   string         `db:"branch_name" json:"branch_name,omitempty"`
	ErrorMessage string         `db:"error_message" json:"error_message,omitempty"`
	StartedAt    *time.Time     `db:"started_at" json:"started_at,omitempty"`
	CompletedAt  *time.Time     `db:"completed_at" json:"completed_at,omitempty"`
	CreatedAt    time.Time      `db:"created_at" json:"created_at"`
}

type StepRunLog struct {
	ID        int64     `db:"id"`
	StepRunID string    `db:"step_run_id"`
	Seq       int64     `db:"seq"`
	Stream    string    `db:"stream"` // stdout | stderr | system
	Content   string    `db:"content"`
	Ts        time.Time `db:"ts"`
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
}
