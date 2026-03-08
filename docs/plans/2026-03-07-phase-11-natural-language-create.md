# Phase 11: Natural Language Task Creation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `fleetlift create --describe "..."` — a one-shot command that uses Claude to generate a Task YAML from a natural language description, shows a preview, and saves to a file.

**Architecture:** Three layers: (1) embedded schema + canonical examples as system prompt context, (2) pure helpers (YAML extraction, validation, prompt building) with full test coverage, (3) the `create` cobra command that ties them together with a Claude API call and interactive confirmation. No new dependencies needed — `anthropic-sdk-go v1.25.0` and `gopkg.in/yaml.v3` already present.

**Tech Stack:** Go, `github.com/anthropics/anthropic-sdk-go v1.25.0`, `gopkg.in/yaml.v3`, `go:embed`, Cobra CLI.

**Scope (this plan):** `fleetlift create --describe "..."` with `--repo`, `--output`, `--dry-run` flags and `[Y/n/e]` confirmation. Interactive wizard (no `--describe`), GitHub repo discovery (11.3), transformation repo registry (11.4), and templates (11.5) are separate follow-on work.

---

## Task 1: Embedded schema and example assets

**Files:**
- Create: `cmd/cli/schema/task-schema.md`
- Create: `cmd/cli/schema/example-transform.yaml`
- Create: `cmd/cli/schema/example-report.yaml`
- Create: `cmd/cli/create_assets.go`
- Create: `cmd/cli/create_assets_test.go`

### Step 1: Create `cmd/cli/schema/task-schema.md`

```markdown
# Fleetlift Task YAML Reference

Generate ONLY valid YAML. No explanations, no markdown fences, no prose — just the raw YAML.

## Always Required

```yaml
version: 1
title: "Short descriptive title"
```

## Repositories

At least one repository is required (unless using `transformation` + `targets`):

```yaml
repositories:
  - url: https://github.com/org/repo.git
    branch: main          # optional, default: main
    name: repo            # optional shortname for display
    setup:                # optional: commands to run after clone
      - go mod download
```

## Execution — choose EXACTLY ONE

### Agentic (Claude Code agent makes code changes):
```yaml
execution:
  agentic:
    prompt: |
      Detailed instructions for the agent.
      Be specific: what to change, how, and why.
    verifiers:            # optional but recommended
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]
```

### Deterministic (runs a Docker image):
```yaml
execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args: ["rewrite:run", "-Drewrite.activeRecipes=..."]
    env:                  # optional
      KEY: value
    verifiers:
      - name: build
        command: ["mvn", "compile"]
```

## Common Optional Fields

```yaml
id: my-task-slug          # alphanumeric + hyphens; auto-generated if omitted
description: "..."        # longer explanation
mode: transform           # "transform" (default, creates PRs) or "report" (analysis only)
require_approval: false   # true = workflow pauses for human approval before creating PRs
timeout: 30m              # e.g. "15m", "30m", "1h", "2h" (default: "1h")
max_parallel: 5           # repos processed concurrently (default: 5)

pull_request:
  branch_prefix: "auto/feature"
  title: "PR title"
  labels: ["automated"]
```

## Report Mode — structured output schema (optional):
```yaml
execution:
  agentic:
    prompt: |
      Analyze the codebase and write findings to /workspace/REPORT.md
      Use YAML frontmatter for structured data.
    output:
      schema:
        type: object
        required: [score]
        properties:
          score:
            type: integer
            minimum: 1
            maximum: 10
```
```

### Step 2: Create `cmd/cli/schema/example-transform.yaml`

```yaml
version: 1
id: add-structured-logging
title: "Add structured logging"
description: "Replace fmt.Printf debug logs with slog structured logging"
mode: transform

repositories:
  - url: https://github.com/acme/api-service.git
    branch: main
    setup:
      - go mod download

execution:
  agentic:
    prompt: |
      Replace all fmt.Printf and fmt.Println debug/log statements with
      structured logging using the standard library slog package.

      Requirements:
      - Import "log/slog" where needed
      - Use slog.Info, slog.Warn, slog.Error with key-value pairs
      - Preserve log message intent
      - Do not change business logic
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 30m
require_approval: true

pull_request:
  branch_prefix: "auto/structured-logging"
  title: "Add structured logging via slog"
  labels: ["automated", "observability"]
```

### Step 3: Create `cmd/cli/schema/example-report.yaml`

