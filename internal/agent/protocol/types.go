// Package protocol defines the file-based communication protocol between
// the fleetlift worker and the sandbox sidecar agent.
//
// All communication is via JSON files in /workspace/.fleetlift/:
//
//	manifest.json   - Worker → Agent: task definition (written once)
//	status.json     - Agent → Worker: lightweight phase indicator (polled frequently)
//	result.json     - Agent → Worker: full structured results
//	steering.json   - Worker → Agent: HITL instruction (deleted after processing)
package protocol

import (
	"path/filepath"
	"time"
)

// Well-known paths inside the sandbox.
const (
	BasePath      = "/workspace/.fleetlift"
	ManifestPath  = BasePath + "/manifest.json"
	StatusPath    = BasePath + "/status.json"
	ResultPath    = BasePath + "/result.json"
	SteeringPath  = BasePath + "/steering.json"
	WorkspacePath = "/workspace"
)

// --- Manifest (Worker → Agent) ---

// TaskManifest is the full task definition written by the worker for the agent to execute.
type TaskManifest struct {
	TaskID                string             `json:"task_id"`
	Mode                  string             `json:"mode"` // "transform" or "report"
	Title                 string             `json:"title"`
	Repositories          []ManifestRepo     `json:"repositories"`
	Transformation        *ManifestRepo      `json:"transformation,omitempty"`
	Targets               []ManifestRepo     `json:"targets,omitempty"`
	ForEach               []ForEachTarget    `json:"for_each,omitempty"`
	Execution             ManifestExecution  `json:"execution"`
	Verifiers             []ManifestVerifier `json:"verifiers,omitempty"`
	TimeoutSeconds        int                `json:"timeout_seconds"`
	RequireApproval       bool               `json:"require_approval"`
	MaxSteeringIterations int                `json:"max_steering_iterations"`
	PullRequest           *ManifestPRConfig  `json:"pull_request,omitempty"`
	GitConfig             ManifestGitConfig  `json:"git_config"`
}

// EffectiveRepos returns the repos to operate on (H6 fix — single source of truth).
func (m *TaskManifest) EffectiveRepos() []ManifestRepo {
	if m.Transformation != nil && len(m.Targets) > 0 {
		return m.Targets
	}
	return m.Repositories
}

// RepoBasePath returns the base path where repos are cloned (H6 fix).
func (m *TaskManifest) RepoBasePath() string {
	if m.Transformation != nil {
		return filepath.Join(WorkspacePath, "targets")
	}
	return WorkspacePath
}

// RepoPath returns the full path for a named repository (H6 fix).
func (m *TaskManifest) RepoPath(repoName string) string {
	return filepath.Join(m.RepoBasePath(), repoName)
}

// ManifestRepo defines a repository to clone.
type ManifestRepo struct {
	URL    string   `json:"url"`
	Branch string   `json:"branch,omitempty"`
	Name   string   `json:"name,omitempty"`
	Setup  []string `json:"setup,omitempty"`
}

// ForEachTarget is a target for iteration within a repository (report mode).
type ForEachTarget struct {
	Name    string `json:"name"`
	Context string `json:"context"`
}

// ManifestExecution specifies what to run.
type ManifestExecution struct {
	Type    string            `json:"type"` // "agentic" or "deterministic"
	Prompt  string            `json:"prompt,omitempty"`
	Image   string            `json:"image,omitempty"` // Deterministic: base image for sandbox (used by worker at provision, not by agent)
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Command []string          `json:"command,omitempty"` // Deterministic: command to run directly in sandbox
}

// ManifestVerifier defines a verification command.
type ManifestVerifier struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
}

