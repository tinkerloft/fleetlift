# init-local Wizard Implementation Plan

> **Completed:** 2026-03-15

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add `fleetlift init-local` command + `make init-local` target that walks a new developer through local environment setup.

**Architecture:** A single new file `cmd/cli/init_local.go` contains the cobra command, `huh` wizard form, and all helper functions (env file writer, script patcher, DB seeder). Testable helpers are unit-tested in `cmd/cli/init_local_test.go`. The command reuses the existing `saveToken` function from `client.go`.

**Tech Stack:** Go, `github.com/charmbracelet/huh v0.6+` (TUI prompts), `database/sql` + `github.com/lib/pq` (DB seeding), `docker` CLI (via `os/exec`).

**Spec:** `docs/superpowers/specs/2026-03-15-init-local-wizard-design.md`

---

## Chunk 1: Dependency + Helper Functions

### Task 1: Add `huh` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [x] **Step 1: Add huh**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/bootstrap_command
go get github.com/charmbracelet/huh@latest
go mod tidy
```

- [x] **Step 2: Verify build still compiles**

```bash
go build ./...
```

Expected: no errors.

- [x] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add charmbracelet/huh for interactive CLI prompts"
```

---

### Task 2: Helper functions — env writing and script patching

**Files:**
- Create: `cmd/cli/init_local.go`
- Create: `cmd/cli/init_local_test.go`

These helpers are pure functions over the filesystem — easy to unit test without any external dependencies.

- [x] **Step 1: Write failing tests for `generateHexSecret`, `writeLocalEnv`, `patchDevEnv`**

Create `cmd/cli/init_local_test.go`:

```go
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
	// Two calls must differ
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
	// CLAUDE_CODE_OAUTH_TOKEN not set — should not appear
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
	os.WriteFile(script, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o755)

	patchDevEnv(script) //nolint:errcheck
	patchDevEnv(script) //nolint:errcheck

	data, _ := os.ReadFile(script)
	count := strings.Count(string(data), "local.env")
	if count != 1 {
		t.Errorf("source line should appear exactly once, got %d", count)
	}
}
```

- [x] **Step 2: Run tests — confirm they fail (functions not defined)**

```bash
go test ./cmd/cli/... -run "TestGenerateHexSecret|TestWriteLocalEnv|TestPatchDevEnv" -v
```

Expected: compile error — `generateHexSecret`, `writeLocalEnv`, `patchDevEnv`, `localEnvConfig`, `devUserID`, `devTeamID` undefined.

- [x] **Step 3: Create `cmd/cli/init_local.go` with the struct, constants, and helper functions**