```yaml
version: 1
id: dependency-audit
title: "Dependency audit"
description: "Audit direct dependencies for outdated versions and CVEs"
mode: report

repositories:
  - url: https://github.com/acme/api-service.git
    branch: main

execution:
  agentic:
    prompt: |
      Audit the direct dependencies in this repository.

      Write your findings to /workspace/REPORT.md with YAML frontmatter:

      ---
      total_dependencies: <count>
      outdated_count: <count>
      cve_count: <count>
      risk: low|medium|high
      ---

      # Dependency Audit

      List each outdated or vulnerable dependency with current version,
      latest version, and CVE IDs if applicable.
    output:
      schema:
        type: object
        required: [total_dependencies, risk]
        properties:
          total_dependencies:
            type: integer
          outdated_count:
            type: integer
          cve_count:
            type: integer
          risk:
            type: string
            enum: [low, medium, high]

timeout: 15m
```

### Step 4: Create `cmd/cli/create_assets.go`

```go
package main

import _ "embed"

//go:embed schema/task-schema.md
var taskSchema string

//go:embed schema/example-transform.yaml
var exampleTransform string

//go:embed schema/example-report.yaml
var exampleReport string
```

### Step 5: Write the test

Create `cmd/cli/create_assets_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestEmbeddedAssets_NonEmpty(t *testing.T) {
	if len(taskSchema) == 0 {
		t.Error("taskSchema is empty")
	}
	if len(exampleTransform) == 0 {
		t.Error("exampleTransform is empty")
	}
	if len(exampleReport) == 0 {
		t.Error("exampleReport is empty")
	}
}

func TestEmbeddedAssets_ContainExpectedContent(t *testing.T) {
	if !strings.Contains(taskSchema, "version: 1") {
		t.Error("taskSchema should contain 'version: 1'")
	}
	if !strings.Contains(exampleTransform, "mode: transform") {
		t.Error("exampleTransform should contain 'mode: transform'")
	}
	if !strings.Contains(exampleReport, "mode: report") {
		t.Error("exampleReport should contain 'mode: report'")
	}
}
```

### Step 6: Run tests

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go test ./cmd/cli/... -run TestEmbeddedAssets -v
```

Expected: both tests PASS.

### Step 7: Commit

```bash
git add cmd/cli/schema/ cmd/cli/create_assets.go cmd/cli/create_assets_test.go
git commit -m "feat(cli): embed task schema and examples for create command"
```

---

## Task 2: YAML helpers — extract, validate, buildSystemPrompt

**Files:**
- Create: `cmd/cli/create.go` (helpers only — no command yet)
- Create: `cmd/cli/create_test.go`

### Step 1: Write failing tests first

Create `cmd/cli/create_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractYAML_PlainYAML(t *testing.T) {
	input := "version: 1\ntitle: \"Test\"\n"
	result := extractYAML(input)
	assert.Equal(t, input, result)
}

func TestExtractYAML_StripsMarkdownFence(t *testing.T) {
	input := "Here is the task:\n\n```yaml\nversion: 1\ntitle: \"Test\"\n```\n\nDone."
	result := extractYAML(input)
	assert.Equal(t, "version: 1\ntitle: \"Test\"\n", result)
}

func TestExtractYAML_StripsPlainFence(t *testing.T) {
	input := "```\nversion: 1\ntitle: \"Test\"\n```"
	result := extractYAML(input)
	assert.Equal(t, "version: 1\ntitle: \"Test\"\n", result)
}

func TestExtractYAML_EmptyInput(t *testing.T) {
	result := extractYAML("")
	assert.Equal(t, "", result)
}

func TestValidateTaskYAML_ValidTransform(t *testing.T) {
	yaml := `version: 1
title: "Test Task"
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: "Do the thing"
`
	task, err := validateTaskYAML(yaml)
	require.NoError(t, err)
	assert.Equal(t, "Test Task", task.Title)
	assert.Len(t, task.Repositories, 1)
}

func TestValidateTaskYAML_MissingTitle(t *testing.T) {
	yaml := `version: 1
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: "Do the thing"
`
	_, err := validateTaskYAML(yaml)
	assert.ErrorContains(t, err, "title")
}

func TestValidateTaskYAML_MissingExecution(t *testing.T) {
	yaml := `version: 1
title: "Test"
repositories:
  - url: https://github.com/org/repo.git
`
	_, err := validateTaskYAML(yaml)
	assert.ErrorContains(t, err, "execution")
}

