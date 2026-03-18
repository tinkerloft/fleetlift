package activity_test

import (
	"strings"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestBuildPreflightScript_EmptyProfile(t *testing.T) {
	script := activity.BuildPreflightScript(model.AgentProfileBody{}, "", "")
	if strings.Contains(script, "claude plugin install") {
		t.Error("expected no plugin install for empty profile")
	}
	if strings.Contains(script, "claude mcp add") {
		t.Error("expected no mcp add for empty profile")
	}
}

func TestBuildPreflightScript_WithMarketplacePlugin(t *testing.T) {
	profile := model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/miro-helm-doctor"}},
	}
	script := activity.BuildPreflightScript(profile, "https://github.com/miroapp-dev/claude-marketplace.git", "ghp_test_token")
	if !strings.Contains(script, "claude plugin marketplace add") {
		t.Error("expected marketplace add command")
	}
	if !strings.Contains(script, "claude plugin install 'miro-helm-doctor'") {
		t.Error("expected plugin install for 'miro-helm-doctor'")
	}
	if !strings.Contains(script, "claude plugin uninstall 'miro-helm-doctor'") {
		t.Error("expected plugin uninstall before install")
	}
	if !strings.Contains(script, "credential.helper store") {
		t.Error("expected credential.helper store for private marketplace")
	}
	if !strings.Contains(script, ".git-credentials") {
		t.Error("expected .git-credentials write for private marketplace")
	}
}

func TestBuildPreflightScript_PublicMarketplace_NoCredentials(t *testing.T) {
	profile := model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/foo"}},
	}
	script := activity.BuildPreflightScript(profile, "https://github.com/org/marketplace.git", "")
	if strings.Contains(script, "credential.helper") {
		t.Error("expected no credential setup for public marketplace")
	}
	if !strings.Contains(script, "claude plugin marketplace add") {
		t.Error("expected marketplace add even for public marketplace")
	}
}

func TestBuildPreflightScript_GitHubURLPluginSkipped(t *testing.T) {
	profile := model.AgentProfileBody{
		Plugins: []model.PluginSource{{GitHubURL: "https://github.com/org/repo/tree/main/plugins/foo"}},
	}
	script := activity.BuildPreflightScript(profile, "", "")
	if strings.Contains(script, "claude plugin install") {
		t.Error("GitHubURL plugins must not be installed via marketplace")
	}
}

func TestBuildPreflightScript_WithMCP(t *testing.T) {
	profile := model.AgentProfileBody{
		MCPs: []model.MCPConfig{
			{Name: "my-mcp", Transport: "sse", URL: "https://mcp.example.com/sse"},
		},
	}
	script := activity.BuildPreflightScript(profile, "", "")
	if !strings.Contains(script, "claude mcp remove 'my-mcp'") {
		t.Error("expected mcp remove before add")
	}
	if !strings.Contains(script, "claude mcp add --transport 'sse' --scope user 'my-mcp'") {
		t.Error("expected mcp add command")
	}
}

func TestBuildPreflightScript_MCPWithHeader(t *testing.T) {
	profile := model.AgentProfileBody{
		MCPs: []model.MCPConfig{{
			Name:      "auth-mcp",
			Transport: "http",
			URL:       "https://mcp.example.com",
			Headers:   []model.Header{{Name: "Authorization", Value: "Bearer ${MY_TOKEN}"}},
		}},
	}
	script := activity.BuildPreflightScript(profile, "", "")
	if !strings.Contains(script, "--header") {
		t.Errorf("expected --header in script, got:\n%s", script)
	}
	if !strings.Contains(script, "Authorization: Bearer ${MY_TOKEN}") {
		t.Errorf("expected header value in script, got:\n%s", script)
	}
}

func TestBuildEvalCloneCommands_RejectsNonHTTPS(t *testing.T) {
	_, err := activity.BuildEvalCloneCommands([]string{"git://github.com/org/repo"})
	if err == nil {
		t.Fatal("expected error for non-https eval plugin URL")
	}
}

func TestBuildEvalCloneCommands_ProducesGitClone(t *testing.T) {
	results, err := activity.BuildEvalCloneCommands([]string{
		"https://github.com/org/repo/tree/main/plugins/miro-helm-doctor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Command, "git clone") {
		t.Error("expected git clone in command")
	}
	if !strings.Contains(results[0].Command, "sparse-checkout set 'plugins/miro-helm-doctor'") {
		t.Error("expected sparse-checkout targeting the plugin subpath")
	}
	if results[0].PluginDir != "/tmp/eval-plugin-0/plugins/miro-helm-doctor" {
		t.Errorf("expected plugin dir with subpath, got %q", results[0].PluginDir)
	}
}

func TestBuildEvalCloneCommands_IncludesRmRf(t *testing.T) {
	results, err := activity.BuildEvalCloneCommands([]string{
		"https://github.com/org/repo/tree/main/plugins/foo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(results[0].Command, "rm -rf") {
		t.Errorf("expected rm -rf in clone command for idempotency, got:\n%s", results[0].Command)
	}
}

func TestParseGitHubTreeURL(t *testing.T) {
	repoURL, subPath, err := activity.ParseGitHubTreeURL(
		"https://github.com/org/repo/tree/main/plugins/foo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if repoURL != "https://github.com/org/repo.git" {
		t.Errorf("unexpected repoURL: %q", repoURL)
	}
	if subPath != "plugins/foo" {
		t.Errorf("unexpected subPath: %q", subPath)
	}
}
