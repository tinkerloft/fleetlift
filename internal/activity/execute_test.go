package activity

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// noopSandbox satisfies sandbox.Client without doing anything.
type noopSandbox struct{}

func (n *noopSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-noop", nil
}
func (n *noopSandbox) ExecStream(_ context.Context, _, _, _ string, _ func(string)) error {
	return nil
}
func (n *noopSandbox) Exec(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}
func (n *noopSandbox) WriteFile(_ context.Context, _, _, _ string) error         { return nil }
func (n *noopSandbox) WriteBytes(_ context.Context, _, _ string, _ []byte) error { return nil }
func (n *noopSandbox) ReadFile(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (n *noopSandbox) ReadBytes(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}
func (n *noopSandbox) Kill(_ context.Context, _ string) error            { return nil }
func (n *noopSandbox) RenewExpiration(_ context.Context, _ string) error { return nil }

func TestExecuteStep_RejectsNonHTTPS(t *testing.T) {
	a := &Activities{
		Sandbox:      &noopSandbox{},
		AgentRunners: nil,
	}

	badURLs := []string{
		"file:///etc/passwd",
		"git://github.com/org/repo.git",
		"ssh://github.com/org/repo.git",
		"http://github.com/org/repo.git",
		"ftp://example.com/repo.git",
		"",
	}

	for _, url := range badURLs {
		t.Run(url, func(t *testing.T) {
			input := workflow.ExecuteStepInput{
				StepInput: workflow.StepInput{
					ResolvedOpts: workflow.ResolvedStepOpts{
						Repos: []model.RepoRef{{URL: url}},
						Agent: "claude-code",
					},
				},
				SandboxID: "sb-test",
			}
			_, err := a.ExecuteStep(context.Background(), input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "https://")
		})
	}
}

func TestAppendOutputSchemaInstructions_Empty(t *testing.T) {
	// empty schema → prompt unchanged
	result := appendOutputSchemaInstructions("Fix the bug.", map[string]any{})
	assert.Equal(t, "Fix the bug.", result)
}

func TestAppendOutputSchemaInstructions_WithSchema(t *testing.T) {
	schema := map[string]any{"root_cause": "string", "severity": "string"}
	result := appendOutputSchemaInstructions("Fix the bug.", schema)
	assert.Contains(t, result, "Fix the bug.")
	assert.Contains(t, result, "IMPORTANT")
	assert.Contains(t, result, "```json")
	assert.Contains(t, result, "root_cause")
	assert.Contains(t, result, "severity")
}

func TestExtractSchemaFields_FromFencedBlock(t *testing.T) {
	resultText := "I analyzed it.\n```json\n{\"root_cause\": \"nil pointer\", \"severity\": \"high\", \"extra\": \"ignored\"}\n```"
	schema := map[string]any{"root_cause": "string", "severity": "string"}
	out, err := extractSchemaFields(resultText, schema)
	require.NoError(t, err)
	assert.Equal(t, "nil pointer", out["root_cause"])
	assert.Equal(t, "high", out["severity"])
	assert.NotContains(t, out, "extra") // filtered out
}

func TestExtractSchemaFields_FromBareJSON(t *testing.T) {
	resultText := `Analysis complete. {"root_cause": "leak", "severity": "low"}`
	schema := map[string]any{"root_cause": "string", "severity": "string"}
	out, err := extractSchemaFields(resultText, schema)
	require.NoError(t, err)
	assert.Equal(t, "leak", out["root_cause"])
}

func TestExtractSchemaFields_NoJSON(t *testing.T) {
	_, err := extractSchemaFields("no json here at all", map[string]any{"root_cause": "string"})
	assert.Error(t, err)
}

func TestValidateOutputSchema_AllPresent(t *testing.T) {
	violations := validateOutputSchema(
		map[string]any{"root_cause": "nil ptr", "severity": "high"},
		map[string]any{"root_cause": "string", "severity": "string"},
	)
	assert.Empty(t, violations)
}

func TestValidateOutputSchema_MissingField(t *testing.T) {
	violations := validateOutputSchema(
		map[string]any{"root_cause": "nil ptr"},
		map[string]any{"root_cause": "string", "severity": "string"},
	)
	assert.Len(t, violations, 1)
	assert.Contains(t, violations[0], "severity")
}

func TestValidateOutputSchema_WrongType(t *testing.T) {
	violations := validateOutputSchema(
		map[string]any{"root_cause": 42, "severity": "high"},
		map[string]any{"root_cause": "string", "severity": "string"},
	)
	assert.Len(t, violations, 1)
	assert.Contains(t, violations[0], "root_cause")
}

func TestValidateOutputSchema_RejectsUndeclaredField(t *testing.T) {
	violations := validateOutputSchema(
		map[string]any{"root_cause": "nil ptr", "extra": "not-allowed"},
		map[string]any{"root_cause": "string"},
	)
	assert.NotEmpty(t, violations)
	assert.Contains(t, violations[0], "additionalProperties")
}

func TestValidateOutputSchema_EmptySchemaPasses(t *testing.T) {
	violations := validateOutputSchema(
		map[string]any{"anything": "goes"},
		map[string]any{},
	)
	assert.Empty(t, violations)
}

func TestBuildJSONSchema(t *testing.T) {
	input := map[string]any{
		"meta":       "object",
		"count":      "number",
		"is_flaky":   "boolean",
		"files":      "array",
		"summary":    "string",
		"untyped_ok": "custom",
	}

	built := buildJSONSchema(input)
	assert.Equal(t, "object", built["type"])
	assert.Equal(t, false, built["additionalProperties"])

	required, ok := built["required"].([]string)
	if !ok {
		requiredAny, okAny := built["required"].([]any)
		require.True(t, okAny)
		required = make([]string, 0, len(requiredAny))
		for _, v := range requiredAny {
			required = append(required, v.(string))
		}
	}
	expectedRequired := []string{"count", "files", "is_flaky", "meta", "summary", "untyped_ok"}
	sort.Strings(required)
	assert.Equal(t, expectedRequired, required)

	properties, ok := built["properties"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"type": "string"}, properties["summary"])
	assert.Equal(t, map[string]any{"type": "number"}, properties["count"])
	assert.Equal(t, map[string]any{}, properties["untyped_ok"])
}