func TestValidateTaskYAML_MissingRepositories(t *testing.T) {
	yaml := `version: 1
title: "Test"
execution:
  agentic:
    prompt: "Do the thing"
`
	_, err := validateTaskYAML(yaml)
	assert.ErrorContains(t, err, "repositor")
}

func TestBuildSystemPrompt_ContainsSchema(t *testing.T) {
	prompt := buildSystemPrompt()
	assert.True(t, strings.Contains(prompt, "version: 1"))
	assert.True(t, strings.Contains(prompt, "example-transform") || strings.Contains(prompt, "mode: transform"))
}
```

### Step 2: Run tests to verify they fail

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go test ./cmd/cli/... -run "TestExtractYAML|TestValidateTaskYAML|TestBuildSystemPrompt" -v 2>&1 | head -20
```

Expected: FAIL — functions not yet defined.

### Step 3: Implement helpers in `cmd/cli/create.go`

Create `cmd/cli/create.go`:

```go
package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// extractYAML strips markdown code fences from a Claude response, returning raw YAML.
// If no fence is found, returns the input trimmed.
func extractYAML(response string) string {
	// Try ```yaml ... ``` first
	if idx := strings.Index(response, "```yaml"); idx != -1 {
		start := idx + len("```yaml")
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimLeft(response[start:start+end], "\n")
		}
	}
	// Try plain ``` ... ```
	if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimLeft(response[start:start+end], "\n")
		}
	}
	return response
}

// validateTaskYAML parses yamlStr into a model.Task and checks required fields.
func validateTaskYAML(yamlStr string) (model.Task, error) {
	var task model.Task
	if err := yaml.Unmarshal([]byte(yamlStr), &task); err != nil {
		return model.Task{}, fmt.Errorf("invalid YAML: %w", err)
	}
	if task.Title == "" {
		return model.Task{}, fmt.Errorf("task is missing required field: title")
	}
	if task.Execution.Agentic == nil && task.Execution.Deterministic == nil {
		return model.Task{}, fmt.Errorf("task is missing required field: execution (must have agentic or deterministic)")
	}
	hasRepos := len(task.Repositories) > 0 || len(task.Targets) > 0 ||
		(task.Transformation != nil && len(task.Targets) > 0)
	if !hasRepos {
		return model.Task{}, fmt.Errorf("task is missing required field: repositories (at least one repo)")
	}
	return task, nil
}

// buildSystemPrompt constructs the system prompt for Claude from embedded schema + examples.
func buildSystemPrompt() string {
	return strings.Join([]string{
		"You are an expert at writing Fleetlift task YAML files.",
		"Generate ONLY valid YAML — no markdown fences, no explanations, no prose.",
		"Use the schema and examples below as your reference.",
		"",
		"# Task YAML Schema",
		taskSchema,
		"",
		"# Example: Transform Task",
		exampleTransform,
		"",
		"# Example: Report Task",
		exampleReport,
	}, "\n")
}
```

> **Implementation notes:**
> - `model.Execution` has `Agentic *model.AgenticExecution` and `Deterministic *model.DeterministicExecution` — both pointer fields, so `== nil` check works.
> - Read `internal/model/task.go` around line 254 to confirm field names before writing.
> - If `model.Execution` uses different field names (e.g. `Agentic` vs `AgenticExecution`), adjust accordingly.

### Step 4: Run tests — verify they pass

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go test ./cmd/cli/... -run "TestExtractYAML|TestValidateTaskYAML|TestBuildSystemPrompt" -v
```

Expected: all 9 tests PASS.

### Step 5: Run all CLI tests

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go test ./cmd/cli/... -v 2>&1 | tail -15
```

### Step 6: Commit

```bash
git add cmd/cli/create.go cmd/cli/create_test.go
git commit -m "feat(cli): add YAML extraction, validation, and system prompt helpers for create command"
```

---

## Task 3: `fleetlift create` command

**Files:**
- Modify: `cmd/cli/create.go` (add command, Claude call, editor support)
- Modify: `cmd/cli/main.go` (register `createCmd`)

### Step 1: Read `cmd/cli/main.go`

Read the file to see:
1. How other commands are registered (look for `rootCmd.AddCommand(...)` calls in `init()` or `main()`)
2. The import block — confirm how packages are imported

### Step 2: Implement the full `create.go`

Add the following to `cmd/cli/create.go` (append after the existing helpers):

```go
import (
	// (add to existing imports)
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/model"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Generate a task YAML using AI from a natural language description",
	Long: `Generate a Fleetlift task YAML file using Claude.

Examples:
  # One-shot from description:
  fleetlift create --describe "Add OpenTelemetry tracing to all Go services" \
    --repo https://github.com/acme/api.git \
    --output tracing-task.yaml

  # Preview without saving:
  fleetlift create --describe "Security audit of auth module" --dry-run`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().String("describe", "", "Natural language description of the task (required)")
	createCmd.Flags().StringArray("repo", nil, "Repository URL to include (repeatable)")
	createCmd.Flags().String("output", "", "Save generated YAML to this file path")
	createCmd.Flags().Bool("dry-run", false, "Print generated YAML without prompting to save")
}

