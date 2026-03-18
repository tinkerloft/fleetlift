package activity_test

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

type stubProfileStore struct {
	profiles map[string]*model.AgentProfile
}

func (s *stubProfileStore) GetProfile(ctx context.Context, teamID, name string) (*model.AgentProfile, error) {
	if p, ok := s.profiles["team:"+teamID+":"+name]; ok {
		return p, nil
	}
	if p, ok := s.profiles["system:"+name]; ok {
		return p, nil
	}
	return nil, nil
}

func TestResolveProfile_BaselineOnly(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{
		"system:baseline": {Body: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/base"}},
		}},
	}}
	acts := &activity.Activities{ProfileStore: store}
	result, err := acts.ResolveAgentProfile(context.Background(), workflow.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plugins) != 1 || result.Plugins[0].Plugin != "plugins/base" {
		t.Errorf("unexpected plugins: %+v", result.Plugins)
	}
}

func TestResolveProfile_WorkflowProfileMerged(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{
		"system:baseline": {Body: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/base"}},
		}},
		"system:helm-auditor": {Body: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/miro-helm-doctor"}},
		}},
	}}
	acts := &activity.Activities{ProfileStore: store}
	result, err := acts.ResolveAgentProfile(context.Background(), workflow.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "helm-auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d: %+v", len(result.Plugins), result.Plugins)
	}
}

func TestResolveProfile_TeamScopedWinsOverSystem(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{
		"system:baseline": {Body: model.AgentProfileBody{
			MCPs: []model.MCPConfig{{Name: "system-mcp"}},
		}},
		"team:team-1:baseline": {Body: model.AgentProfileBody{
			MCPs: []model.MCPConfig{{Name: "team-mcp"}},
		}},
	}}
	acts := &activity.Activities{ProfileStore: store}
	result, err := acts.ResolveAgentProfile(context.Background(), workflow.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPs) != 1 || result.MCPs[0].Name != "team-mcp" {
		t.Errorf("expected team-scoped baseline to win, got: %+v", result.MCPs)
	}
}

func TestResolveProfile_MissingProfileErrors(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{}}
	acts := &activity.Activities{ProfileStore: store}
	_, err := acts.ResolveAgentProfile(context.Background(), workflow.ResolveProfileInput{
		TeamID: "team-1", ProfileName: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestResolveProfile_MissingProfile_IsNonRetryable(t *testing.T) {
	store := &stubProfileStore{profiles: map[string]*model.AgentProfile{}}
	acts := &activity.Activities{ProfileStore: store}
	_, err := acts.ResolveAgentProfile(context.Background(), workflow.ResolveProfileInput{
		TeamID:      "team-1",
		ProfileName: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Errorf("expected NonRetryableApplicationError, got: %T %v", err, err)
	}
}