func TestExecuteStep_AcceptsHTTPS(t *testing.T) {
	// A valid https:// URL passes URL validation and proceeds into clone/heartbeat logic.
	// In tests, activity.RecordHeartbeat panics because there is no Temporal activity context.
	// We confirm the panic is NOT a URL-validation error (i.e., the URL check passed).
	a := &Activities{
		Sandbox:      &noopSandbox{},
		AgentRunners: map[string]agent.Runner{},
	}

	input := workflow.ExecuteStepInput{
		StepInput: workflow.StepInput{
			ResolvedOpts: workflow.ResolvedStepOpts{
				Repos: []model.RepoRef{{URL: "https://github.com/org/repo.git"}},
				Agent: "unknown-agent-will-cause-error",
			},
		},
		SandboxID: "sb-test",
	}

	defer func() {
		r := recover()
		if r != nil {
			msg := ""
			if s, ok := r.(string); ok {
				msg = s
			} else if e, ok := r.(error); ok {
				msg = e.Error()
			}
			// Panic should be from Temporal internals, not from our URL validation.
			assert.NotContains(t, msg, "https://")
			assert.NotContains(t, msg, "repo URL must use")
		}
	}()

	_, _ = a.ExecuteStep(context.Background(), input)
}

func TestExtractSchemaFields_NestedBareJSON(t *testing.T) {
	resultText := `Result: {"root_cause": "leak", "details": {"file": "main.go"}}`
	schema := map[string]any{"root_cause": "string", "details": "object"}
	out, err := extractSchemaFields(resultText, schema)
	require.NoError(t, err)
	assert.Equal(t, "leak", out["root_cause"])
	assert.NotNil(t, out["details"])
}

