# Phase 11.1: Interactive Create Command — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `fleetlift create --interactive` that runs a multi-turn conversation with Claude to gather task requirements and produce a task YAML file.

**Architecture:** `--interactive` / `-i` flag branches `runCreate` into a new `runInteractiveCreate` function. A `[]anthropic.MessageParam` history is maintained in memory. Claude asks questions one at a time; when it signals readiness with `---YAML---`, the CLI extracts the YAML and passes it to the existing `confirmAndSave` / `startRunFromFile` flow. All one-shot code is unchanged.

**Tech Stack:** Go, `github.com/anthropics/anthropic-sdk-go`, `bufio.Scanner`, cobra

---

### Task 1: Add generation marker helpers + tests

**Files:**
- Modify: `cmd/cli/create.go`
- Modify: `cmd/cli/create_test.go`

**Step 1: Write the failing tests**

Add to `cmd/cli/create_test.go`:

```go
func TestHasGenerationMarker_Present(t *testing.T) {
    assert.True(t, hasGenerationMarker("Here is the task:\n---YAML---\nversion: 1\n"))
}

func TestHasGenerationMarker_Absent(t *testing.T) {
    assert.False(t, hasGenerationMarker("Just a question, no YAML yet."))
}

func TestExtractYAMLFromMarker_Basic(t *testing.T) {
    response := "I have enough info.\n---YAML---\nversion: 1\ntitle: Test\n"
    result := extractYAMLFromMarker(response)
    assert.Equal(t, "version: 1\ntitle: Test\n", result)
}

func TestExtractYAMLFromMarker_StripsFences(t *testing.T) {
    response := "Ready.\n---YAML---\n```yaml\nversion: 1\ntitle: Test\n```\n"
    result := extractYAMLFromMarker(response)
    assert.Equal(t, "version: 1\ntitle: Test\n", result)
}

func TestExtractYAMLFromMarker_NoMarker(t *testing.T) {
    result := extractYAMLFromMarker("no marker here")
    assert.Equal(t, "", result)
}
```

**Step 2: Run tests to verify they fail**

```bash
cd cmd/cli && go test -run "TestHasGenerationMarker|TestExtractYAMLFromMarker" -v .
```
Expected: FAIL — `hasGenerationMarker` and `extractYAMLFromMarker` undefined.

**Step 3: Add the helpers to `create.go`**

Add after the `extractYAML` function (line 36):

```go
const generationMarker = "---YAML---"

// hasGenerationMarker reports whether a Claude response contains the YAML generation signal.
func hasGenerationMarker(response string) bool {
	return strings.Contains(response, generationMarker)
}

// extractYAMLFromMarker extracts and returns the YAML portion after the generation marker.
// Returns empty string if the marker is not present.
func extractYAMLFromMarker(response string) string {
	_, after, found := strings.Cut(response, generationMarker)
	if !found {
		return ""
	}
	return extractYAML(strings.TrimLeft(after, "\n"))
}
```

**Step 4: Run tests to verify they pass**

```bash
cd cmd/cli && go test -run "TestHasGenerationMarker|TestExtractYAMLFromMarker" -v .
```
Expected: PASS (5 tests).

**Step 5: Commit**

```bash
git add cmd/cli/create.go cmd/cli/create_test.go
git commit -m "feat(create): add generation marker helpers for interactive mode"
```

---

### Task 2: Add interactive system prompt builder + test

**Files:**
- Modify: `cmd/cli/create.go`
- Modify: `cmd/cli/create_test.go`

**Step 1: Write the failing test**

Add to `cmd/cli/create_test.go`:

```go
func TestBuildInteractiveSystemPrompt_ContainsMarkerInstruction(t *testing.T) {
	prompt := buildInteractiveSystemPrompt()
	assert.Contains(t, prompt, "---YAML---")
	assert.Contains(t, prompt, "one at a time")
	// Also contains the schema (inherited from buildSystemPrompt)
	assert.Contains(t, prompt, "version: 1")
}
```

**Step 2: Run test to verify it fails**

```bash
cd cmd/cli && go test -run TestBuildInteractiveSystemPrompt -v .
```
Expected: FAIL — `buildInteractiveSystemPrompt` undefined.

