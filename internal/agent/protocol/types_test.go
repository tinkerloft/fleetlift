package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskManifestJSON(t *testing.T) {
	manifest := TaskManifest{
		TaskID: "slog-migration",
		Mode:   "transform",
		Title:  "Migrate to slog",
		Repositories: []ManifestRepo{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: ManifestExecution{
			Type:   "agentic",
			Prompt: "Migrate from log.Printf to slog",
		},
		Verifiers: []ManifestVerifier{
			{Name: "build", Command: []string{"go", "build", "./..."}},
		},
		TimeoutSeconds:        1800,
		RequireApproval:       true,
		MaxSteeringIterations: 5,
		PullRequest: &ManifestPRConfig{
			BranchPrefix: "auto/slog",
			Title:        "Migrate to slog",
			Labels:       []string{"automated"},
		},
		GitConfig: ManifestGitConfig{
			UserEmail:  "agent@test.com",
			UserName:   "Test Agent",
			CloneDepth: 50,
		},
	}

	data, err := json.Marshal(manifest)
	require.NoError(t, err)

	var decoded TaskManifest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, manifest.TaskID, decoded.TaskID)
	assert.Equal(t, manifest.Mode, decoded.Mode)
	assert.Equal(t, manifest.Title, decoded.Title)
	assert.Len(t, decoded.Repositories, 1)
	assert.Equal(t, "svc", decoded.Repositories[0].Name)
	assert.Equal(t, "agentic", decoded.Execution.Type)
	assert.Len(t, decoded.Verifiers, 1)
	assert.True(t, decoded.RequireApproval)
	assert.Equal(t, 5, decoded.MaxSteeringIterations)
	assert.NotNil(t, decoded.PullRequest)
	assert.Equal(t, "auto/slog", decoded.PullRequest.BranchPrefix)
	assert.Equal(t, 50, decoded.GitConfig.CloneDepth)
}

func TestTaskManifestWithTransformation(t *testing.T) {
	manifest := TaskManifest{
		TaskID: "classify",
		Mode:   "report",
		Transformation: &ManifestRepo{
			URL:    "https://github.com/org/tools.git",
			Branch: "main",
			Name:   "tools",
		},
		Targets: []ManifestRepo{
			{URL: "https://github.com/org/svc-a.git", Name: "svc-a"},
			{URL: "https://github.com/org/svc-b.git", Name: "svc-b"},
		},
		ForEach: []ForEachTarget{
			{Name: "endpoint-1", Context: "GET /users"},
		},
		Execution: ManifestExecution{Type: "agentic", Prompt: "Classify"},
	}

	data, err := json.Marshal(manifest)
	require.NoError(t, err)

	var decoded TaskManifest
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.NotNil(t, decoded.Transformation)
	assert.Equal(t, "tools", decoded.Transformation.Name)
	assert.Len(t, decoded.Targets, 2)
	assert.Len(t, decoded.ForEach, 1)
	assert.Equal(t, "endpoint-1", decoded.ForEach[0].Name)
}

func TestAgentStatusJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	status := AgentStatus{
		Phase:   PhaseExecuting,
		Step:    "running_claude_code",
		Message: "Running Claude Code on svc...",
		Progress: &StatusProgress{
			CompletedRepos: 1,
			TotalRepos:     3,
		},
		Iteration: 0,
		UpdatedAt: now,
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded AgentStatus
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, PhaseExecuting, decoded.Phase)
	assert.Equal(t, "running_claude_code", decoded.Step)
	assert.NotNil(t, decoded.Progress)
	assert.Equal(t, 1, decoded.Progress.CompletedRepos)
	assert.Equal(t, 3, decoded.Progress.TotalRepos)
}

func TestAgentResultJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	errMsg := "verifier build failed"
	result := AgentResult{
		Status: PhaseAwaitingInput,
		Repositories: []RepoResult{
			{
				Name:          "svc",
				Status:        "success",
				FilesModified: []string{"pkg/logger/logger.go"},
				Diffs: []DiffEntry{
					{Path: "pkg/logger/logger.go", Status: "modified", Additions: 15, Deletions: 8, Diff: "..."},
				},
				VerifierResults: []VerifierResult{
					{Name: "build", Success: true, ExitCode: 0},
				},
			},
			{
				Name:   "svc-b",
				Status: "failed",
				Error:  &errMsg,
			},
		},
		SteeringHistory: []SteeringRecord{
			{Iteration: 1, Prompt: "Also update test helpers", Timestamp: now},
		},
		StartedAt: now,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded AgentResult
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, PhaseAwaitingInput, decoded.Status)
	assert.Len(t, decoded.Repositories, 2)
	assert.Equal(t, "svc", decoded.Repositories[0].Name)
	assert.Len(t, decoded.Repositories[0].Diffs, 1)
	assert.Equal(t, 15, decoded.Repositories[0].Diffs[0].Additions)
	assert.NotNil(t, decoded.Repositories[1].Error)
	assert.Len(t, decoded.SteeringHistory, 1)
}

func TestSteeringInstructionJSON(t *testing.T) {
	instruction := SteeringInstruction{
		Action:    SteeringActionSteer,
		Prompt:    "Also update test helpers",
		Iteration: 1,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(instruction)
	require.NoError(t, err)

	var decoded SteeringInstruction
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, SteeringActionSteer, decoded.Action)
	assert.Equal(t, "Also update test helpers", decoded.Prompt)
	assert.Equal(t, 1, decoded.Iteration)
}

func TestPhaseConstants(t *testing.T) {
	phases := []Phase{
		PhaseInitializing, PhaseExecuting, PhaseVerifying,
		PhaseAwaitingInput, PhaseCreatingPRs, PhaseComplete,
		PhaseFailed, PhaseCancelled,
	}
	// Verify uniqueness
	seen := make(map[Phase]bool)
	for _, p := range phases {
		assert.False(t, seen[p], "duplicate phase: %s", p)
		seen[p] = true
	}
}

func TestSteeringActionConstants(t *testing.T) {
	actions := []SteeringAction{
		SteeringActionSteer, SteeringActionApprove,
		SteeringActionReject, SteeringActionCancel,
	}
	seen := make(map[SteeringAction]bool)
	for _, a := range actions {
		assert.False(t, seen[a], "duplicate action: %s", a)
		seen[a] = true
	}
}

func TestRepoResultWithReport(t *testing.T) {
	result := RepoResult{
		Name:   "svc",
		Status: "success",
		Report: &ReportResult{
			Frontmatter: map[string]any{"score": 8, "issues": []string{"none"}},
			Body:        "# Analysis\nAll good.",
			Raw:         "---\nscore: 8\n---\n# Analysis\nAll good.",
		},
		ForEachResults: []ForEachResult{
			{
				Target: ForEachTarget{Name: "users-api", Context: "/users endpoint"},
				Report: &ReportResult{Raw: "---\nstatus: ok\n---\n"},
			},
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded RepoResult
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.NotNil(t, decoded.Report)
	assert.Equal(t, "# Analysis\nAll good.", decoded.Report.Body)
	assert.Len(t, decoded.ForEachResults, 1)
	assert.Equal(t, "users-api", decoded.ForEachResults[0].Target.Name)
}