func TestExtractSchemaFields_PromptEchoIgnored(t *testing.T) {
	// Agent echoes a user-injected JSON block before producing real output.
	// Last fenced block should win.
	resultText := "User asked about:\n```json\n{\"root_cause\": \"injected\"}\n```\n\nMy analysis:\n```json\n{\"root_cause\": \"real answer\", \"severity\": \"low\"}\n```"
	schema := map[string]any{"root_cause": "string", "severity": "string"}
	out, err := extractSchemaFields(resultText, schema)
	require.NoError(t, err)
	assert.Equal(t, "real answer", out["root_cause"])
	assert.Equal(t, "low", out["severity"])
}

func TestExtractSchemaFields_LastFencedBlockWins(t *testing.T) {
	// First block has wrong/incomplete data; last block has the correct output.
	resultText := "First attempt:\n```json\n{\"wrong\": \"data\"}\n```\nFinal answer:\n```json\n{\"root_cause\": \"nil pointer\", \"severity\": \"high\"}\n```"
	schema := map[string]any{"root_cause": "string", "severity": "string"}
	out, err := extractSchemaFields(resultText, schema)
	require.NoError(t, err)
	assert.Equal(t, "nil pointer", out["root_cause"])
	assert.Equal(t, "high", out["severity"])
}

