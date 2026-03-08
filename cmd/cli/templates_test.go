package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinTemplates_Count(t *testing.T) {
	assert.Len(t, builtinTemplates, 4)
}

func TestBuiltinTemplates_AllHaveContent(t *testing.T) {
	for _, tmpl := range builtinTemplates {
		assert.NotEmpty(t, tmpl.Name, "template missing name")
		assert.NotEmpty(t, tmpl.Description, "template %q missing description", tmpl.Name)
		assert.NotEmpty(t, tmpl.Content, "template %q missing content", tmpl.Name)
	}
}

func TestBuiltinTemplates_ContentIsValidYAML(t *testing.T) {
	for _, tmpl := range builtinTemplates {
		_, err := validateTaskYAML(tmpl.Content)
		require.NoError(t, err, "template %q has invalid YAML", tmpl.Name)
	}
}

func TestFindTemplate_BuiltinFound(t *testing.T) {
	tmpl, err := findTemplate("security-audit")
	require.NoError(t, err)
	assert.Equal(t, "security-audit", tmpl.Name)
}

func TestFindTemplate_NotFound(t *testing.T) {
	_, err := findTemplate("nonexistent-template")
	assert.ErrorContains(t, err, "nonexistent-template")
}

func TestListUserTemplates_NoErrorOnMissingDir(t *testing.T) {
	templates, err := listUserTemplates()
	assert.NoError(t, err)
	_ = templates
}

func TestExtractTemplateDescription_Valid(t *testing.T) {
	content := "version: 1\ndescription: \"My desc\"\ntitle: T\n"
	desc := extractTemplateDescription(content)
	assert.Equal(t, "My desc", desc)
}

func TestExtractTemplateDescription_Missing(t *testing.T) {
	desc := extractTemplateDescription("version: 1\ntitle: T\n")
	assert.Equal(t, "", desc)
}

func TestAllTemplates_ContainsBuiltins(t *testing.T) {
	all := allTemplates()
	names := make([]string, len(all))
	for i, tmpl := range all {
		names[i] = tmpl.Name
	}
	assert.Contains(t, names, "dependency-upgrade")
	assert.Contains(t, names, "api-migration")
	assert.Contains(t, names, "security-audit")
	assert.Contains(t, names, "framework-upgrade")
}
