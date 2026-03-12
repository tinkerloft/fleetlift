package activity

import (
	"context"
	"fmt"
	"time"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// UpdateStepStatus updates the status of a step run in the database.
func (a *Activities) UpdateStepStatus(ctx context.Context, stepRunID string, status string) error {
	now := time.Now()
	var query string
	var args []any

	switch {
	case isTerminal(model.StepStatus(status)):
		query = `UPDATE step_runs SET status = $1, started_at = COALESCE(started_at, $2), completed_at = $3 WHERE id = $4`
		args = []any{status, now, now, stepRunID}
	case status != string(model.StepStatusPending):
		query = `UPDATE step_runs SET status = $1, started_at = COALESCE(started_at, $2) WHERE id = $3`
		args = []any{status, now, stepRunID}
	default:
		query = `UPDATE step_runs SET status = $1 WHERE id = $2`
		args = []any{status, stepRunID}
	}

	if _, err := a.DB.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update step status: %w", err)
	}
	return nil
}

// UpdateRunStatus updates the status of a run in the database.
func (a *Activities) UpdateRunStatus(ctx context.Context, runID string, status string) error {
	now := time.Now()
	var query string
	var args []any

	switch {
	case isRunTerminal(model.RunStatus(status)):
		query = `UPDATE runs SET status = $1, completed_at = $2 WHERE id = $3`
		args = []any{status, now, runID}
	case status == string(model.RunStatusRunning):
		query = `UPDATE runs SET status = $1, started_at = COALESCE(started_at, $2) WHERE id = $3`
		args = []any{status, now, runID}
	default:
		query = `UPDATE runs SET status = $1 WHERE id = $2`
		args = []any{status, runID}
	}

	if _, err := a.DB.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	return nil
}

// CreateInboxItem creates an inbox notification for awaiting_input or output_ready events.
func (a *Activities) CreateInboxItem(ctx context.Context, teamID, runID, stepRunID, kind, title, summary string) error {
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO inbox_items (team_id, run_id, step_run_id, kind, title, summary)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		teamID, runID, stepRunID, kind, title, summary)
	if err != nil {
		return fmt.Errorf("create inbox item: %w", err)
	}
	return nil
}

// CleanupSandbox kills a sandbox.
func (a *Activities) CleanupSandbox(ctx context.Context, sandboxID string) error {
	return a.Sandbox.Kill(ctx, sandboxID)
}

// updateStepStatus is a helper used within activities (not registered as a separate activity).
func (a *Activities) updateStepStatus(ctx context.Context, stepRunID string, status model.StepStatus) {
	if a.DB == nil {
		return
	}
	_, _ = a.DB.ExecContext(ctx,
		`UPDATE step_runs SET status = $1 WHERE id = $2`,
		string(status), stepRunID)
}

// writeLogLine appends a log line to step_run_logs.
func (a *Activities) writeLogLine(ctx context.Context, stepRunID string, seq int64, stream, content string) {
	if a.DB == nil || content == "" {
		return
	}
	_, _ = a.DB.ExecContext(ctx,
		`INSERT INTO step_run_logs (step_run_id, seq, stream, content) VALUES ($1, $2, $3, $4)`,
		stepRunID, seq, stream, content)
}

func isTerminal(s model.StepStatus) bool {
	switch s {
	case model.StepStatusComplete, model.StepStatusFailed, model.StepStatusSkipped:
		return true
	}
	return false
}

func isRunTerminal(s model.RunStatus) bool {
	switch s {
	case model.RunStatusComplete, model.RunStatusFailed, model.RunStatusCancelled:
		return true
	}
	return false
}
