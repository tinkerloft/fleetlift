package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// RenderContext holds the data available during prompt template rendering.
type RenderContext struct {
	Params map[string]any
	Steps  map[string]*model.StepOutput
}

// RenderPrompt resolves Go template expressions in a prompt string.
func RenderPrompt(tmpl string, ctx RenderContext) (string, error) {
	t, err := template.New("prompt").
		Funcs(templateFuncs()).
		Option("missingkey=error").
		Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"toJSON":   toJSON,
		"truncate": truncate,
		"join":     strings.Join,
	}
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func truncate(max int, s string) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
