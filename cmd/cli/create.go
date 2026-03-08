package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/config"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/state"
)

// extractYAML strips markdown code fences from an LLM response, returning only the YAML content.
func extractYAML(response string) string {
	if idx := strings.Index(response, "```yaml"); idx != -1 {
		start := idx + len("```yaml")
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimLeft(response[start:start+end], "\n")
		}
	}
	if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimLeft(response[start:start+end], "\n")
		}
	}
	return response
}

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

// validateTaskYAML parses and validates the required fields of a Fleetlift task YAML string.
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
	hasRepos := len(task.Repositories) > 0 || len(task.Targets) > 0 || len(task.Groups) > 0
	if !hasRepos {
		return model.Task{}, fmt.Errorf("task is missing required field: repositories (at least one repo)")
	}
	return task, nil
}

// buildInteractiveSystemPrompt builds the system prompt for interactive multi-turn mode.
// Claude asks clarifying questions one at a time and signals completion with the generation marker.
func buildInteractiveSystemPrompt() string {
	return strings.Join([]string{
		"You are an expert at writing Fleetlift task YAML files.",
		"In interactive mode, ask the user clarifying questions one at a time to gather all required information.",
		"When you have enough information, output " + generationMarker + " on its own line, followed immediately by the complete YAML — no markdown fences, no explanations.",
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

// buildSystemPrompt builds the system prompt for the LLM that generates task YAML.
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

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Generate a task YAML using AI from a natural language description",
	Long: `Generate a Fleetlift task YAML file using Claude.

Examples:
  # One-shot from description:
  fleetlift create --describe "Add OpenTelemetry tracing to all Go services" \
    --repo https://github.com/acme/api.git \
    --output tracing-task.yaml

  # Generate, save, and immediately run:
  fleetlift create --describe "Add OpenTelemetry tracing to all Go services" \
    --repo https://github.com/acme/api.git \
    --output tracing-task.yaml --run

  # Preview without saving:
  fleetlift create --describe "Security audit of auth module" --dry-run`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().String("describe", "", "Natural language description of the task (required)")
	createCmd.Flags().StringArray("repo", nil, "Repository URL to include (repeatable)")
	createCmd.Flags().String("output", "", "Save generated YAML to this file path")
	createCmd.Flags().Bool("dry-run", false, "Print generated YAML without prompting to save")
	createCmd.Flags().Bool("run", false, "Immediately execute after saving (requires --output)")
}

func runCreate(cmd *cobra.Command, args []string) error {
	description, _ := cmd.Flags().GetString("describe")
	repos, _ := cmd.Flags().GetStringArray("repo")
	outputPath, _ := cmd.Flags().GetString("output")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	runAfter, _ := cmd.Flags().GetBool("run")

	if runAfter && outputPath == "" {
		return fmt.Errorf("--run requires --output to specify where to save the task file")
	}

	if description == "" {
		fmt.Print("Describe what you want the agent to do: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		description = strings.TrimSpace(line)
		if description == "" {
			return fmt.Errorf("description is required (use --describe or enter interactively)")
		}
	}

	fmt.Fprintf(os.Stderr, "Generating task YAML...\n")

	yamlStr, err := generateTaskYAML(cmd.Context(), description, repos)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	if _, err := validateTaskYAML(yamlStr); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: generated YAML may have issues: %v\n", err)
	}

	fmt.Println("---")
	fmt.Print(yamlStr)
	fmt.Println("---")

	if dryRun {
		return nil
	}

	if runAfter {
		if err := os.WriteFile(outputPath, []byte(yamlStr), 0o644); err != nil {
			return fmt.Errorf("writing file: %w", err)
		}
		fmt.Printf("Saved to %s\n", outputPath)
		return startRunFromFile(cmd.Context(), outputPath)
	}

	return confirmAndSave(yamlStr, outputPath)
}

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

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
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
	if raw == "" {
		return "", fmt.Errorf("Claude returned an empty response")
	}

	return extractYAML(raw), nil
}

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

func startRunFromFile(ctx context.Context, filePath string) error {
	task, err := config.LoadTaskFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to load task file: %w", err)
	}

	workflowID, err := client.StartTransform(ctx, *task)
	if err != nil {
		return fmt.Errorf("failed to start workflow: %w", err)
	}

	if saveErr := state.SaveLastWorkflow(workflowID); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save last workflow: %v\n", saveErr)
	}

	fmt.Printf("Workflow started: %s\n", workflowID)
	fmt.Printf("View at: http://localhost:8233/namespaces/default/workflows/%s\n", workflowID)
	return nil
}

func confirmAndSave(yamlStr, outputPath string) error {
	reader := bufio.NewReader(os.Stdin)
	for {
		if outputPath != "" {
			fmt.Printf("\nSave to %s? [Y]es / [n]o / [e]dit: ", outputPath)
		} else {
			fmt.Print("\nSave task? [Y]es (requires --output) / [n]o / [e]dit: ")
		}

		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(os.Stderr, "No input; discarding.")
			return nil
		}
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
	defer tmpFile.Close() // ensures close on all error paths

	if _, err := tmpFile.WriteString(content); err != nil {
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("flushing temp file: %w", err)
	}

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
