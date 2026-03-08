// Package create provides AI-assisted task YAML generation.
// It extracts shared logic from the CLI so both CLI and web server can use it.
package create

import (
	_ "embed"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed schema/task-schema.md
var taskSchema string

//go:embed schema/example-transform.yaml
var exampleTransform string

//go:embed schema/example-report.yaml
var exampleReport string

//go:embed templates/dependency-upgrade.yaml
var tmplDependencyUpgrade string

//go:embed templates/api-migration.yaml
var tmplAPIMigration string

//go:embed templates/security-audit.yaml
var tmplSecurityAudit string

//go:embed templates/framework-upgrade.yaml
var tmplFrameworkUpgrade string

// Template is a named task YAML template.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content,omitempty"`
}

// BuiltinTemplates is the registry of embedded templates.
var BuiltinTemplates = []Template{
	{Name: "dependency-upgrade", Description: "Upgrade outdated dependencies to latest compatible versions", Content: tmplDependencyUpgrade},
	{Name: "api-migration", Description: "Migrate from a deprecated API version to a new one", Content: tmplAPIMigration},
	{Name: "security-audit", Description: "Audit repository for common security vulnerabilities and issues", Content: tmplSecurityAudit},
	{Name: "framework-upgrade", Description: "Upgrade a framework or runtime to a new major version", Content: tmplFrameworkUpgrade},
}

// ExtractTemplateDescription reads the `description` field from YAML content.
func ExtractTemplateDescription(content string) string {
	var raw struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return ""
	}
	return raw.Description
}

// GenerationMarker is the signal Claude uses to indicate YAML generation in interactive mode.
const GenerationMarker = "---YAML---"

// HasGenerationMarker reports whether a Claude response contains the YAML generation signal.
func HasGenerationMarker(response string) bool {
	return strings.Contains(response, GenerationMarker)
}

// ExtractYAMLFromMarker extracts and returns the YAML portion after the generation marker.
func ExtractYAMLFromMarker(response string) string {
	_, after, found := strings.Cut(response, GenerationMarker)
	if !found {
		return ""
	}
	return ExtractYAML(strings.TrimLeft(after, "\n"))
}

// ExtractYAML strips markdown code fences from an LLM response, returning only the YAML content.
func ExtractYAML(response string) string {
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

// BuildInteractiveSystemPrompt builds the system prompt for interactive multi-turn mode.
func BuildInteractiveSystemPrompt() string {
	return strings.Join([]string{
		"You are an expert Fleetlift assistant — knowledgeable, precise, and helpful.",
		"Your job is to help users create Fleetlift task YAML files through natural conversation.",
		"",
		"## Persona",
		"- You understand software engineering workflows deeply: CI/CD, code migrations, dependency management, security auditing",
		"- You proactively suggest best practices (verifiers, timeouts, labels, approval gates)",
		"- You ask clarifying questions ONE AT A TIME to keep the conversation focused",
		"- When the user's intent is clear, you fill in sensible defaults rather than asking about every field",
		"- You explain WHY you make certain choices (e.g., \"I'm adding a build verifier because this is a code change\")",
		"",
		"## Conversation Flow",
		"1. Understand what the user wants to accomplish",
		"2. Ask about repositories (which repos, branches)",
		"3. Determine execution mode (agentic vs deterministic, transform vs report)",
		"4. Gather any additional requirements (PR config, timeouts, groups)",
		"5. When you have enough info, output " + GenerationMarker + " on its own line, followed by the complete YAML",
		"",
		"## Output Rules",
		"- After " + GenerationMarker + ", output ONLY raw YAML — no markdown fences, no explanations",
		"- Before the marker, you may include a brief summary of what you're generating",
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

// BuildSystemPrompt builds the system prompt for one-shot generation.
func BuildSystemPrompt() string {
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
