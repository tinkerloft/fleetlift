package activity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// CreateStepRun inserts a new step_run record for the given run/step and returns its UUID.
// temporalWorkflowID is the Temporal child workflow ID used to route HITL signals.
func (a *Activities) CreateStepRun(ctx context.Context, runID, stepID, stepTitle, temporalWorkflowID string) (string, error) {
	id := uuid.New().String()
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO step_runs (id, run_id, step_id, step_title, status, temporal_workflow_id)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, runID, stepID, nullStr(stepTitle), string(model.StepStatusPending), nullStr(temporalWorkflowID))
	if err != nil {
		return "", fmt.Errorf("create step run: %w", err)
	}
	return id, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

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
func (a *Activities) UpdateRunStatus(ctx context.Context, runID string, status string, errorMsg string) error {
	now := time.Now()
	var query string
	var args []any

	switch {
	case isRunTerminal(model.RunStatus(status)):
		query = `UPDATE runs
             SET status = $1,
                 completed_at = $2,
                 error_message = NULLIF($3, ''),
                 total_cost_usd = (
                     SELECT SUM(cost_usd)
                     FROM step_runs
                     WHERE run_id = $4
                 )
             WHERE id = $5`
		args = []any{status, now, errorMsg, runID, runID}
	case status == string(model.RunStatusRunning):
		query = `UPDATE runs SET status = $1, started_at = COALESCE(started_at, $2), error_message = NULL WHERE id = $3`
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
		 VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6)`,
		teamID, runID, stepRunID, kind, title, summary)
	if err != nil {
		return fmt.Errorf("create inbox item: %w", err)
	}
	return nil
}

// CompleteStepRun sets the final status, output, diff, error, and cost for a step run.
func (a *Activities) CompleteStepRun(ctx context.Context, stepRunID string, status string, output map[string]any, diff string, errorMsg string, costUSD float64) error {
	now := time.Now()
	_, err := a.DB.ExecContext(ctx,
		`UPDATE step_runs
		 SET status = $1,
		     output = $2,
		     diff = NULLIF($3, ''),
		     error_message = NULLIF($4, ''),
		     started_at = COALESCE(started_at, $5),
		     completed_at = $6,
		     cost_usd = NULLIF($8::numeric, 0)
		 WHERE id = $7`,
		status, model.JSONMap(output), diff, errorMsg, now, now, stepRunID, costUSD)
	if err != nil {
		return fmt.Errorf("complete step run: %w", err)
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
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE step_runs SET status = $1 WHERE id = $2`,
		string(status), stepRunID); err != nil {
		activity.RecordHeartbeat(ctx, "status update failed: "+err.Error())
	}
}

// logLine holds a single buffered log entry.
type logLine struct {
	Seq     int64
	Stream  string
	Content string
}

// logBuffer is a simple buffer of log lines with a configurable flush threshold.
// It also flushes on a time interval to support real-time log streaming.
type logBuffer struct {
	stepRunID string
	stream    string
	lines     []logLine
	threshold int
	acts      *Activities
	lastFlush time.Time
}

// newLogBuffer creates a logBuffer that flushes every threshold lines or every second.
func newLogBuffer(acts *Activities, stepRunID, stream string, threshold int) *logBuffer {
	return &logBuffer{
		stepRunID: stepRunID,
		stream:    stream,
		lines:     make([]logLine, 0, threshold),
		threshold: threshold,
		acts:      acts,
		lastFlush: time.Now(),
	}
}

// add appends a log line and flushes if the threshold is reached or 1 second has passed.
func (b *logBuffer) add(ctx context.Context, seq int64, content string) {
	if content == "" {
		return
	}
	b.lines = append(b.lines, logLine{Seq: seq, Stream: b.stream, Content: content})
	if len(b.lines) >= b.threshold || time.Since(b.lastFlush) >= time.Second {
		b.flush(ctx)
	}
}

// flush writes all buffered lines to the DB in a single multi-row INSERT.
func (b *logBuffer) flush(ctx context.Context) {
	if b.acts.DB == nil || len(b.lines) == 0 {
		b.lines = b.lines[:0]
		return
	}
	_ = batchInsertLogs(ctx, b.acts, b.stepRunID, b.lines)
	b.lines = b.lines[:0]
	b.lastFlush = time.Now()
}

// buildInsertLogsQuery returns the INSERT SQL for a batch of log lines.
// Uses ON CONFLICT DO NOTHING so Temporal activity retries are idempotent.
func buildInsertLogsQuery(lines []logLine) string {
	placeholders := make([]string, 0, len(lines))
	for i := range lines {
		base := i * 4
		placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4))
	}
	return "INSERT INTO step_run_logs (step_run_id, seq, stream, content) VALUES " +
		strings.Join(placeholders, ", ") +
		" ON CONFLICT (step_run_id, seq) DO NOTHING"
}

// batchInsertLogs writes a slice of log lines to step_run_logs in one INSERT.
func batchInsertLogs(ctx context.Context, a *Activities, stepRunID string, lines []logLine) error {
	if len(lines) == 0 {
		return nil
	}
	args := make([]any, 0, len(lines)*4)
	for _, ln := range lines {
		args = append(args, stepRunID, ln.Seq, ln.Stream, ln.Content)
	}
	_, err := a.DB.ExecContext(ctx, buildInsertLogsQuery(lines), args...)
	return err
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