**Step 3: Add the function to `create.go`**

Add after `buildSystemPrompt()`:

```go
// buildInteractiveSystemPrompt builds the system prompt for the multi-turn interactive session.
func buildInteractiveSystemPrompt() string {
	return buildSystemPrompt() + "\n\n" + strings.Join([]string{
		"# Interactive Session Instructions",
		"",
		"You are guiding a user through creating a Fleetlift task YAML interactively.",
		"Ask questions ONE AT A TIME. Cover these topics in order:",
		"1. What the agent should do (the prompt or transformation)",
		"2. Which repositories to run against (URLs)",
		"3. Mode: transform (creates pull requests) or report (collects findings, no PRs)",
		"4. Verifiers to run after the change (e.g. go build ./..., go test ./..., npm test)",
		"5. Any other options the user raises (approval, timeout, etc.) — do NOT ask about these proactively",
		"",
		"When you have enough information to generate a valid task, output EXACTLY:",
		"---YAML---",
		"<yaml content here>",
		"",
		"Rules:",
		"- Do NOT output the ---YAML--- marker until you have all required information",
		"- Do NOT output any prose after the ---YAML--- marker",
		"- Do NOT use markdown code fences around the YAML",
	}, "\n")
}
```

**Step 4: Run test to verify it passes**

```bash
cd cmd/cli && go test -run TestBuildInteractiveSystemPrompt -v .
```
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/cli/create.go cmd/cli/create_test.go
git commit -m "feat(create): add interactive system prompt builder"
```

---

### Task 3: Add `sendConversationMessage` helper

**Files:**
- Modify: `cmd/cli/create.go`

**Step 1: Add the helper** (no dedicated unit test — tested end-to-end via interactive flow; the API call itself is integration-level)

Add after `generateTaskYAML`:

```go
// sendConversationMessage appends a user message to the history, calls Claude, appends
// the assistant reply, and returns the updated history and the reply text.
func sendConversationMessage(
	ctx context.Context,
	c *anthropic.Client,
	systemPrompt string,
	history []anthropic.MessageParam,
	userText string,
) ([]anthropic.MessageParam, string, error) {
	history = append(history, anthropic.MessageParam{
		Role: anthropic.MessageParamRoleUser,
		Content: []anthropic.ContentBlockParamUnion{
			{OfText: &anthropic.TextBlockParam{Text: userText}},
		},
	})

	msg, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  history,
	})
	if err != nil {
		return history, "", fmt.Errorf("Claude API error: %w", err)
	}

	var reply string
	for _, block := range msg.Content {
		if block.Type == "text" {
			reply += block.Text
		}
	}

	history = append(history, anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfText: &anthropic.TextBlockParam{Text: reply}},
		},
	})

	return history, reply, nil
}
```

**Step 2: Build to verify it compiles**

```bash
go build ./cmd/cli/
```
Expected: no errors.

**Step 3: Commit**

```bash
git add cmd/cli/create.go
git commit -m "feat(create): add sendConversationMessage helper"
```

---

### Task 4: Implement `runInteractiveCreate`

**Files:**
- Modify: `cmd/cli/create.go`

**Step 1: Add the function** after `startRunFromFile`:

```go
// runInteractiveCreate runs a multi-turn conversation with Claude to gather task requirements
// and produce a task YAML file. Claude asks questions; when ready it emits ---YAML--- followed
// by the task YAML, at which point the session ends and confirmAndSave is called.
func runInteractiveCreate(cmd *cobra.Command, outputPath string, runAfter bool) error {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	systemPrompt := buildInteractiveSystemPrompt()
	apiClient := anthropic.NewClient()
	history := []anthropic.MessageParam{}
	scanner := bufio.NewScanner(os.Stdin)

	// Trigger Claude's opening question with a hidden seed message.
	var err error
	var response string
	history, response, err = sendConversationMessage(
		cmd.Context(), apiClient, systemPrompt, history,
		"Hello! I'd like to create a Fleetlift task.",
	)
	if err != nil {
		return err
	}
	fmt.Printf("\nClaude: %s\n", response)

	for {
		fmt.Print("\nYou: ")
		if !scanner.Scan() {
			fmt.Println("\nSession ended.")
			return nil
		}
		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		history, response, err = sendConversationMessage(
			cmd.Context(), apiClient, systemPrompt, history, userInput,
		)
		if err != nil {
			return err
		}

		if hasGenerationMarker(response) {
			// Print any prose before the marker.
			before, _, _ := strings.Cut(response, generationMarker)
			if msg := strings.TrimSpace(before); msg != "" {
				fmt.Printf("\nClaude: %s\n", msg)
			}

			yamlStr := extractYAMLFromMarker(response)
			if _, valErr := validateTaskYAML(yamlStr); valErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: generated YAML may have issues: %v\n", valErr)
			}

			fmt.Println("\n---")
			fmt.Print(yamlStr)
			fmt.Println("---")

			if runAfter {
				if err := os.WriteFile(outputPath, []byte(yamlStr), 0o644); err != nil {
					return fmt.Errorf("writing file: %w", err)
				}
				fmt.Printf("Saved to %s\n", outputPath)
				return startRunFromFile(cmd.Context(), outputPath)
			}
			return confirmAndSave(yamlStr, outputPath)
		}

		fmt.Printf("\nClaude: %s\n", response)
	}
}
```

**Step 2: Build to verify it compiles**

```bash
go build ./cmd/cli/
```
Expected: no errors.

**Step 3: Commit**

```bash
git add cmd/cli/create.go
git commit -m "feat(create): implement runInteractiveCreate conversation loop"
```

---

### Task 5: Wire in `--interactive` flag and update `runCreate`

**Files:**
- Modify: `cmd/cli/create.go`

**Step 1: Add flag to `init()`**

Add to the `init()` function after the `--run` flag line:

```go
createCmd.Flags().BoolP("interactive", "i", false, "Start a multi-turn conversation with Claude to build the task")
```

**Step 2: Update `createCmd.Long`**

Add to the `Long` field of `createCmd` (after the `--dry-run` example):

```
  # Interactive multi-turn session:
  fleetlift create --interactive --output task.yaml