```go
package main

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

const (
	devUserID = "00000000-0000-0000-0000-000000000001"
	devTeamID = "00000000-0000-0000-0000-000000000002"

	localEnvPath = ".fleetlift/local.env"
	authJSONPath = ".fleetlift/auth.json"

	sourceLineMarker = "[ -f ~/.fleetlift/local.env ] && source ~/.fleetlift/local.env"
)

type localEnvConfig struct {
	DatabaseURL             string
	TemporalAddress         string
	TemporalUIURL           string
	OpenSandboxDomain       string
	OpenSandboxAPIKey       string
	AgentImage              string
	GitUserEmail            string
	GitUserName             string
	JWTSecret               string
	CredentialEncryptionKey string
	DevNoAuth               bool
	DevUserID               string
	DevTeamID               string
	// Exactly one of these is set:
	AnthropicAPIKey  string
	ClaudeOAuthToken string
	// Optional:
	GitHubToken string
}

func generateHexSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func writeLocalEnv(path string, cfg localEnvConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("# Fleetlift local dev environment — generated by 'fleetlift init-local'\n")
	sb.WriteString("# Source this file or let scripts/integration/dev-env.sh load it automatically.\n\n")

	line := func(k, v string) {
		fmt.Fprintf(&sb, "export %s=%q\n", k, v)
	}

	line("DATABASE_URL", cfg.DatabaseURL)
	line("TEMPORAL_ADDRESS", cfg.TemporalAddress)
	line("TEMPORAL_UI_URL", cfg.TemporalUIURL)
	line("OPENSANDBOX_DOMAIN", cfg.OpenSandboxDomain)
	line("OPENSANDBOX_API_KEY", cfg.OpenSandboxAPIKey)
	line("AGENT_IMAGE", cfg.AgentImage)
	line("GIT_USER_EMAIL", cfg.GitUserEmail)
	line("GIT_USER_NAME", cfg.GitUserName)
	sb.WriteString("\n# Auth\n")
	line("JWT_SECRET", cfg.JWTSecret)
	line("CREDENTIAL_ENCRYPTION_KEY", cfg.CredentialEncryptionKey)
	sb.WriteString("\n# Dev auth bypass\n")
	if cfg.DevNoAuth {
		sb.WriteString("export DEV_NO_AUTH=1\n")
	}
	line("DEV_USER_ID", cfg.DevUserID)
	line("DEV_TEAM_ID", cfg.DevTeamID)
	sb.WriteString("\n# Claude agent auth\n")
	if cfg.AnthropicAPIKey != "" {
		line("ANTHROPIC_API_KEY", cfg.AnthropicAPIKey)
	} else if cfg.ClaudeOAuthToken != "" {
		line("CLAUDE_CODE_OAUTH_TOKEN", cfg.ClaudeOAuthToken)
	}
	if cfg.GitHubToken != "" {
		sb.WriteString("\n# GitHub\n")
		line("GITHUB_TOKEN", cfg.GitHubToken)
	}

	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

func patchDevEnv(scriptPath string) error {
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return err
	}
	content := string(data)

	// Idempotency check
	if strings.Contains(content, sourceLineMarker) {
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) == 0 {
		return nil
	}

	// Insert after shebang (line 0)
	patched := make([]string, 0, len(lines)+1)
	patched = append(patched, lines[0])
	patched = append(patched, sourceLineMarker)
	patched = append(patched, lines[1:]...)

	return os.WriteFile(scriptPath, []byte(strings.Join(patched, "\n")+"\n"), 0o755)
}

func seedDevIdentity(dbURL string) error {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	// Wait for Postgres to be ready
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := db.Ping(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("postgres not ready after 30s — run 'docker compose up -d' and try again")
		}
		time.Sleep(1 * time.Second)
		fmt.Print(".")
	}
	fmt.Println()

	_, err = db.Exec(`INSERT INTO teams (id, name, slug) VALUES ($1, 'dev-team', 'dev-team') ON CONFLICT (slug) DO NOTHING`, devTeamID)
	if err != nil {
		return fmt.Errorf("upsert team: %w", err)
	}

	_, err = db.Exec(`INSERT INTO users (id, name, provider, provider_id) VALUES ($1, 'Dev User', 'dev', $1) ON CONFLICT (provider, provider_id) DO NOTHING`, devUserID)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}

	_, err = db.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT (team_id, user_id) DO NOTHING`, devTeamID, devUserID)
	if err != nil {
		return fmt.Errorf("upsert team_members: %w", err)
	}

	return nil
}

func initLocalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init-local",
		Short: "Interactive wizard to set up a local development environment",
		RunE:  runInitLocal,
	}
}

func runInitLocal(cmd *cobra.Command, args []string) error {
	// Implemented in Task 4
	return fmt.Errorf("not yet implemented")
}
```

- [x] **Step 4: Run tests — confirm they pass**

```bash
go test ./cmd/cli/... -run "TestGenerateHexSecret|TestWriteLocalEnv|TestPatchDevEnv" -v
```

Expected: all PASS.

- [x] **Step 5: Commit**

```bash
git add cmd/cli/init_local.go cmd/cli/init_local_test.go
git commit -m "feat: add init-local helper functions (env writing, script patching, DB seeding)"
```

---

### Task 3: DB seeding integration test

**Files:**
- Modify: `cmd/cli/init_local_test.go`

The `seedDevIdentity` function touches a real database. Write an integration test gated behind a build tag that only runs when `DATABASE_URL` is set.

- [x] **Step 1: Add integration test to `init_local_test.go`**

Append to the file:

```go
func TestSeedDevIdentity(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	if err := seedDevIdentity(dbURL); err != nil {
		t.Fatalf("seedDevIdentity: %v", err)
	}

	// Re-run must be idempotent
	if err := seedDevIdentity(dbURL); err != nil {
		t.Fatalf("seedDevIdentity idempotency: %v", err)
	}
}
```

- [x] **Step 2: Run without DATABASE_URL — confirm skip**

```bash
go test ./cmd/cli/... -run TestSeedDevIdentity -v
```

Expected: `SKIP — DATABASE_URL not set`.

- [x] **Step 3: Run with DATABASE_URL (requires local Postgres running)**

```bash
DATABASE_URL="postgres://fleetlift:fleetlift@localhost:5432/fleetlift?sslmode=disable" \
  go test ./cmd/cli/... -run TestSeedDevIdentity -v
