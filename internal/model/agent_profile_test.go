package model_test

import (
	"encoding/json"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestPluginSourceValidate_MarketplaceSource(t *testing.T) {
	p := model.PluginSource{Plugin: "plugins/miro-helm-doctor"}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestPluginSourceValidate_GitHubURLSource(t *testing.T) {
	p := model.PluginSource{GitHubURL: "https://github.com/org/repo/tree/main/plugins/foo"}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestPluginSourceValidate_BothSet(t *testing.T) {
	p := model.PluginSource{Plugin: "plugins/foo", GitHubURL: "https://github.com/org/repo"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error when both fields set")
	}
}

func TestPluginSourceValidate_NeitherSet(t *testing.T) {
	p := model.PluginSource{}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error when neither field set")
	}
}

func TestPluginSourceValidate_RejectsNonHTTPS(t *testing.T) {
	p := model.PluginSource{GitHubURL: "git://github.com/org/repo"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for non-https scheme")
	}
}

func TestAgentProfileBodyJSONRoundTrip(t *testing.T) {
	orig := model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo"}},
		MCPs:    []model.MCPConfig{{Name: "my-mcp", Type: "remote", Transport: "sse", URL: "https://mcp.example.com/sse"}},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got model.AgentProfileBody
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Plugin != "plugins/foo" {
		t.Errorf("plugins mismatch: %+v", got.Plugins)
	}
}

func TestMergeProfiles_Accumulates(t *testing.T) {
	baseline := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/base"}},
		MCPs:    []model.MCPConfig{{Name: "base-mcp"}},
	}
	wp := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/extra"}},
	}
	merged := model.MergeProfiles(baseline, wp)
	if len(merged.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(merged.Plugins))
	}
	if len(merged.MCPs) != 1 {
		t.Errorf("expected 1 MCP from baseline, got %d", len(merged.MCPs))
	}
}

func TestMergeProfiles_LaterLayerWins(t *testing.T) {
	baseline := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo", Marketplace: "old"}},
	}
	wp := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo", Marketplace: "new"}},
	}
	merged := model.MergeProfiles(baseline, wp)
	if len(merged.Plugins) != 1 {
		t.Errorf("expected dedup to 1 plugin, got %d", len(merged.Plugins))
	}
	if merged.Plugins[0].Marketplace != "new" {
		t.Errorf("expected later layer to win, got marketplace=%q", merged.Plugins[0].Marketplace)
	}
}

func TestMergeProfiles_NilBaseline(t *testing.T) {
	wp := &model.AgentProfileBody{Plugins: []model.PluginSource{{Plugin: "plugins/foo"}}}
	merged := model.MergeProfiles(nil, wp)
	if len(merged.Plugins) != 1 {
		t.Errorf("expected 1 plugin from workflow layer, got %d", len(merged.Plugins))
	}
}

func TestMergeProfiles_BothNil(t *testing.T) {
	merged := model.MergeProfiles(nil, nil)
	if len(merged.Plugins) != 0 || len(merged.MCPs) != 0 {
		t.Errorf("expected empty merged profile, got: %+v", merged)
	}
}
