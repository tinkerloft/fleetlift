package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestRenderPrompt(t *testing.T) {
	ctx := RenderContext{
		Params: map[string]any{"issue_body": "Login broken"},
		Steps:  map[string]*model.StepOutput{},
	}
	out, err := RenderPrompt("Fix: {{ .Params.issue_body }}", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Fix: Login broken", out)
}

func TestRenderUnknownVar(t *testing.T) {
	ctx := RenderContext{Params: map[string]any{}}
	_, err := RenderPrompt("{{ .Params.missing }}", ctx)
	require.Error(t, err)
}

func TestTruncate_MultiByte(t *testing.T) {
	// "日本語" is 3 runes, 9 bytes
	result := truncate(2, "日本語")
	assert.Equal(t, "日本", result)
	assert.Equal(t, 2, len([]rune(result)))
}