```

Expected: PASS (or SKIP if DB not running — acceptable in CI).

- [x] **Step 4: Commit**

```bash
git add cmd/cli/init_local_test.go
git commit -m "test: add integration test for seedDevIdentity"
```

---

## Chunk 2: Wizard Command + Makefile

### Task 4: Implement the wizard form

**Files:**
- Modify: `cmd/cli/init_local.go` — replace `runInitLocal` stub with full implementation
- Modify: `cmd/cli/main.go` — register `initLocalCmd()`

The wizard uses `huh.NewForm` for interactive prompts. The `huh` library handles masked input for secrets natively.

- [x] **Step 1: Replace `runInitLocal` stub with full wizard**

Replace the `runInitLocal` function in `cmd/cli/init_local.go`:

```go
func runInitLocal(_ *cobra.Command, _ []string) error {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║   Fleetlift — Local Setup Wizard     ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Preflight — Docker running?
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("Docker is not running. Start Docker Desktop and try again")
	}

	// Step 2 + 3: Collect secrets from user
	var (
		claudeAuthMethod string // "api_key" or "oauth_token"
		anthropicKey     string
		oauthToken       string
		githubToken      string
	)

	authForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Claude authentication method").
				Options(
					huh.NewOption("Anthropic API key (ANTHROPIC_API_KEY)", "api_key"),
					huh.NewOption("Claude OAuth token (CLAUDE_CODE_OAUTH_TOKEN)", "oauth_token"),
				).
				Value(&claudeAuthMethod),
		),
	)
	if err := authForm.Run(); err != nil {
		return err
	}

	var secretForm *huh.Form
	if claudeAuthMethod == "api_key" {
		secretForm = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Anthropic API key").
					Description("Starts with sk-ant-api").
					EchoMode(huh.EchoModePassword).
					Value(&anthropicKey).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("required")
						}
						return nil
					}),
				huh.NewInput().
					Title("GitHub personal access token (optional)").
					Description("Required for workflows that interact with GitHub repos. Press Enter to skip.").
					EchoMode(huh.EchoModePassword).
					Value(&githubToken),
			),
		)
	} else {
		secretForm = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Claude OAuth token").
					Description("Starts with sk-ant-oat01-").
					EchoMode(huh.EchoModePassword).
					Value(&oauthToken).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("required")
						}
						return nil
					}),
				huh.NewInput().
					Title("GitHub personal access token (optional)").
					Description("Required for workflows that interact with GitHub repos. Press Enter to skip.").
					EchoMode(huh.EchoModePassword).
					Value(&githubToken),
			),
		)
	}
	if err := secretForm.Run(); err != nil {
		return err
	}

	// Step 4: Generate secrets
	cfg := localEnvConfig{
		DatabaseURL:             "postgres://fleetlift:fleetlift@localhost:5432/fleetlift?sslmode=disable",
		TemporalAddress:         "localhost:7233",
		TemporalUIURL:           "http://localhost:8233",
		OpenSandboxDomain:       "http://localhost:8090",
		OpenSandboxAPIKey:       "",
		AgentImage:              "claude-code-sandbox:latest",
		GitUserEmail:            "claude-agent@noreply.localhost",
		GitUserName:             "Claude Code Agent",
		JWTSecret:               generateHexSecret(32),
		CredentialEncryptionKey: generateHexSecret(32),
		DevNoAuth:               true,
		DevUserID:               devUserID,
		DevTeamID:               devTeamID,
		AnthropicAPIKey:         strings.TrimSpace(anthropicKey),
		ClaudeOAuthToken:        strings.TrimSpace(oauthToken),
		GitHubToken:             strings.TrimSpace(githubToken),
	}

	// Step 5: Write ~/.fleetlift/local.env
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	envPath := home + "/" + localEnvPath

	if _, err := os.Stat(envPath); err == nil {
		var overwrite bool
		overwriteForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(envPath + " already exists. Overwrite?").
					Value(&overwrite),
			),
		)
		if err := overwriteForm.Run(); err != nil {
			return err
		}
		if !overwrite {
			fmt.Println("Skipping env file write. Using existing local.env.")
			// Re-read existing env for cfg.DatabaseURL used by seedDevIdentity
		} else {
			if err := writeLocalEnv(envPath, cfg); err != nil {
				return fmt.Errorf("write local.env: %w", err)
			}
			fmt.Println("✓ Written:", envPath)
		}
	} else {
		if err := writeLocalEnv(envPath, cfg); err != nil {
			return fmt.Errorf("write local.env: %w", err)
		}
		fmt.Println("✓ Written:", envPath)
	}

	// Step 6: Patch dev-env.sh
	devEnvScript := "scripts/integration/dev-env.sh"
	if err := patchDevEnv(devEnvScript); err != nil {
		fmt.Printf("Warning: could not patch %s: %v\n", devEnvScript, err)
		fmt.Printf("Manually add this line after the shebang in %s:\n  %s\n", devEnvScript, sourceLineMarker)
	} else {
		fmt.Println("✓ Patched:", devEnvScript)
	}

	// Step 7: Start Docker stacks
	var startDocker bool
	dockerForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Start Docker Compose stacks now? (Temporal + Postgres + OpenSandbox)").
				Value(&startDocker),
		),
	)
	if err := dockerForm.Run(); err != nil {
		return err
	}

	proceedWithSeed := startDocker
	if startDocker {
		fmt.Println("Starting Temporal + Postgres...")
		run("docker", "compose", "up", "-d")
		fmt.Println("Starting OpenSandbox...")
		run("docker", "compose", "-f", "docker-compose.opensandbox.yaml", "up", "-d")
	} else {
		var alreadyUp bool
		alreadyUpForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Are the Docker stacks already running? Proceed with database setup?").
					Value(&alreadyUp),
			),
		)
		if err := alreadyUpForm.Run(); err != nil {
			return err
		}
		proceedWithSeed = alreadyUp
		if !proceedWithSeed {
			printManualSteps()
			return nil
		}
	}

	// Step 8: Seed dev identity
	fmt.Print("Waiting for Postgres")
	if err := seedDevIdentity(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("seed dev identity: %w", err)
	}
	fmt.Println("✓ Dev identity seeded (team + user + membership)")

	// Write ~/.fleetlift/auth.json
	if err := saveToken("dev-token"); err != nil {
		return fmt.Errorf("write auth.json: %w", err)
	}
	fmt.Println("✓ Written:", home+"/"+authJSONPath)

	// Step 9: Print next steps
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────┐")
	fmt.Println("│   Local environment ready!              │")
	fmt.Println("└─────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("Start the server and worker:")
	fmt.Println("  scripts/integration/start.sh")
	fmt.Println()
	fmt.Println("Tail logs:")
	fmt.Println("  scripts/integration/logs.sh")
	fmt.Println()
	fmt.Println("Temporal UI: http://localhost:8233")
	fmt.Println("Fleetlift:   http://localhost:8080")
	return nil
}

