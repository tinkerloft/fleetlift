// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// Signal and query names
const (
	SignalApprove  = "approve"
	SignalReject   = "reject"
	SignalCancel   = "cancel"
	SignalSteer    = "steer"
	SignalContinue = "continue"

	QueryStatus            = "get_status"
	QueryResult            = "get_claude_result"
	QueryDiff              = "get_diff"
	QueryVerifierLogs      = "get_verifier_logs"
	QuerySteeringState     = "get_steering_state"
	QueryExecutionProgress = "get_execution_progress"
)

// DefaultMaxSteeringIterations is the default limit for steering iterations.
const DefaultMaxSteeringIterations = 5

// Transform is the main workflow for code transformations.
// It delegates to TransformV2 which uses the sidecar agent pattern.
func Transform(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
	return TransformV2(ctx, task)
}

// substitutePromptTemplate substitutes {{.Name}} and {{.Context}} variables in the prompt.
func substitutePromptTemplate(prompt string, target model.ForEachTarget) (string, error) {
	tmpl, err := template.New("prompt").Parse(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, target); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildDiffSummary creates a human-readable summary of diffs for notifications.
func buildDiffSummary(diffs []model.DiffOutput) string {
	if len(diffs) == 0 {
		return "No changes detected."
	}

	var sb strings.Builder
	sb.WriteString("**Diff summary:**\n")

	for _, diff := range diffs {
		if len(diff.Files) == 0 {
			sb.WriteString(fmt.Sprintf("- **%s**: no changes\n", diff.Repository))
			continue
		}

		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", diff.Repository, diff.Summary))
		for _, f := range diff.Files {
			sb.WriteString(fmt.Sprintf("  - %s (%s, +%d/-%d)\n", f.Path, f.Status, f.Additions, f.Deletions))
		}
	}

	return sb.String()
}
