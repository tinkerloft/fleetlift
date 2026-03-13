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
	templates []*model.WorkflowTemplate
}

func NewBuiltinProvider() (*BuiltinProvider, error) {
	entries, err := workflowFiles.ReadDir("workflows")
	if err != nil {
		return nil, err
	}
	var templates []*model.WorkflowTemplate
	for _, e := range entries {
		data, err := workflowFiles.ReadFile(path.Join("workflows", e.Name()))
		if err != nil {
			return nil, err
		}
		var def model.WorkflowDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("parse builtin %s: %w", e.Name(), err)
		}
		templates = append(templates, &model.WorkflowTemplate{
			ID:          def.ID,
			Slug:        def.ID,
			Title:       def.Title,
			Description: def.Description,
			Tags:        def.Tags,
			YAMLBody:    string(data),
			Builtin:     true,
		})
	}
	return &BuiltinProvider{templates: templates}, nil
}

func (b *BuiltinProvider) Name() string  { return "builtin" }
func (b *BuiltinProvider) Writable() bool { return false }

func (b *BuiltinProvider) List(_ context.Context, _ string) ([]*model.WorkflowTemplate, error) {
	out := make([]*model.WorkflowTemplate, len(b.templates))
	copy(out, b.templates)
	return out, nil
}

func (b *BuiltinProvider) Get(_ context.Context, _, slug string) (*model.WorkflowTemplate, error) {
	for _, t := range b.templates {
		if t.Slug == slug {
			return t, nil
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