func run(name string, args ...string) {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Printf("warning: %s %v: %v\n", name, args, err)
	}
}

func printManualSteps() {
	fmt.Println()
	fmt.Println("Start the stacks manually, then re-run 'fleetlift init-local':")
	fmt.Println()
	fmt.Println("  docker compose up -d")
	fmt.Println("  docker compose -f docker-compose.opensandbox.yaml up -d")
}
```

Add the `huh` import to the import block at the top of `init_local.go`:

```go
import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)
```

- [x] **Step 2: Register `initLocalCmd()` in `main.go`**

In `cmd/cli/main.go`, add to `root.AddCommand(...)`:

```go
root.AddCommand(
    authCmd(),
    workflowCmd(),
    runCmd(),
    inboxCmd(),
    credentialCmd(),
    knowledgeCmd(),
    initLocalCmd(),  // add this line
)
```

- [x] **Step 3: Build and verify it compiles**

```bash
go build ./cmd/cli/...
```

Expected: no errors.

- [x] **Step 4: Run linter**

```bash
make lint
```

Fix any issues before proceeding.

- [x] **Step 5: Run unit tests (helpers still pass)**

```bash
go test ./cmd/cli/... -run "TestGenerateHexSecret|TestWriteLocalEnv|TestPatchDevEnv" -v
```

Expected: all PASS.

- [x] **Step 6: Commit**

```bash
git add cmd/cli/init_local.go cmd/cli/main.go
git commit -m "feat: implement fleetlift init-local wizard command"
```

---

### Task 5: Makefile target

**Files:**
- Modify: `Makefile`

- [x] **Step 1: Add `init-local` to `.PHONY` and add the target**

In `Makefile`, find the `.PHONY` line (line 1) and add `init-local` to it:

```makefile
.PHONY: build test clean fleetlift-worker fleetlift all temporal-dev temporal-up temporal-down temporal-logs sandbox-build agent-image kind-setup test-integration-k8s build-web dev-web opensandbox-up opensandbox-down opensandbox-logs init-local
```

Then append the target (use a real tab character before the recipe, not spaces):

```makefile
# Run local setup wizard (builds agent image first, then CLI, then runs wizard)
init-local: sandbox-build
	go build -o ./fleetlift ./cmd/cli && ./fleetlift init-local; rm -f ./fleetlift
```

- [x] **Step 2: Verify target parses correctly**

```bash
make -n init-local
```

Expected: prints the commands without running them, no make errors.

- [x] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: add make init-local target"
```

---

## Chunk 3: Smoke Test + Final Checks

### Task 6: End-to-end smoke test

- [x] **Step 1: Run full test suite**

```bash
go test ./...
```

Expected: all PASS (integration tests skip without DATABASE_URL).

- [x] **Step 2: Run linter**

```bash
make lint
```

Expected: no errors.

- [x] **Step 3: Build all binaries**

```bash
go build ./...
```

Expected: no errors.

- [x] **Step 4: Manual smoke test of `--help`**

```bash
go run ./cmd/cli init-local --help
```

Expected: prints usage for `init-local`.

- [x] **Step 5: Update implementation plan — mark complete**

Add `[x]` to all tasks above, and note completion date at top of this document.