func runCreate(cmd *cobra.Command, args []string) error {
	description, _ := cmd.Flags().GetString("describe")
	repos, _ := cmd.Flags().GetStringArray("repo")
	outputPath, _ := cmd.Flags().GetString("output")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if description == "" {
		// Interactive fallback: prompt for description
		fmt.Print("Describe what you want the agent to do: ")
		var sb strings.Builder
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if buf[0] == '\n' {
					break
				}
				sb.WriteByte(buf[0])
			}
			if err != nil {
				break
			}
		}
		description = strings.TrimSpace(sb.String())
		if description == "" {
			return fmt.Errorf("description is required (use --describe or enter interactively)")
		}
	}

	fmt.Fprintf(os.Stderr, "Generating task YAML...\n")

	yamlStr, err := generateTaskYAML(cmd.Context(), description, repos)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Validate
	if _, err := validateTaskYAML(yamlStr); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: generated YAML may have issues: %v\n", err)
	}

	// Print the YAML
	fmt.Println("---")
	fmt.Print(yamlStr)
	fmt.Println("---")

	if dryRun {
		return nil
	}

	// Confirmation prompt
	return confirmAndSave(yamlStr, outputPath)
}

// generateTaskYAML calls Claude to generate a Task YAML from a description.
func generateTaskYAML(ctx context.Context, description string, repos []string) (string, error) {
	systemPrompt := buildSystemPrompt()

	var userMsg strings.Builder
	userMsg.WriteString("Generate a Fleetlift task YAML for the following:\n\n")
	userMsg.WriteString(description)
	if len(repos) > 0 {
		userMsg.WriteString("\n\nRepositories to include:\n")
		for _, r := range repos {
			userMsg.WriteString("  - url: ")
			userMsg.WriteString(r)
			userMsg.WriteString("\n")
		}
	}

	client := anthropic.NewClient()
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{OfText: &anthropic.TextBlockParam{Text: userMsg.String()}},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("Claude API error: %w", err)
	}

	var raw string
	for _, block := range msg.Content {
		if block.Type == "text" {
			raw += block.Text
		}
	}

	return extractYAML(raw), nil
}

// confirmAndSave shows [Y/n/e] prompt and handles save/edit/discard.
func confirmAndSave(yamlStr, outputPath string) error {
	reader := newStdinReader()
	for {
		if outputPath != "" {
			fmt.Printf("\nSave to %s? [Y]es / [n]o / [e]dit: ", outputPath)
		} else {
			fmt.Print("\nSave task? [Y]es (requires --output) / [n]o / [e]dit: ")
		}

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))

		switch line {
		case "y", "yes", "":
			if outputPath == "" {
				fmt.Fprintln(os.Stderr, "Use --output <file> to specify where to save.")
				continue
			}
			if err := os.WriteFile(outputPath, []byte(yamlStr), 0o644); err != nil {
				return fmt.Errorf("writing file: %w", err)
			}
			fmt.Printf("Saved to %s\n", outputPath)
			fmt.Printf("Run with: fleetlift run %s\n", outputPath)
			return nil
		case "n", "no":
			fmt.Println("Discarded.")
			return nil
		case "e", "edit":
			edited, err := openEditor(yamlStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Editor error: %v\n", err)
				continue
			}
			yamlStr = edited
			// Re-print and re-validate
			fmt.Println("---")
			fmt.Print(yamlStr)
			fmt.Println("---")
			if _, err := validateTaskYAML(yamlStr); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: YAML may have issues: %v\n", err)
			}
		default:
			fmt.Println("Please enter Y, n, or e.")
		}
	}
}

