package activity

import (
	"os"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// BuildManifest converts a model.Task to a protocol.TaskManifest for the sidecar agent.
func BuildManifest(task model.Task) protocol.TaskManifest {
	manifest := protocol.TaskManifest{
		TaskID:                task.ID,
		Mode:                  string(task.GetMode()),
		Title:                 task.Title,
		TimeoutSeconds:        task.GetTimeoutMinutes() * 60,
		RequireApproval:       task.RequireApproval,
		MaxSteeringIterations: task.MaxSteeringIterations,
		GitConfig:             buildGitConfig(),
	}

	if manifest.MaxSteeringIterations <= 0 {
		manifest.MaxSteeringIterations = 5
	}

	// Repositories
	if task.Transformation != nil {
		manifest.Transformation = convertRepo(task.Transformation)
		for _, t := range task.Targets {
			manifest.Targets = append(manifest.Targets, *convertRepo(&t))
		}
	} else {
		for _, r := range task.GetEffectiveRepositories() {
			manifest.Repositories = append(manifest.Repositories, *convertRepo(&r))
		}
	}

	// ForEach
	for _, fe := range task.ForEach {
		manifest.ForEach = append(manifest.ForEach, protocol.ForEachTarget{
			Name:    fe.Name,
			Context: fe.Context,
		})
	}

	// Execution
	manifest.Execution = buildExecution(task.Execution)

	// Verifiers
	for _, v := range task.Execution.GetVerifiers() {
		manifest.Verifiers = append(manifest.Verifiers, protocol.ManifestVerifier{
			Name:    v.Name,
			Command: v.Command,
		})
	}

	// PR config
	if task.PullRequest != nil {
		manifest.PullRequest = &protocol.ManifestPRConfig{
			BranchPrefix: task.PullRequest.BranchPrefix,
			Title:        task.PullRequest.Title,
			Body:         task.PullRequest.Body,
			Labels:       task.PullRequest.Labels,
			Reviewers:    task.PullRequest.Reviewers,
		}
	}

	return manifest
}


func convertRepo(r *model.Repository) *protocol.ManifestRepo {
	if r == nil {
		return nil
	}
	return &protocol.ManifestRepo{
		URL:    r.URL,
		Branch: r.Branch,
		Name:   r.Name,
		Setup:  r.Setup,
	}
}

func buildExecution(exec model.Execution) protocol.ManifestExecution {
	if exec.Deterministic != nil {
		return protocol.ManifestExecution{
			Type:    "deterministic",
			Image:   exec.Deterministic.Image,
			Args:    exec.Deterministic.Args,
			Env:     exec.Deterministic.Env,
			Command: exec.Deterministic.Command,
		}
	}
	if exec.Agentic != nil {
		return protocol.ManifestExecution{
			Type:   "agentic",
			Prompt: exec.Agentic.Prompt,
		}
	}
	return protocol.ManifestExecution{Type: "agentic"}
}

func buildGitConfig() protocol.ManifestGitConfig {
	email := os.Getenv("GIT_USER_EMAIL")
	if email == "" {
		email = DefaultGitEmail
	}
	name := os.Getenv("GIT_USER_NAME")
	if name == "" {
		name = DefaultGitName
	}
	return protocol.ManifestGitConfig{
		UserEmail:  email,
		UserName:   name,
		CloneDepth: agent.DefaultCloneDepth,
	}
}
