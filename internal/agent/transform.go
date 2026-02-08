package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// blockedEnvVars are environment variable names that manifest env overrides cannot set.
var blockedEnvVars = map[string]bool{
	"PATH":             true,
	"HOME":             true,
	"USER":             true,
	"SHELL":            true,
	"LD_PRELOAD":       true,
	"LD_LIBRARY_PATH":  true,
	"ANTHROPIC_API_KEY": true,
	"GITHUB_TOKEN":     true,
}

// filterEnv removes blocked keys from an environment slice.
func filterEnv(env []string, blockKeys map[string]bool) []string {
	var filtered []string
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if !blockKeys[strings.ToUpper(k)] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// runTransformation executes the transformation (agentic or deterministic).
func (p *Pipeline) runTransformation(ctx context.Context, manifest *protocol.TaskManifest) (string, error) {
	switch manifest.Execution.Type {
	case "agentic":
		return p.runAgenticTransformation(ctx, manifest)
	case "deterministic":
		return p.runDeterministicTransformation(ctx, manifest)
	default:
		if manifest.Execution.Type != "" {
			p.logger.Warn("Unrecognized execution type, defaulting to agentic", "type", manifest.Execution.Type)
		}
		return p.runAgenticTransformation(ctx, manifest)
	}
}

// runAgenticTransformation runs Claude Code with the manifest prompt.
// C1 fix: pass prompt directly to -p flag — exec.CommandContext uses execve, no shell involved.
func (p *Pipeline) runAgenticTransformation(ctx context.Context, manifest *protocol.TaskManifest) (string, error) {
	prompt := p.buildTransformPrompt(manifest)

	args := []string{
		"-p", prompt,
		"--output-format", "text",
		"--dangerously-skip-permissions",
		"--verbose",
	}

	// Filter GITHUB_TOKEN from env — Claude Code doesn't need git credentials
	claudeEnv := filterEnv(os.Environ(), map[string]bool{"GITHUB_TOKEN": true})

	p.logger.Info("Running Claude Code")
	result, err := p.exec.Run(ctx, CommandOpts{
		Name: "claude",
		Args: args,
		Dir:  protocol.WorkspacePath,
		Env:  claudeEnv,
	})
	if err != nil {
		output := ""
		if result != nil {
			output = result.Stdout + result.Stderr
		}
		return output, fmt.Errorf("claude code failed: %w\nOutput: %s", err, output)
	}

	return result.Stdout + result.Stderr, nil
}

// runDeterministicTransformation runs the command specified in the manifest directly in the sandbox.
func (p *Pipeline) runDeterministicTransformation(ctx context.Context, manifest *protocol.TaskManifest) (string, error) {
	if len(manifest.Execution.Command) == 0 {
		return "", fmt.Errorf("deterministic execution requires command")
	}

	args := manifest.Execution.Command[1:]
	if len(manifest.Execution.Args) > 0 {
		args = append(args, manifest.Execution.Args...)
	}

	env := os.Environ()
	for k, v := range manifest.Execution.Env {
		if blockedEnvVars[strings.ToUpper(k)] {
			p.logger.Warn("Blocked env var override", "key", k)
			continue
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	p.logger.Info("Running deterministic transformation", "command", strings.Join(manifest.Execution.Command, " "))
	result, err := p.exec.Run(ctx, CommandOpts{
		Name: manifest.Execution.Command[0],
		Args: args,
		Dir:  protocol.WorkspacePath,
		Env:  env,
	})
	if err != nil {
		output := ""
		if result != nil {
			output = result.Stdout + result.Stderr
		}
		return output, fmt.Errorf("deterministic transformation failed: %w\nOutput: %s", err, output)
	}

	return result.Stdout + result.Stderr, nil
}

// runSteeringTransformation runs Claude Code with a steering prompt that includes prior context.
// C1 fix: pass prompt directly — no base64 encoding needed.
func (p *Pipeline) runSteeringTransformation(ctx context.Context, manifest *protocol.TaskManifest, steeringPrompt string, iteration int, previousOutput string) (string, error) {
	prompt := p.buildSteeringPrompt(manifest, steeringPrompt, iteration, previousOutput)

	args := []string{
		"-p", prompt,
		"--output-format", "text",
		"--dangerously-skip-permissions",
		"--verbose",
	}

	// Filter GITHUB_TOKEN from env — Claude Code doesn't need git credentials
	claudeEnv := filterEnv(os.Environ(), map[string]bool{"GITHUB_TOKEN": true})

	p.logger.Info("Running Claude Code (steering)", "iteration", iteration)
	result, err := p.exec.Run(ctx, CommandOpts{
		Name: "claude",
		Args: args,
		Dir:  protocol.WorkspacePath,
		Env:  claudeEnv,
	})
	if err != nil {
		output := ""
		if result != nil {
			output = result.Stdout + result.Stderr
		}
		return output, fmt.Errorf("claude code steering failed: %w", err)
	}

	return result.Stdout + result.Stderr, nil
}

func (p *Pipeline) buildTransformPrompt(manifest *protocol.TaskManifest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", manifest.Title))
	sb.WriteString(fmt.Sprintf("Instructions:\n%s\n\n", manifest.Execution.Prompt))

	sb.WriteString("Repositories:\n")
	repos := manifest.EffectiveRepos()
	for _, repo := range repos {
		repoPath := manifest.RepoPath(repo.Name)
		sb.WriteString(fmt.Sprintf("- %s (in %s)\n", repo.Name, repoPath))
	}

	sb.WriteString("\nMake minimal, targeted changes. Follow existing code style.\n")

	// ForEach targets (M3 fix)
	if len(manifest.ForEach) > 0 {
		sb.WriteString("\n## Targets\n\nApply the transformation to each of these targets:\n\n")
		for _, target := range manifest.ForEach {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", target.Name, target.Context))
		}
		sb.WriteString("\nProcess each target and include its name in any output files.\n")
	}

	// Verifier instructions
	if len(manifest.Verifiers) > 0 {
		sb.WriteString("\n## Verification\n\nAfter changes, run these and fix any errors:\n\n")
		for _, v := range manifest.Verifiers {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", v.Name, strings.Join(v.Command, " ")))
		}
		sb.WriteString("\nAll verifiers must pass.\n")
	}

	// Report mode instructions
	if manifest.Mode == "report" {
		sb.WriteString("\n## Output Requirements\n\n")
		if len(manifest.ForEach) > 0 {
			sb.WriteString("Write per-target reports as REPORT-{target}.md with YAML frontmatter.\n")
		} else {
			sb.WriteString("Write your report to REPORT.md with YAML frontmatter.\n")
		}
	}

	return sb.String()
}

func (p *Pipeline) buildSteeringPrompt(manifest *protocol.TaskManifest, steeringPrompt string, iteration int, previousOutput string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", manifest.Title))
	sb.WriteString(fmt.Sprintf("Original instructions:\n%s\n\n", manifest.Execution.Prompt))
	sb.WriteString(fmt.Sprintf("--- Steering iteration %d ---\n\n", iteration))
	sb.WriteString(fmt.Sprintf("Additional feedback from reviewer:\n%s\n\n", steeringPrompt))

	if previousOutput != "" {
		// Truncate long previous output
		output := previousOutput
		if len(output) > MaxSteeringContextChars {
			output = output[:MaxSteeringContextChars] + "\n... [truncated]"
		}
		sb.WriteString(fmt.Sprintf("Previous run output:\n%s\n\n", output))
	}

	sb.WriteString("Please address the feedback above. The previous changes are still in the workspace.\n")

	return sb.String()
}