// openEditor writes content to a temp file, opens $EDITOR, and returns the edited content.
func openEditor(content string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "fleetlift-task-*.yaml")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	c := exec.Command(editor, tmpFile.Name())
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	edited, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("reading edited file: %w", err)
	}
	return string(edited), nil
}
```

> **Implementation notes:**
> - `newStdinReader()` — this is a helper used by `runKnowledgeReview` already (check if it exists as `bufio.NewReader(os.Stdin)`). If `newStdinReader` doesn't exist as a function, replace it with `bufio.NewReader(os.Stdin)` and add `"bufio"` to imports.
> - The `init()` function in `create.go` registers flags. The command itself is registered in `main.go`.
> - Double-check how `MessageNewParams` uses `System` — it's `[]anthropic.TextBlockParam`, so `{Text: systemPrompt}` is correct (no pointer needed).

### Step 3: Register `createCmd` in `cmd/cli/main.go`

In `cmd/cli/main.go`, in the `init()` function or wherever other commands are registered with `rootCmd.AddCommand(...)`, add:

```go
rootCmd.AddCommand(createCmd)
```

Find the pattern in `main.go` and add it alongside other commands.

### Step 4: Build check

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go build ./cmd/cli/... 2>&1
```

Fix any compilation errors (usually import issues). Common fixes:
- If `newStdinReader()` is undefined: replace with `bufio.NewReader(os.Stdin)` and add `"bufio"` to imports
- If `anthropic.ModelClaudeSonnet4_6` is undefined: use `anthropic.Model("claude-sonnet-4-6")`
- Ensure `context` is imported for `generateTaskYAML`

### Step 5: Run all tests

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go test ./cmd/cli/... -v 2>&1 | tail -20
```

```bash
cd /Users/andrew/dev/code/projects/fleetlift && go test ./... 2>&1 | grep -E "FAIL|ok"
```

### Step 6: Lint

```bash
cd /Users/andrew/dev/code/projects/fleetlift && make lint 2>&1 | head -20
```

### Step 7: Update ROADMAP.md

In `docs/plans/ROADMAP.md`, find the Phase 11 section and mark implemented items:

```markdown
### 11.1 Core commands
- [x] `fleetlift create --describe "..."` — one-shot; Claude infers all params, writes YAML
- [x] Generated YAML validated against schema; show with syntax highlighting; prompt `[Y/n/edit]`
- [x] `--dry-run`, `--output task.yaml`
- [ ] `fleetlift create` — multi-step interactive session; asks for repos, prompt, verifiers, mode, approval (deferred)
- [ ] `edit` choice opens `$EDITOR` ← already implemented in this plan, mark [x]
```

Actually mark:
- `[x]` `fleetlift create --describe "..."`
- `[x]` Generated YAML validated + `[Y/n/edit]` prompt
- `[x]` `--dry-run`, `--output task.yaml`; `edit` choice opens `$EDITOR`
- `[ ]` Multi-step interactive `fleetlift create` (deferred)
- `[ ]` 11.2 Schema bundle (mark `[x]` — done in Task 1)

### Step 8: Commit

```bash
git add cmd/cli/create.go cmd/cli/main.go docs/plans/ROADMAP.md
git commit -m "feat(cli): add 'fleetlift create --describe' command for AI-powered task generation"
```

---

## Unanswered Questions

1. **`newStdinReader` helper**: `cmd/cli/knowledge.go` uses `bufio.NewReader(os.Stdin)` inline (not a shared helper). The `create.go` command should do the same — no shared helper needed.

2. **`model.Execution` field names**: The plan assumes `Execution.Agentic *model.AgenticExecution` and `Execution.Deterministic *model.DeterministicExecution`. Confirm by reading `internal/model/task.go` around the `Execution` struct definition before implementing `validateTaskYAML`.

3. **`MessageNewParams.System` field**: Confirmed in SDK v1.25.0 as `System []TextBlockParam` (not a pointer, not wrapped). Use `[]anthropic.TextBlockParam{{Text: systemPrompt}}` directly.

4. **`anthropic.ModelClaudeSonnet4_6`**: Confirmed present in SDK v1.25.0 at `message.go:4092`. Safe to use.

5. **YAML `---` separator**: The command prints `---` before and after the YAML. This is a YAML document separator. It's cosmetic for the preview — the actual YAML written to file should NOT include these separators (just the raw YAML string from `generateTaskYAML`).
