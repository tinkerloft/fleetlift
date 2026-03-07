package main

import (
	"strings"
	"testing"

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

func TestBuildSystemPrompt_ContainsSchema(t *testing.T) {
	prompt := buildSystemPrompt()
	assert.True(t, strings.Contains(prompt, "version: 1"))
	assert.True(t, strings.Contains(prompt, "example-transform") || strings.Contains(prompt, "mode: transform"))
}
