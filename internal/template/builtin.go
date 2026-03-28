package template

import (
	"context"
	"embed"
	"fmt"
	"path"

	"github.com/tinkerloft/fleetlift/internal/model"
	"gopkg.in/yaml.v3"
)

//go:embed workflows/*.yaml
var workflowFiles embed.FS

// BuiltinProvider serves embedded workflow templates.
type BuiltinProvider struct {
	templates []builtinEntry
}

type builtinEntry struct {
	tmpl   *model.WorkflowTemplate
	hidden bool
}

func NewBuiltinProvider() (*BuiltinProvider, error) {
	entries, err := workflowFiles.ReadDir("workflows")
	if err != nil {
		return nil, err
	}
	var templates []builtinEntry
	for _, e := range entries {
		data, err := workflowFiles.ReadFile(path.Join("workflows", e.Name()))
		if err != nil {
			return nil, err
		}
		var def model.WorkflowDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("parse builtin %s: %w", e.Name(), err)
		}
		templates = append(templates, builtinEntry{
			tmpl: &model.WorkflowTemplate{
				ID:          def.ID,
				Slug:        def.ID,
				Title:       def.Title,
				Description: def.Description,
				Tags:        def.Tags,
				YAMLBody:    string(data),
				Builtin:     true,
			},
			hidden: def.Hidden,
		})
	}
	return &BuiltinProvider{templates: templates}, nil
}

func (b *BuiltinProvider) Name() string   { return "builtin" }
func (b *BuiltinProvider) Writable() bool { return false }

func (b *BuiltinProvider) List(_ context.Context, _ string) ([]*model.WorkflowTemplate, error) {
	out := make([]*model.WorkflowTemplate, 0, len(b.templates))
	for _, t := range b.templates {
		if t.hidden {
			continue
		}
		out = append(out, t.tmpl)
	}
	return out, nil
}

func (b *BuiltinProvider) Get(_ context.Context, _, slug string) (*model.WorkflowTemplate, error) {
	for _, t := range b.templates {
		if t.tmpl.Slug == slug {
			return t.tmpl, nil
		}
	}
	return nil, ErrNotFound
}

func (b *BuiltinProvider) Save(_ context.Context, _ string, _ *model.WorkflowTemplate) error {
	return fmt.Errorf("builtin provider is read-only")
}

func (b *BuiltinProvider) Delete(_ context.Context, _, _ string) error {
	return fmt.Errorf("builtin provider is read-only")
}
