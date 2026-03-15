package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	flcrypto "github.com/tinkerloft/fleetlift/internal/crypto"
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
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("missing line: %s", want)
		}
	}
	if strings.Contains(content, "GITHUB_TOKEN") {
		t.Error("GITHUB_TOKEN must not appear in local.env output")
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

func TestParseLocalEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.env")
	content := "# comment\nexport ANTHROPIC_API_KEY=\"sk-ant-test\"\nexport JWT_SECRET=\"abc\"\nexport GITHUB_TOKEN=\"\"\nexport DEV_NO_AUTH=1\n"
	os.WriteFile(path, []byte(content), 0o600) //nolint:errcheck

	got := parseLocalEnv(path)

	if got["ANTHROPIC_API_KEY"] != "sk-ant-test" {
		t.Errorf("ANTHROPIC_API_KEY: got %q", got["ANTHROPIC_API_KEY"])
	}
	if got["JWT_SECRET"] != "abc" {
		t.Errorf("JWT_SECRET: got %q", got["JWT_SECRET"])
	}
	if v, ok := got["GITHUB_TOKEN"]; !ok || v != "" {
		t.Errorf("GITHUB_TOKEN: got %q, ok=%v", v, ok)
	}
	if _, ok := got["comment"]; ok {
		t.Error("comment should not be parsed as a key")
	}
	if got["DEV_NO_AUTH"] != "1" {
		t.Errorf("DEV_NO_AUTH: got %q", got["DEV_NO_AUTH"])
	}
}

func TestParseLocalEnv_Missing(t *testing.T) {
	got := parseLocalEnv("/nonexistent/path/local.env")
	if got != nil {
		t.Error("expected nil for missing file")
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

func TestSeedGitHubToken_InsertsEncryptedCredential(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	encKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() }) // registered first → runs last (LIFO)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck
	})
	db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck

	if err := seedGitHubToken(dbURL, encKey, "ghp_testtoken123"); err != nil {
		t.Fatalf("seedGitHubToken: %v", err)
	}

	var valueEnc []byte
	if err := db.QueryRow(
		`SELECT value_enc FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`,
		devTeamID,
	).Scan(&valueEnc); err != nil {
		t.Fatalf("credential not found after seed: %v", err)
	}

	got, err := flcrypto.DecryptAESGCM(encKey, valueEnc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "ghp_testtoken123" {
		t.Errorf("decrypted value = %q, want %q", got, "ghp_testtoken123")
	}
}

func TestSeedGitHubToken_Idempotent(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	encKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() }) // registered first → runs last (LIFO)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck
	})
	db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck

	if err := seedGitHubToken(dbURL, encKey, "ghp_first"); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := seedGitHubToken(dbURL, encKey, "ghp_second"); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	// Verify exactly one row
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`,
		devTeamID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}

	var valueEnc []byte
	if err := db.QueryRow(
		`SELECT value_enc FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`,
		devTeamID,
	).Scan(&valueEnc); err != nil {
		t.Fatalf("query: %v", err)
	}
	got, err := flcrypto.DecryptAESGCM(encKey, valueEnc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "ghp_second" {
		t.Errorf("got %q, want %q", got, "ghp_second")
	}
}

func TestCheckExistingGitHubToken_NotFound(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() }) // registered first → runs last (LIFO)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck
	})
	db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck

	found, err := checkExistingGitHubToken(dbURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false, got true")
	}
}

func TestCheckExistingGitHubToken_Found(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	encKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() }) // registered first → runs last (LIFO)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck
	})
	db.Exec(`DELETE FROM credentials WHERE team_id = $1 AND name = 'GITHUB_TOKEN'`, devTeamID) //nolint:errcheck

	if err := seedGitHubToken(dbURL, encKey, "ghp_existing"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	found, err := checkExistingGitHubToken(dbURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Error("expected found=true, got false")
	}
}

func TestCheckExistingGitHubToken_DBError_TreatedAsNotFound(t *testing.T) {
	// sql.Open is lazy — the dial happens on QueryRow. connect_timeout=1 is
	// required to keep this test fast (caps the TCP dial wait to 1 second).
	found, err := checkExistingGitHubToken("postgres://invalid:invalid@localhost:9999/nodb?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("expected nil error (DB error treated as not-found), got: %v", err)
	}
	if found {
		t.Error("expected found=false on DB error")
	}
}