```

**Step 3: Update `runCreate` to branch on `--interactive`**

Add at the top of `runCreate` (after the existing `runAfter` check):

```go
interactive, _ := cmd.Flags().GetBool("interactive")
if interactive {
    return runInteractiveCreate(cmd, outputPath, runAfter)
}
```

The full updated top of `runCreate`:

```go
func runCreate(cmd *cobra.Command, args []string) error {
	description, _ := cmd.Flags().GetString("describe")
	repos, _ := cmd.Flags().GetStringArray("repo")
	outputPath, _ := cmd.Flags().GetString("output")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	runAfter, _ := cmd.Flags().GetBool("run")
	interactive, _ := cmd.Flags().GetBool("interactive")

	if runAfter && outputPath == "" {
		return fmt.Errorf("--run requires --output to specify where to save the task file")
	}

	if interactive {
		return runInteractiveCreate(cmd, outputPath, runAfter)
	}

	// ... rest of one-shot path unchanged
```

**Step 4: Build to verify**

```bash
go build ./cmd/cli/
```
Expected: no errors.

**Step 5: Run all CLI tests**

```bash
go test ./cmd/cli/...
```
Expected: all pass.

**Step 6: Commit**

```bash
git add cmd/cli/create.go
git commit -m "feat(create): wire --interactive flag to conversation loop"
```

---

### Task 6: Run lint, full test suite, and update plan

**Step 1: Lint**

```bash
make lint
```
Expected: no errors.

**Step 2: Full test suite**

```bash
go test ./...
```
Expected: all pass.

**Step 3: Update implementation plan**

In `docs/plans/IMPLEMENTATION_PLAN.md`, update Phase 11.1:

```markdown
### 11.1 Interactive Create Command ✅ Complete

`fleetlift create --interactive` (`-i`) starts a multi-turn conversation with Claude. Claude asks questions one at a time (mode, repos, execution, verifiers), then emits `---YAML---` followed by the task YAML when it has enough information. The CLI extracts the YAML and passes it to the existing save/edit/run flow. Compatible with `--output` and `--run`.
```

**Step 4: Commit**

```bash
git add docs/plans/IMPLEMENTATION_PLAN.md
git commit -m "docs: mark Phase 11.1 interactive create complete"
```
