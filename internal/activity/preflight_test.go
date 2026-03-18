package activity_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// capturingSandbox implements sandbox.Client and captures the script passed to Exec.
type capturingSandbox struct {
	captured string
}

func (s *capturingSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-test", nil
}
func (s *capturingSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
	s.captured = cmd
	return "", "", nil
}
func (s *capturingSandbox) ExecStream(_ context.Context, _, _, _ string, _ func(string)) error {
	return nil
}
func (s *capturingSandbox) WriteFile(_ context.Context, _, _, _ string) error         { return nil }
func (s *capturingSandbox) WriteBytes(_ context.Context, _, _ string, _ []byte) error { return nil }
func (s *capturingSandbox) ReadFile(_ context.Context, _, _ string) (string, error)   { return "", nil }
func (s *capturingSandbox) ReadBytes(_ context.Context, _, _ string) ([]byte, error)  { return nil, nil }
func (s *capturingSandbox) Kill(_ context.Context, _ string) error                    { return nil }
func (s *capturingSandbox) RenewExpiration(_ context.Context, _ string) error         { return nil }

// stubCredStore resolves credentials from an in-memory map.
type stubCredStore struct {
	creds map[string]string
}

func (s *stubCredStore) Get(_ context.Context, _, name string) (string, error) {
	if v, ok := s.creds[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("credential %q not found", name)
}

func (s *stubCredStore) GetBatch(_ context.Context, _ string, names []string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	for _, name := range names {
		v, ok := s.creds[name]
		if !ok {
			return nil, fmt.Errorf("credential %q not found", name)
		}
		result[name] = v
	}
	return result, nil
}

func TestBuildPreflightScript_EmptyProfile(t *testing.T) {
	script := activity.BuildPreflightScript(model.AgentProfileBody{}, "", "", nil)
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
	script := activity.BuildPreflightScript(profile, "https://github.com/miroapp-dev/claude-marketplace.git", "ghp_test_token", nil)
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
	script := activity.BuildPreflightScript(profile, "https://github.com/org/marketplace.git", "", nil)
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
	script := activity.BuildPreflightScript(profile, "", "", nil)
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
	script := activity.BuildPreflightScript(profile, "", "", nil)
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
	script := activity.BuildPreflightScript(profile, "", "", nil)
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

func TestParseGitHubTreeURL_RejectsNonGitHubHost(t *testing.T) {
	_, _, err := activity.ParseGitHubTreeURL("https://github.example.com/org/repo/tree/main/plugins/foo")
	if err == nil {
		t.Fatal("expected error for non-github.com host")
	}
}

func TestBuildPreflightScript_MCPCredentialsExported(t *testing.T) {
	profile := model.AgentProfileBody{
		MCPs: []model.MCPConfig{{
			Name:        "auth-mcp",
			Transport:   "sse",
			URL:         "https://mcp.example.com/sse",
			Headers:     []model.Header{{Name: "Authorization", Value: "Bearer ${MY_TOKEN}"}},
			Credentials: []string{"MY_TOKEN"},
		}},
	}
	resolvedCreds := map[string]string{"MY_TOKEN": "secret-value"}
	script := activity.BuildPreflightScript(profile, "", "", resolvedCreds)
	if !strings.Contains(script, "export MY_TOKEN=") {
		t.Errorf("expected export MY_TOKEN in script, got:\n%s", script)
	}
	if !strings.Contains(script, "secret-value") {
		t.Errorf("expected resolved credential value in script, got:\n%s", script)
	}
}

func TestRunPreflight_ResolvesAndInjectsMCPCredentials(t *testing.T) {
	sb := &capturingSandbox{}
	acts := &activity.Activities{
		Sandbox:   sb,
		CredStore: &stubCredStore{creds: map[string]string{"MY_TOKEN": "resolved-secret"}},
	}
	_, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID: "s1",
		TeamID:    "team-1",
		Profile: model.AgentProfileBody{
			MCPs: []model.MCPConfig{{
				Name:        "auth-mcp",
				Transport:   "sse",
				URL:         "https://mcp.example.com/sse",
				Credentials: []string{"MY_TOKEN"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.captured, "export MY_TOKEN=") {
		t.Errorf("expected MCP credential exported in script, got:\n%s", sb.captured)
	}
}
