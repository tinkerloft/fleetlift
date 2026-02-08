package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestBuildManifest_Basic(t *testing.T) {
	task := model.Task{
		Version: 1,
		ID:      "slog-migration",
		Title:   "Migrate to slog",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{
				Prompt: "Migrate from log.Printf to slog",
				Verifiers: []model.Verifier{
					{Name: "build", Command: []string{"go", "build", "./..."}},
				},
			},
		},
		Timeout:               "30m",
		RequireApproval:       true,
		MaxSteeringIterations: 3,
		PullRequest: &model.PullRequestConfig{
			BranchPrefix: "auto/slog",
			Title:        "Migrate to slog",
			Labels:       []string{"automated"},
		},
	}

	manifest := BuildManifest(task)

	assert.Equal(t, "slog-migration", manifest.TaskID)
	assert.Equal(t, "transform", manifest.Mode)
	assert.Equal(t, "Migrate to slog", manifest.Title)
	assert.Equal(t, 1800, manifest.TimeoutSeconds)
	assert.True(t, manifest.RequireApproval)
	assert.Equal(t, 3, manifest.MaxSteeringIterations)

	// Repositories
	require.Len(t, manifest.Repositories, 1)
	assert.Equal(t, "svc", manifest.Repositories[0].Name)
	assert.Equal(t, "main", manifest.Repositories[0].Branch)

	// Execution
	assert.Equal(t, "agentic", manifest.Execution.Type)
	assert.Equal(t, "Migrate from log.Printf to slog", manifest.Execution.Prompt)

	// Verifiers
	require.Len(t, manifest.Verifiers, 1)
	assert.Equal(t, "build", manifest.Verifiers[0].Name)

	// PR config
	require.NotNil(t, manifest.PullRequest)
	assert.Equal(t, "auto/slog", manifest.PullRequest.BranchPrefix)
	assert.Equal(t, []string{"automated"}, manifest.PullRequest.Labels)

	// Git config defaults
	assert.NotEmpty(t, manifest.GitConfig.UserEmail)
	assert.NotEmpty(t, manifest.GitConfig.UserName)
	assert.Equal(t, 50, manifest.GitConfig.CloneDepth)
}

func TestBuildManifest_Deterministic(t *testing.T) {
	task := model.Task{
		ID:    "rewrite-task",
		Title: "Run OpenRewrite",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Name: "svc"},
		},
		Execution: model.Execution{
			Deterministic: &model.DeterministicExecution{
				Image:   "openrewrite/rewrite:latest",
				Args:    []string{"rewrite:run"},
				Env:     map[string]string{"JAVA_HOME": "/usr/lib/jvm"},
				Command: []string{"mvn"},
			},
		},
	}

	manifest := BuildManifest(task)

	assert.Equal(t, "deterministic", manifest.Execution.Type)
	assert.Equal(t, "openrewrite/rewrite:latest", manifest.Execution.Image)
	assert.Equal(t, []string{"rewrite:run"}, manifest.Execution.Args)
	assert.Equal(t, map[string]string{"JAVA_HOME": "/usr/lib/jvm"}, manifest.Execution.Env)
	assert.Equal(t, []string{"mvn"}, manifest.Execution.Command)
}

func TestBuildManifest_TransformationRepo(t *testing.T) {
	task := model.Task{
		ID:    "classify",
		Title: "Classify endpoints",
		Mode:  model.TaskModeReport,
		Transformation: &model.Repository{
			URL:    "https://github.com/org/tools.git",
			Branch: "main",
			Name:   "tools",
			Setup:  []string{"npm install"},
		},
		Targets: []model.Repository{
			{URL: "https://github.com/org/svc-a.git", Name: "svc-a"},
			{URL: "https://github.com/org/svc-b.git", Name: "svc-b"},
		},
		ForEach: []model.ForEachTarget{
			{Name: "users-api", Context: "GET /users"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Classify"},
		},
	}

	manifest := BuildManifest(task)

	assert.Equal(t, "report", manifest.Mode)
	require.NotNil(t, manifest.Transformation)
	assert.Equal(t, "tools", manifest.Transformation.Name)
	assert.Equal(t, []string{"npm install"}, manifest.Transformation.Setup)

	require.Len(t, manifest.Targets, 2)
	assert.Equal(t, "svc-a", manifest.Targets[0].Name)

	require.Len(t, manifest.ForEach, 1)
	assert.Equal(t, "users-api", manifest.ForEach[0].Name)

	// Repositories should be empty when Transformation is set
	assert.Empty(t, manifest.Repositories, "Repositories should not be populated when Transformation is set")
}

func TestBuildManifest_DefaultMaxSteering(t *testing.T) {
	task := model.Task{
		ID:    "test",
		Title: "Test",
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "test"},
		},
	}

	manifest := BuildManifest(task)
	assert.Equal(t, 5, manifest.MaxSteeringIterations)
}
