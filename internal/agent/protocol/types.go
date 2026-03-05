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
	agentboxproto "github.com/tinkerloft/agentbox/protocol"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

// DefaultBasePath is the default directory for protocol files inside the sandbox.
const DefaultBasePath = "/workspace/.fleetlift"

// BasePath is an alias for DefaultBasePath for backward compatibility.
const BasePath = DefaultBasePath

// WorkspacePath is the sandbox workspace root.
const WorkspacePath = fleetproto.WorkspacePath

// Path helpers return the path to each protocol file under the given base directory.
// If no base is provided, DefaultBasePath is used.

// ManifestPath returns the path to manifest.json under base.
func ManifestPath(base ...string) string {
	return agentboxproto.ManifestPath(resolveBase(base))
}

// StatusPath returns the path to status.json under base.
func StatusPath(base ...string) string {
	return agentboxproto.StatusPath(resolveBase(base))
}

// ResultPath returns the path to result.json under base.
func ResultPath(base ...string) string {
	return agentboxproto.ResultPath(resolveBase(base))
}

// SteeringPath returns the path to steering.json under base.
func SteeringPath(base ...string) string {
	return agentboxproto.SteeringPath(resolveBase(base))
}

func resolveBase(base []string) string {
	if len(base) > 0 {
		return base[0]
	}
	return DefaultBasePath
}

// --- Phase ---

// Phase is a typed string for agent lifecycle phases (alias of agentbox type).
type Phase = agentboxproto.Phase

// Phase constants for agent status.
const (
	PhaseInitializing  = agentboxproto.PhaseInitializing
	PhaseExecuting     = agentboxproto.PhaseExecuting
	PhaseVerifying     = agentboxproto.PhaseVerifying
	PhaseAwaitingInput = agentboxproto.PhaseAwaitingInput
	PhaseCreatingPRs   = fleetproto.PhaseCreatingPRs
	PhaseComplete      = agentboxproto.PhaseComplete
	PhaseFailed        = agentboxproto.PhaseFailed
	PhaseCancelled     = agentboxproto.PhaseCancelled
)

// --- Status (Agent → Worker) ---

// AgentStatus is a lightweight status indicator (alias of agentbox type).
type AgentStatus = agentboxproto.AgentStatus

// StatusProgress tracks completion within a phase (alias of agentbox type).
type StatusProgress = agentboxproto.StatusProgress

// --- Steering (Worker → Agent) ---

// SteeringAction is a typed string for steering actions (alias of agentbox type).
type SteeringAction = agentboxproto.SteeringAction

// SteeringAction constants.
const (
	SteeringActionSteer   = agentboxproto.SteeringActionSteer
	SteeringActionApprove = agentboxproto.SteeringActionApprove
	SteeringActionReject  = agentboxproto.SteeringActionReject
	SteeringActionCancel  = agentboxproto.SteeringActionCancel
)

// SteeringInstruction extends agentbox's SteeringInstruction with a Timestamp field.
type SteeringInstruction = fleetproto.SteeringInstruction

// --- Manifest (Worker → Agent) ---

// TaskManifest is the full task definition (re-exported from fleetproto).
type TaskManifest = fleetproto.TaskManifest

// ManifestRepo defines a repository to clone.
type ManifestRepo = fleetproto.ManifestRepo

// ForEachTarget is a target for iteration within a repository.
type ForEachTarget = fleetproto.ForEachTarget

// ManifestExecution specifies what to run.
type ManifestExecution = fleetproto.ManifestExecution

// ManifestVerifier defines a verification command.
type ManifestVerifier = fleetproto.ManifestVerifier

// ManifestPRConfig contains PR creation settings.
type ManifestPRConfig = fleetproto.ManifestPRConfig

// ManifestGitConfig contains git identity settings.
type ManifestGitConfig = fleetproto.ManifestGitConfig

// --- Result (Agent → Worker) ---

// AgentResult is the full structured result written by the agent.
type AgentResult = fleetproto.AgentResult

// RepoResult contains the result for a single repository.
type RepoResult = fleetproto.RepoResult

// DiffEntry represents a single file's diff.
type DiffEntry = fleetproto.DiffEntry

// VerifierResult is the result of running a single verifier.
type VerifierResult = fleetproto.VerifierResult

// ReportResult contains report-mode output for a repo.
type ReportResult = fleetproto.ReportResult

// ForEachResult contains the result for a single forEach target.
type ForEachResult = fleetproto.ForEachResult

// PRInfo contains information about a created pull request.
type PRInfo = fleetproto.PRInfo

// SteeringRecord records a single steering interaction.
type SteeringRecord = fleetproto.SteeringRecord

// MaxDiffLinesPerFile is the default truncation limit for per-file diffs.
const MaxDiffLinesPerFile = fleetproto.MaxDiffLinesPerFile
