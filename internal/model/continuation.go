package model

// ContinuationContext is passed to a continuation ExecuteStep call.
type ContinuationContext struct {
	InboxItemID      string
	Question         string
	HumanAnswer      string
	CheckpointBranch string // empty if not set
	StateArtifactID  string // empty if no state_summary
}

// InboxAnswer is delivered via the Temporal "respond" signal.
type InboxAnswer struct {
	Answer    string
	Responder string
}

// CleanupCheckpointInput is the input for CleanupCheckpointBranch activity.
type CleanupCheckpointInput struct {
	RepoURL        string
	Branch         string
	CredentialName string // GitHub token credential name
	TeamID         string
}

// CreateContinuationStepRunInput is the input for CreateContinuationStepRun activity.
type CreateContinuationStepRunInput struct {
	RunID                string
	StepID               string // e.g. "fix-resume-1"
	StepTitle            string // e.g. "Fix (resumed)"
	TemporalWorkflowID   string
	ParentStepRunID      string
	CheckpointBranch     string
	CheckpointArtifactID string
}
