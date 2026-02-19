package server

import (
	"context"

	flclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// TemporalClient is the interface the server uses to interact with Temporal.
// *client.Client satisfies this interface.
type TemporalClient interface {
	ListWorkflows(ctx context.Context, statusFilter string, limit int) ([]flclient.WorkflowInfo, error)
	GetWorkflowStatus(ctx context.Context, workflowID string) (model.TaskStatus, error)
	GetWorkflowDiff(ctx context.Context, workflowID string) ([]model.DiffOutput, error)
	GetWorkflowVerifierLogs(ctx context.Context, workflowID string) ([]model.VerifierOutput, error)
	GetSteeringState(ctx context.Context, workflowID string) (*model.SteeringState, error)
	GetExecutionProgress(ctx context.Context, workflowID string) (*model.ExecutionProgress, error)
	ApproveWorkflow(ctx context.Context, workflowID string) error
	RejectWorkflow(ctx context.Context, workflowID string) error
	CancelWorkflow(ctx context.Context, workflowID string) error
	SteerWorkflow(ctx context.Context, workflowID, prompt string) error
	ContinueWorkflow(ctx context.Context, workflowID string, skipRemaining bool) error
	Close()
}
