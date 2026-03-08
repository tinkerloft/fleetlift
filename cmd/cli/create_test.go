package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractYAML_PlainYAML(t *testing.T) {
	input := "version: 1\ntitle: \"Test\"\n"
	result := extractYAML(input)
	assert.Equal(t, input, result)
}

func TestExtractYAML_StripsMarkdownFence(t *testing.T) {
	input := "Here is the task:\n\n```yaml\nversion: 1\ntitle: \"Test\"\n```\n\nDone."
	result := extractYAML(input)
	assert.Equal(t, "version: 1\ntitle: \"Test\"\n", result)
}

func TestExtractYAML_StripsPlainFence(t *testing.T) {
	input := "```\nversion: 1\ntitle: \"Test\"\n```"
	result := extractYAML(input)
	assert.Equal(t, "version: 1\ntitle: \"Test\"\n", result)
}

func TestExtractYAML_EmptyInput(t *testing.T) {
	result := extractYAML("")
	assert.Equal(t, "", result)
}

func TestValidateTaskYAML_ValidTransform(t *testing.T) {
	yaml := `version: 1
title: "Test Task"
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: "Do the thing"
`
	task, err := validateTaskYAML(yaml)
	require.NoError(t, err)
	assert.Equal(t, "Test Task", task.Title)
	assert.Len(t, task.Repositories, 1)
}

func TestValidateTaskYAML_MissingTitle(t *testing.T) {
	yaml := `version: 1
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: "Do the thing"
`
	_, err := validateTaskYAML(yaml)
	assert.ErrorContains(t, err, "title")
}

func TestValidateTaskYAML_MissingExecution(t *testing.T) {
	yaml := `version: 1
title: "Test"
repositories:
  - url: https://github.com/org/repo.git
`
	_, err := validateTaskYAML(yaml)
	assert.ErrorContains(t, err, "execution")
}

func TestValidateTaskYAML_MissingRepositories(t *testing.T) {
	yaml := `version: 1
title: "Test"
execution:
  agentic:
    prompt: "Do the thing"
`
	_, err := validateTaskYAML(yaml)
	assert.ErrorContains(t, err, "repositor")
}

func TestValidateTaskYAML_GroupsOnlyTask(t *testing.T) {
	yaml := `version: 1
title: "Test"
groups:
  - name: my-group
    repositories:
      - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: "Do the thing"
`
	task, err := validateTaskYAML(yaml)
	require.NoError(t, err)
	assert.Equal(t, "Test", task.Title)
}

func TestBuildSystemPrompt_ContainsSchema(t *testing.T) {
	prompt := buildSystemPrompt()
	assert.Contains(t, prompt, "version: 1")
	assert.True(t, strings.Contains(prompt, "example-transform") || strings.Contains(prompt, "mode: transform"))
}

func TestRunCreate_RunFlagRequiresOutput(t *testing.T) {
	// Create a fresh command to avoid modifying global flag state.
	cmd := &cobra.Command{}
	cmd.Flags().String("describe", "test task", "")
	cmd.Flags().StringArray("repo", nil, "")
	cmd.Flags().String("output", "", "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("run", true, "")

	err := runCreate(cmd, nil)
	require.ErrorContains(t, err, "--run requires --output")
}

func TestHasGenerationMarker_Present(t *testing.T) {
	assert.True(t, hasGenerationMarker("Here is the task:\n---YAML---\nversion: 1\n"))
}

func TestHasGenerationMarker_Absent(t *testing.T) {
	assert.False(t, hasGenerationMarker("Just a question, no YAML yet."))
}

func TestExtractYAMLFromMarker_Basic(t *testing.T) {
	response := "I have enough info.\n---YAML---\nversion: 1\ntitle: Test\n"
	result := extractYAMLFromMarker(response)
	assert.Equal(t, "version: 1\ntitle: Test\n", result)
}

func TestExtractYAMLFromMarker_StripsFences(t *testing.T) {
	response := "Ready.\n---YAML---\n```yaml\nversion: 1\ntitle: Test\n```\n"
	result := extractYAMLFromMarker(response)
	assert.Equal(t, "version: 1\ntitle: Test\n", result)
}

func TestExtractYAMLFromMarker_NoMarker(t *testing.T) {
	result := extractYAMLFromMarker("no marker here")
	assert.Equal(t, "", result)
}

func TestBuildInteractiveSystemPrompt_ContainsMarkerInstruction(t *testing.T) {
	prompt := buildInteractiveSystemPrompt()
	assert.Contains(t, prompt, "---YAML---")
	assert.Contains(t, prompt, "one at a time")
	// Also inherits schema from buildSystemPrompt
	assert.Contains(t, prompt, "version: 1")
}

func TestApplyRepoOverrides_ReplacesRepositories(t *testing.T) {
	yamlStr := `version: 1
title: "Test"
repositories:
  - url: https://github.com/your-org/your-repo.git
execution:
  agentic:
    prompt: "do it"
`
	result, err := applyRepoOverrides(yamlStr, []string{"https://github.com/acme/svc.git"})
	require.NoError(t, err)
	assert.Contains(t, result, "acme/svc.git")
	assert.NotContains(t, result, "your-org")
}

func TestApplyRepoOverrides_NoRepos_ReturnsUnchanged(t *testing.T) {
	yamlStr := "version: 1\ntitle: T\nrepositories:\n  - url: https://github.com/x/y.git\nexecution:\n  agentic:\n    prompt: p\n"
	result, err := applyRepoOverrides(yamlStr, nil)
	require.NoError(t, err)
	assert.Equal(t, yamlStr, result)
}
