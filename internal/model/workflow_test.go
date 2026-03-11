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
	err := parseWorkflowYAML([]byte(raw), &def)
	require.NoError(t, err)
	assert.Equal(t, "test-wf", def.ID)
	assert.Len(t, def.Parameters, 1)
	assert.Len(t, def.Steps, 1)
	assert.Equal(t, "analyze", def.Steps[0].ID)
}
