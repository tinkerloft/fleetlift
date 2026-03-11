package template

import (
	"context"
	"errors"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// ErrNotFound is returned when a workflow template is not found.
var ErrNotFound = errors.New("workflow template not found")

// Provider is the interface for workflow template storage backends.
type Provider interface {
	Name() string
	Writable() bool
	List(ctx context.Context, teamID string) ([]*model.WorkflowTemplate, error)
	Get(ctx context.Context, teamID, slug string) (*model.WorkflowTemplate, error)
	Save(ctx context.Context, teamID string, t *model.WorkflowTemplate) error
	Delete(ctx context.Context, teamID, slug string) error
}

// Registry merges multiple providers, higher-index providers override lower-index.
type Registry struct {
	providers []Provider
}

func NewRegistry(providers ...Provider) *Registry {
	return &Registry{providers: providers}
}

func (r *Registry) List(ctx context.Context, teamID string) ([]*model.WorkflowTemplate, error) {
	seen := map[string]*model.WorkflowTemplate{}
	for _, p := range r.providers {
		items, err := p.List(ctx, teamID)
		if err != nil {
			return nil, err
		}
		for _, t := range items {
			seen[t.Slug] = t // later providers override
		}
	}
	out := make([]*model.WorkflowTemplate, 0, len(seen))
	for _, t := range seen {
		out = append(out, t)
	}
	return out, nil
}

func (r *Registry) Get(ctx context.Context, teamID, slug string) (*model.WorkflowTemplate, error) {
	// Search providers in reverse (highest priority first)
	for i := len(r.providers) - 1; i >= 0; i-- {
		t, err := r.providers[i].Get(ctx, teamID, slug)
		if err == nil && t != nil {
			return t, nil
		}
	}
	return nil, ErrNotFound
}

func (r *Registry) WritableProvider() Provider {
	for i := len(r.providers) - 1; i >= 0; i-- {
		if r.providers[i].Writable() {
			return r.providers[i]
		}
	}
	return nil
}
