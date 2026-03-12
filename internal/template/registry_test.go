package template

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// stubProvider is a simple in-memory Provider for testing.
type stubProvider struct {
	name      string
	writable  bool
	templates map[string]*model.WorkflowTemplate
}

func newStubProvider(name string, writable bool, templates ...*model.WorkflowTemplate) *stubProvider {
	m := make(map[string]*model.WorkflowTemplate, len(templates))
	for _, t := range templates {
		m[t.Slug] = t
	}
	return &stubProvider{name: name, writable: writable, templates: m}
}

func (s *stubProvider) Name() string  { return s.name }
func (s *stubProvider) Writable() bool { return s.writable }

func (s *stubProvider) List(_ context.Context, _ string) ([]*model.WorkflowTemplate, error) {
	out := make([]*model.WorkflowTemplate, 0, len(s.templates))
	for _, t := range s.templates {
		out = append(out, t)
	}
	return out, nil
}

func (s *stubProvider) Get(_ context.Context, _, slug string) (*model.WorkflowTemplate, error) {
	if t, ok := s.templates[slug]; ok {
		return t, nil
	}
	return nil, ErrNotFound
}

func (s *stubProvider) Save(_ context.Context, _ string, t *model.WorkflowTemplate) error {
	s.templates[t.Slug] = t
	return nil
}

func (s *stubProvider) Delete(_ context.Context, _, slug string) error {
	if _, ok := s.templates[slug]; !ok {
		return ErrNotFound
	}
	delete(s.templates, slug)
	return nil
}

func tmpl(slug, title string) *model.WorkflowTemplate {
	return &model.WorkflowTemplate{Slug: slug, Title: title}
}

// TestRegistry_GetPriority verifies that a higher-priority (later) provider
// overrides a lower-priority provider for the same slug.
func TestRegistry_GetPriority(t *testing.T) {
	low := newStubProvider("low", false, tmpl("shared", "Low Title"), tmpl("only-low", "Only Low"))
	high := newStubProvider("high", false, tmpl("shared", "High Title"))

	// low is index 0 (lower priority), high is index 1 (higher priority)
	reg := NewRegistry(low, high)

	got, err := reg.Get(context.Background(), "team1", "shared")
	require.NoError(t, err)
	assert.Equal(t, "High Title", got.Title, "higher-priority provider should win")
}

// TestRegistry_GetFallthrough verifies that when the highest-priority provider
// returns ErrNotFound, the registry falls through to the next provider.
func TestRegistry_GetFallthrough(t *testing.T) {
	low := newStubProvider("low", false, tmpl("fallback-slug", "Fallback"))
	high := newStubProvider("high", false) // has no templates

	reg := NewRegistry(low, high)

	got, err := reg.Get(context.Background(), "team1", "fallback-slug")
	require.NoError(t, err)
	assert.Equal(t, "Fallback", got.Title, "should fall through to low-priority provider")
}

// TestRegistry_GetNotFound verifies ErrNotFound when no provider has the slug.
func TestRegistry_GetNotFound(t *testing.T) {
	p := newStubProvider("p", false, tmpl("existing", "Existing"))
	reg := NewRegistry(p)

	_, err := reg.Get(context.Background(), "team1", "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestRegistry_ListMerge verifies that List merges providers and higher-priority
// entries win for overlapping slugs.
func TestRegistry_ListMerge(t *testing.T) {
	low := newStubProvider("low", false,
		tmpl("shared", "Low Title"),
		tmpl("only-low", "Only Low"),
	)
	high := newStubProvider("high", false,
		tmpl("shared", "High Title"),
		tmpl("only-high", "Only High"),
	)

	reg := NewRegistry(low, high)

	templates, err := reg.List(context.Background(), "team1")
	require.NoError(t, err)

	bySlug := make(map[string]*model.WorkflowTemplate, len(templates))
	for _, t := range templates {
		bySlug[t.Slug] = t
	}

	assert.Len(t, bySlug, 3, "merged list should have 3 unique slugs")
	assert.Equal(t, "High Title", bySlug["shared"].Title, "higher-priority should win for shared slug")
	assert.Equal(t, "Only Low", bySlug["only-low"].Title)
	assert.Equal(t, "Only High", bySlug["only-high"].Title)
}

// TestRegistry_WritableProvider_NilWhenNone verifies that WritableProvider
// returns nil when no writable provider is registered.
func TestRegistry_WritableProvider_NilWhenNone(t *testing.T) {
	reg := NewRegistry()
	assert.Nil(t, reg.WritableProvider())

	readOnly := newStubProvider("ro", false, tmpl("a", "A"))
	reg2 := NewRegistry(readOnly)
	assert.Nil(t, reg2.WritableProvider())
}

// TestRegistry_WritableProvider_ReturnsHighestPriority verifies that
// WritableProvider returns the highest-priority writable provider.
func TestRegistry_WritableProvider_ReturnsHighestPriority(t *testing.T) {
	ro := newStubProvider("ro", false, tmpl("a", "A"))
	rw1 := newStubProvider("rw1", true)
	rw2 := newStubProvider("rw2", true)

	// rw2 is highest priority (last index)
	reg := NewRegistry(ro, rw1, rw2)
	wp := reg.WritableProvider()
	require.NotNil(t, wp)
	assert.Equal(t, "rw2", wp.Name())
}
