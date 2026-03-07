package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// extractYAML strips markdown code fences from an LLM response, returning only the YAML content.
func extractYAML(response string) string {
	if idx := strings.Index(response, "```yaml"); idx != -1 {
		start := idx + len("```yaml")
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimLeft(response[start:start+end], "\n")
		}
	}
	if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimLeft(response[start:start+end], "\n")
		}
	}
	return response
}

// validateTaskYAML parses and validates the required fields of a Fleetlift task YAML string.
func validateTaskYAML(yamlStr string) (model.Task, error) {
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
	hasRepos := len(task.Repositories) > 0 || len(task.Targets) > 0
	if !hasRepos {
		return model.Task{}, fmt.Errorf("task is missing required field: repositories (at least one repo)")
	}
	return task, nil
}

// buildSystemPrompt builds the system prompt for the LLM that generates task YAML.
func buildSystemPrompt() string {
	return strings.Join([]string{
		"You are an expert at writing Fleetlift task YAML files.",
		"Generate ONLY valid YAML — no markdown fences, no explanations, no prose.",
		"Use the schema and examples below as your reference.",
		"",
		"# Task YAML Schema",
		taskSchema,
		"",
		"# Example: Transform Task",
		exampleTransform,
		"",
		"# Example: Report Task",
		exampleReport,
	}, "\n")
}