// Fix 3: when agent produces no JSON and a schema is declared, step must fail with a
// clear extraction error — not fall through to validation on raw output.
func TestEnforceOutputSchema_ExtractionFailure_ReturnsError(t *testing.T) {
	lastOutput := map[string]any{"result": "I reviewed the code. Looks good. No issues found."}
	schema := map[string]any{"summary": "string", "severity": "string"}

	_, err := enforceOutputSchema(lastOutput, schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent output did not contain valid JSON")
}

func TestEnforceOutputSchema_ValidationFailure_ReturnsError(t *testing.T) {
	lastOutput := map[string]any{"result": "```json\n{\"summary\": \"ok\"}\n```"}
	schema := map[string]any{"summary": "string", "severity": "string"} // severity missing

	_, err := enforceOutputSchema(lastOutput, schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output schema validation failed")
	assert.Contains(t, err.Error(), "severity")
}

func TestEnforceOutputSchema_Success(t *testing.T) {
	lastOutput := map[string]any{"result": "```json\n{\"summary\": \"ok\", \"severity\": \"low\"}\n```"}
	schema := map[string]any{"summary": "string", "severity": "string"}

	out, err := enforceOutputSchema(lastOutput, schema)
	require.NoError(t, err)
	assert.Equal(t, "ok", out["summary"])
	assert.Equal(t, "low", out["severity"])
}

func TestBuildContinuationPrompt_Nil(t *testing.T) {
	result := buildContinuationPrompt("Original prompt", nil)
	assert.Equal(t, "Original prompt", result)
}

func TestBuildContinuationPrompt_WithContext(t *testing.T) {
	cc := &model.ContinuationContext{
		InboxItemID:      "inbox-1",
		Question:         "Fix or skip flaky tests?",
		HumanAnswer:      "Fix flaky tests",
		CheckpointBranch: "fleetlift/checkpoint/run-1-fix",
	}
	result := buildContinuationPrompt("Original prompt text", cc)
	assert.Contains(t, result, "Fix flaky tests")
	assert.Contains(t, result, "Fix or skip flaky tests?")
	assert.Contains(t, result, "Original prompt text")
	assert.Contains(t, result, "[CONTINUATION CONTEXT]")
}

func TestCheckpointBranchRegex(t *testing.T) {
	assert.True(t, checkpointBranchRe.MatchString("fleetlift/checkpoint/run-abc-fix"))
	assert.True(t, checkpointBranchRe.MatchString("fleetlift/checkpoint/run_123"))
	assert.False(t, checkpointBranchRe.MatchString("../../etc/passwd"))
	assert.False(t, checkpointBranchRe.MatchString("main"))
	assert.False(t, checkpointBranchRe.MatchString("fleetlift/checkpoint/"))
}

func TestExtractCostFromOutput(t *testing.T) {
	tests := []struct {
		name     string
		raw      map[string]any
		wantCost float64
	}{
		{"total_cost_usd field", map[string]any{"total_cost_usd": 0.11, "result": "done"}, 0.11},
		{"cost_usd fallback", map[string]any{"cost_usd": 0.05, "result": "done"}, 0.05},
		{"total_cost_usd takes precedence", map[string]any{"total_cost_usd": 0.11, "cost_usd": 0.05, "result": "done"}, 0.11},
		{"no cost field", map[string]any{"result": "done"}, 0.0},
		{"zero cost", map[string]any{"total_cost_usd": 0.0, "result": "done"}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCostUSD(tt.raw)
			if got != tt.wantCost {
				t.Errorf("extractCostUSD() = %v, want %v", got, tt.wantCost)
			}
		})
	}
}

func TestExtractStructuredOutput_PlainText(t *testing.T) {
	tests := []struct {
		name      string
		raw       map[string]any
		wantKey   string
		wantValue any
	}{
		{
			name: "plain text result normalized to result key",
			raw: map[string]any{
				"type":   "complete",
				"result": "The task is done",
			},
			wantKey:   "result",
			wantValue: "The task is done",
		},
		{
			name: "plain text preserves is_error",
			raw: map[string]any{
				"type":     "complete",
				"result":   "agent error occurred",
				"is_error": true,
			},
			wantKey:   "is_error",
			wantValue: true,
		},
		{
			name: "plain text preserves exit_code",
			raw: map[string]any{
				"type":      "complete",
				"result":    "non-zero exit",
				"exit_code": float64(1),
			},
			wantKey:   "exit_code",
			wantValue: float64(1),
		},
		{
			name: "plain text drops internal fields",
			raw: map[string]any{
				"type":           "complete",
				"result":         "done",
				"total_cost_usd": float64(0.05),
				"session_id":     "abc123",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractStructuredOutput(tc.raw)
			require.NotNil(t, got)
			// All plain-text cases must set "result" to the original string.
			assert.Equal(t, tc.raw["result"], got["result"])
			// Internal fields must not appear.
			assert.NotContains(t, got, "type")
			assert.NotContains(t, got, "session_id")
			assert.NotContains(t, got, "total_cost_usd")
			// If wantKey is set, verify it is preserved.
			if tc.wantKey != "" {
				assert.Equal(t, tc.wantValue, got[tc.wantKey])
			}
		})
	}
}

func TestExtractStructuredOutput_JSONInString_Unchanged(t *testing.T) {
	// Fenced JSON — existing behaviour must not regress.
	raw := map[string]any{
		"type":   "complete",
		"result": "Here is the result:\n```json\n{\"key\": \"value\"}\n```",
	}
	got := extractStructuredOutput(raw)
	assert.Equal(t, "value", got["key"])
	assert.NotContains(t, got, "type")
}

func TestExtractStructuredOutput_MapResult_Unchanged(t *testing.T) {
	// Structured map result — existing behaviour must not regress.
	raw := map[string]any{
		"type":   "complete",
		"result": map[string]any{"status": "ok"},
	}
	got := extractStructuredOutput(raw)
	assert.Equal(t, "ok", got["status"])
}

func TestExtractStructuredOutput_Nil(t *testing.T) {
	assert.Nil(t, extractStructuredOutput(nil))
}

func TestExtractStructuredOutput_PlainText_AbsentOptionalFields(t *testing.T) {
	// When raw has no is_error or exit_code, those keys must be absent from the
	// normalized output — not present with nil values (which would break type
	// assertions downstream and pollute step output stored in the DB).
	raw := map[string]any{
		"type":   "complete",
		"result": "plain output only",
	}
	got := extractStructuredOutput(raw)
	require.NotNil(t, got)
	assert.Equal(t, "plain output only", got["result"])
	_, hasIsError := got["is_error"]
	assert.False(t, hasIsError, "is_error should be absent when not in raw")
	_, hasExitCode := got["exit_code"]
	assert.False(t, hasExitCode, "exit_code should be absent when not in raw")
}