// ManifestPRConfig contains PR creation settings.
type ManifestPRConfig struct {
	BranchPrefix string   `json:"branch_prefix,omitempty"`
	Title        string   `json:"title,omitempty"`
	Body         string   `json:"body,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Reviewers    []string `json:"reviewers,omitempty"`
}

// ManifestGitConfig contains git identity settings.
type ManifestGitConfig struct {
	UserEmail  string `json:"user_email"`
	UserName   string `json:"user_name"`
	CloneDepth int    `json:"clone_depth,omitempty"`
}

// --- Status (Agent → Worker) ---

// Phase is a typed string for agent status phases (M8 fix).
type Phase string

// Phase constants for agent status.
const (
	PhaseInitializing  Phase = "initializing"
	PhaseExecuting     Phase = "executing"
	PhaseVerifying     Phase = "verifying"
	PhaseAwaitingInput Phase = "awaiting_input"
	PhaseCreatingPRs   Phase = "creating_prs"
	PhaseComplete      Phase = "complete"
	PhaseFailed        Phase = "failed"
	PhaseCancelled     Phase = "cancelled"
)

// AgentStatus is a lightweight status indicator written by the agent.
// The worker polls this frequently to track progress.
type AgentStatus struct {
	Phase     Phase           `json:"phase"`
	Step      string          `json:"step,omitempty"`
	Message   string          `json:"message,omitempty"`
	Progress  *StatusProgress `json:"progress,omitempty"`
	Iteration int             `json:"iteration"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// StatusProgress tracks completion within a phase.
type StatusProgress struct {
	CompletedRepos int `json:"completed_repos"`
	TotalRepos     int `json:"total_repos"`
}

// --- Result (Agent → Worker) ---

// AgentResult is the full structured result written by the agent.
type AgentResult struct {
	Status          Phase              `json:"status"` // mirrors phase: "awaiting_input", "complete", "failed", "cancelled"
	Repositories    []RepoResult       `json:"repositories"`
	AgentOutput     string             `json:"agent_output,omitempty"`
	SteeringHistory []SteeringRecord   `json:"steering_history,omitempty"`
	Error           *string            `json:"error,omitempty"`
	StartedAt       time.Time          `json:"started_at"`
	CompletedAt     *time.Time         `json:"completed_at,omitempty"`
}

// RepoResult contains the result for a single repository.
type RepoResult struct {
	Name            string           `json:"name"`
	Status          string           `json:"status"` // "success", "failed", "skipped"
	FilesModified   []string         `json:"files_modified,omitempty"`
	Diffs           []DiffEntry      `json:"diffs,omitempty"`
	VerifierResults []VerifierResult `json:"verifier_results,omitempty"`
	Report          *ReportResult    `json:"report,omitempty"`
	ForEachResults  []ForEachResult  `json:"for_each_results,omitempty"`
	PullRequest     *PRInfo          `json:"pull_request,omitempty"`
	Error           *string          `json:"error,omitempty"`
}

// DiffEntry represents a single file's diff.
type DiffEntry struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // "modified", "added", "deleted"
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"`
}

// VerifierResult is the result of running a single verifier.
type VerifierResult struct {
	Name     string `json:"name"`
	Success  bool   `json:"success"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
}

// ReportResult contains report-mode output for a repo.
type ReportResult struct {
	Frontmatter      map[string]any `json:"frontmatter,omitempty"`
	Body             string         `json:"body,omitempty"`
	Raw              string         `json:"raw"`
	ValidationErrors []string       `json:"validation_errors,omitempty"`
}

// ForEachResult contains the result for a single forEach target.
type ForEachResult struct {
	Target ForEachTarget `json:"target"`
	Report *ReportResult `json:"report,omitempty"`
	Error  *string       `json:"error,omitempty"`
}

// PRInfo contains information about a created pull request.
type PRInfo struct {
	URL        string `json:"url"`
	Number     int    `json:"number"`
	BranchName string `json:"branch_name"`
	Title      string `json:"title"`
}

// SteeringRecord records a single steering interaction.
type SteeringRecord struct {
	Iteration int       `json:"iteration"`
	Prompt    string    `json:"prompt"`
	Timestamp time.Time `json:"timestamp"`
}

// --- Steering (Worker → Agent) ---

// SteeringAction is a typed string for steering actions (M8 fix).
type SteeringAction string

// SteeringAction constants.
const (
	SteeringActionSteer   SteeringAction = "steer"
	SteeringActionApprove SteeringAction = "approve"
	SteeringActionReject  SteeringAction = "reject"
	SteeringActionCancel  SteeringAction = "cancel"
)

// SteeringInstruction is written by the worker to direct the agent.
// The agent polls for this file and deletes it after processing.
type SteeringInstruction struct {
	Action    SteeringAction `json:"action"` // "steer", "approve", "reject", "cancel"
	Prompt    string         `json:"prompt,omitempty"`
	Iteration int            `json:"iteration"`
	Timestamp time.Time      `json:"timestamp"`
}

// MaxDiffLinesPerFile is the default truncation limit for per-file diffs.
const MaxDiffLinesPerFile = 1000

