package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateHexSecret(t *testing.T) {
	s := generateHexSecret(32)
	if len(s) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(s))
	}
	s2 := generateHexSecret(32)
	if s == s2 {
		t.Fatal("expected different secrets on each call")
	}
}

func TestWriteLocalEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.env")

	cfg := localEnvConfig{
		DatabaseURL:             "postgres://x",
		TemporalAddress:         "localhost:7233",
		TemporalUIURL:           "http://localhost:8233",
		OpenSandboxDomain:       "http://localhost:8090",
		OpenSandboxAPIKey:       "",
		AgentImage:              "claude-code-sandbox:latest",
		GitUserEmail:            "claude-agent@noreply.localhost",
		GitUserName:             "Claude Code Agent",
		JWTSecret:               "abc123",
		CredentialEncryptionKey: "def456",
		DevNoAuth:               true,
		DevUserID:               devUserID,
		DevTeamID:               devTeamID,
		AnthropicAPIKey:         "sk-ant-test",
		ClaudeOAuthToken:        "",
		GitHubToken:             "ghp_test",
	}

	if err := writeLocalEnv(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	checks := []string{
		`export DATABASE_URL="postgres://x"`,
		`export JWT_SECRET="abc123"`,
		`export DEV_NO_AUTH=1`,
		`export DEV_USER_ID="` + devUserID + `"`,
		`export ANTHROPIC_API_KEY="sk-ant-test"`,
		`export GITHUB_TOKEN="ghp_test"`,
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("missing line: %s", want)
		}
	}
	if strings.Contains(content, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Error("unexpected CLAUDE_CODE_OAUTH_TOKEN in output")
	}
}

func TestWriteLocalEnv_OAuthToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.env")

	cfg := localEnvConfig{
		DatabaseURL:             "postgres://x",
		TemporalAddress:         "localhost:7233",
		TemporalUIURL:           "http://localhost:8233",
		OpenSandboxDomain:       "http://localhost:8090",
		AgentImage:              "claude-code-sandbox:latest",
		GitUserEmail:            "a@b.com",
		GitUserName:             "Bot",
		JWTSecret:               "s",
		CredentialEncryptionKey: "e",
		DevNoAuth:               true,
		DevUserID:               devUserID,
		DevTeamID:               devTeamID,
		ClaudeOAuthToken:        "sk-ant-oat01-token",
	}

	if err := writeLocalEnv(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, `export CLAUDE_CODE_OAUTH_TOKEN="sk-ant-oat01-token"`) {
		t.Error("missing CLAUDE_CODE_OAUTH_TOKEN")
	}
	if strings.Contains(content, "ANTHROPIC_API_KEY") {
		t.Error("unexpected ANTHROPIC_API_KEY")
	}
}

func TestPatchDevEnv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "dev-env.sh")

	original := "#!/usr/bin/env bash\nset -euo pipefail\nexport FOO=bar\n"
	if err := os.WriteFile(script, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := patchDevEnv(script); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(script)
	lines := strings.Split(string(data), "\n")

	if lines[0] != "#!/usr/bin/env bash" {
		t.Errorf("shebang must remain line 1, got: %s", lines[0])
	}
	wantSource := `[ -f ~/.fleetlift/local.env ] && source ~/.fleetlift/local.env`
	if lines[1] != wantSource {
		t.Errorf("expected source line at line 2, got: %s", lines[1])
	}
	if lines[2] != "set -euo pipefail" {
		t.Errorf("set line must follow, got: %s", lines[2])
	}
}

func TestPatchDevEnv_Idempotent(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "dev-env.sh")
	os.WriteFile(script, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o755) //nolint:errcheck

	patchDevEnv(script) //nolint:errcheck
	patchDevEnv(script) //nolint:errcheck

	data, _ := os.ReadFile(script)
	count := strings.Count(string(data), sourceLineMarker)
	if count != 1 {
		t.Errorf("source line should appear exactly once, got %d", count)
	}
}

func TestPatchDevEnv_NoShebang(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "no-shebang.sh")
	os.WriteFile(script, []byte("export FOO=bar\n"), 0o755) //nolint:errcheck

	err := patchDevEnv(script)
	if err == nil {
		t.Error("expected error for file without shebang, got nil")
	}
}

func TestWriteLocalEnv_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.env")
	cfg := localEnvConfig{
		DatabaseURL: "postgres://x", TemporalAddress: "localhost:7233",
		TemporalUIURL: "http://localhost:8233", OpenSandboxDomain: "http://localhost:8090",
		AgentImage: "img", GitUserEmail: "a@b.com", GitUserName: "Bot",
		JWTSecret: "s", CredentialEncryptionKey: "e",
		DevNoAuth: true, DevUserID: devUserID, DevTeamID: devTeamID,
		AnthropicAPIKey: "key",
	}
	if err := writeLocalEnv(path, cfg); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %o", fi.Mode().Perm())
	}
}

func TestSeedDevIdentity(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	if err := seedDevIdentity(dbURL); err != nil {
		t.Fatalf("seedDevIdentity: %v", err)
	}

	if err := seedDevIdentity(dbURL); err != nil {
		t.Fatalf("seedDevIdentity idempotency: %v", err)
	}
}
