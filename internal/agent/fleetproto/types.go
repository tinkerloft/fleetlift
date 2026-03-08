// Package fleetproto defines fleetlift-specific protocol types for the agent sidecar.
package fleetproto

import (
	"path/filepath"
	"time"
)

// --- Phase ---

// Phase represents the lifecycle phase of the agent.
type Phase string

const (
	PhaseInitializing  Phase = "initializing"
	PhaseExecuting     Phase = "executing"
	PhaseVerifying     Phase = "verifying"
	PhaseAwaitingInput Phase = "awaiting_input"
	PhaseComplete      Phase = "complete"
	PhaseFailed        Phase = "failed"
	PhaseCancelled     Phase = "cancelled"
)

// PhaseCreatingPRs is a fleetlift-specific agent lifecycle phase.
const PhaseCreatingPRs Phase = "creating_prs"

// --- SteeringAction ---

// SteeringAction represents the type of steering instruction.
type SteeringAction string

const (
	SteeringActionApprove SteeringAction = "approve"
	SteeringActionReject  SteeringAction = "reject"
	SteeringActionCancel  SteeringAction = "cancel"
	SteeringActionSteer   SteeringAction = "steer"
)

// --- AgentStatus ---

// AgentStatus is the current status written by the agent to the status file.
type AgentStatus struct {
	Phase     Phase             `json:"phase"`
	Step      string            `json:"step,omitempty"`
	Message   string            `json:"message,omitempty"`
	Progress  *StatusProgress   `json:"progress,omitempty"`
	Iteration int               `json:"iteration"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// StatusProgress represents progress within a phase.
type StatusProgress struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

// --- Path functions ---

// ManifestPath returns the path to the manifest file under base.
func ManifestPath(base string) string { return filepath.Join(base, "manifest.json") }

// StatusPath returns the path to the status file under base.
func StatusPath(base string) string { return filepath.Join(base, "status.json") }

// ResultPath returns the path to the result file under base.
func ResultPath(base string) string { return filepath.Join(base, "result.json") }

// SteeringPath returns the path to the steering file under base.
func SteeringPath(base string) string { return filepath.Join(base, "steering.json") }

// DefaultBasePath is the base directory for fleetlift agent protocol files inside the sandbox.
const DefaultBasePath = "/workspace/.fleetlift"

// --- SteeringInstruction ---

// SteeringInstruction extends the base steering instruction with a Timestamp field.
// The worker sets Timestamp when writing the instruction for audit/tracing.
type SteeringInstruction struct {
	Action    SteeringAction `json:"action"`
	Prompt    string         `json:"prompt,omitempty"`
	Iteration int            `json:"iteration"`
	Timestamp time.Time      `json:"timestamp"`
}

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

// EffectiveRepos returns the repos to operate on.
func (m *TaskManifest) EffectiveRepos() []ManifestRepo {
	if m.Transformation != nil && len(m.Targets) > 0 {
		return m.Targets
	}
	return m.Repositories
}

// RepoBasePath returns the base path where repos are cloned.
func (m *TaskManifest) RepoBasePath() string {
	if m.Transformation != nil {
		return filepath.Join(WorkspacePath, "targets")
	}
	return WorkspacePath
}

// RepoPath returns the full path for a named repository.
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

// ForEachTarget is a target for iteration within a repository.
type ForEachTarget struct {
	Name    string `json:"name"`
	Context string `json:"context"`
}

// ManifestExecution specifies what to run.
type ManifestExecution struct {
	Type    string            `json:"type"` // "agentic" or "deterministic"
	Prompt  string            `json:"prompt,omitempty"`
	Image   string            `json:"image,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Command []string          `json:"command,omitempty"`
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

// --- Result (Agent → Worker) ---

// AgentResult is the full structured result written by the agent.
type AgentResult struct {
	Status          Phase            `json:"status"`
	Repositories    []RepoResult     `json:"repositories"`
	AgentOutput     string           `json:"agent_output,omitempty"`
	SteeringHistory []SteeringRecord `json:"steering_history,omitempty"`
	Error           *string          `json:"error,omitempty"`
	StartedAt       time.Time        `json:"started_at"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
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

// MaxDiffLinesPerFile is the default truncation limit for per-file diffs.
const MaxDiffLinesPerFile = 1000

// Well-known workspace paths used by fleetlift agents.
const WorkspacePath = "/workspace"
