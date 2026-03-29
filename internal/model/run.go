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
	ID            string     `db:"id" json:"id"`
	TeamID        string     `db:"team_id" json:"team_id"`
	WorkflowID    string     `db:"workflow_id" json:"workflow_id"`
	WorkflowTitle string     `db:"workflow_title" json:"workflow_title"`
	Parameters    JSONMap    `db:"parameters" json:"parameters"`
	Status        RunStatus  `db:"status" json:"status"`
	Model         *string    `db:"model" json:"model,omitempty"`
	TemporalID    string     `db:"temporal_id" json:"temporal_id,omitempty"`
	TriggeredBy   string     `db:"triggered_by" json:"triggered_by,omitempty"`
	StartedAt     *time.Time `db:"started_at" json:"started_at,omitempty"`
	CompletedAt   *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	ErrorMessage  *string    `db:"error_message" json:"error_message,omitempty"`
	TotalCostUSD  *float64   `db:"total_cost_usd" json:"total_cost_usd,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
}
