package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowDefParse(t *testing.T) {
	raw := `
version: 1
id: test-wf
title: Test
parameters:
  - name: repo_url
    type: string
    required: true
steps:
  - id: analyze
    mode: report
    repositories:
      - url: "{{ .Params.repo_url }}"
    execution:
      agent: claude-code
      prompt: "Analyze the code"
`
	var def WorkflowDef
	err := ParseWorkflowYAML([]byte(raw), &def)
	require.NoError(t, err)
	assert.Equal(t, "test-wf", def.ID)
	assert.Len(t, def.Parameters, 1)
	assert.Len(t, def.Steps, 1)
	assert.Equal(t, "analyze", def.Steps[0].ID)
}

func TestParseWorkflowYAML_Roundtrip(t *testing.T) {
	raw := `
version: 1
id: fleet-transform
title: Transform repositories
steps:
  - id: transform
    mode: transform
    repositories: "{{ .Params.repos }}"
    execution:
      agent: claude-code
      prompt: "Fix all TODO comments"
`
	var def WorkflowDef
	err := ParseWorkflowYAML([]byte(raw), &def)
	assert.NoError(t, err)
	assert.Equal(t, "fleet-transform", def.ID)
	assert.Len(t, def.Steps, 1)
	assert.Equal(t, "transform", def.Steps[0].ID)
}
