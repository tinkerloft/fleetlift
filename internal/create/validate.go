package create

import (
	"fmt"

	"github.com/tinkerloft/fleetlift/internal/model"
	"gopkg.in/yaml.v3"
)

// ValidateTaskYAML parses and validates the required fields of a Fleetlift task YAML string.
func ValidateTaskYAML(yamlStr string) (model.Task, error) {
	var task model.Task
	if err := yaml.Unmarshal([]byte(yamlStr), &task); err != nil {
		return model.Task{}, fmt.Errorf("invalid YAML: %w", err)
	}
	if task.Title == "" {
		return model.Task{}, fmt.Errorf("task is missing required field: title")
	}
	if task.Execution.Agentic == nil && task.Execution.Deterministic == nil {
		return model.Task{}, fmt.Errorf("task is missing required field: execution (must have agentic or deterministic)")
	}
	hasRepos := len(task.Repositories) > 0 || len(task.Targets) > 0 || len(task.Groups) > 0
	if !hasRepos {
		return model.Task{}, fmt.Errorf("task is missing required field: repositories (at least one repo)")
	}
	return task, nil
}
