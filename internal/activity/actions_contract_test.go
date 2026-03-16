package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// TestActionContract_HandlersExist verifies every registered action type has a handler.
func TestActionContract_HandlersExist(t *testing.T) {
	registry := model.DefaultActionRegistry()
	a := &Activities{CredStore: &mockCredStore{data: map[string]string{
		"GITHUB_TOKEN":    "test-token",
		"SLACK_BOT_TOKEN": "test-token",
	}}}
	for _, actionType := range registry.Types() {
		t.Run(actionType, func(t *testing.T) {
			// ExecuteAction should not return "unknown action type" error
			_, err := a.ExecuteAction(context.Background(), "", actionType, map[string]any{}, "team-1", nil)
			if err != nil {
				assert.NotContains(t, err.Error(), "unknown action type",
					"action type %q is in registry but has no handler", actionType)
			}
		})
	}
}

// TestActionContract_MissingRequiredInputReturnsError verifies handlers reject missing required config.
func TestActionContract_MissingRequiredInputReturnsError(t *testing.T) {
	registry := model.DefaultActionRegistry()
	a := &Activities{CredStore: &mockCredStore{data: map[string]string{
		"GITHUB_TOKEN":    "test-token",
		"SLACK_BOT_TOKEN": "test-token",
	}}}

	for _, actionType := range registry.Types() {
		contract, _ := registry.Get(actionType)
		if actionType == "create_pr" {
			continue // create_pr is a passthrough, always returns skipped
		}
		t.Run(actionType+"_empty_config", func(t *testing.T) {
			_, err := a.ExecuteAction(context.Background(), "", actionType, map[string]any{}, "team-1", nil)
			// With empty config, actions with required inputs should error
			hasRequired := false
			for _, f := range contract.Inputs {
				if f.Required {
					hasRequired = true
					break
				}
			}
			if hasRequired {
				assert.Error(t, err, "action %q should error on empty config (has required inputs)", actionType)
			}
		})
	}
}

// TestActionContract_OutputKeysMatchContract verifies that when handlers return non-nil
// output, the keys are a subset of declared output fields.
func TestActionContract_OutputKeysMatchContract(t *testing.T) {
	registry := model.DefaultActionRegistry()

	// Test cases with valid config that produce non-nil output without external calls
	// Only actions that don't call activity.GetLogger (requires Temporal context)
	testCases := map[string]map[string]any{
		"create_pr": {"title": "test"},
	}

	a := &Activities{CredStore: &mockCredStore{data: map[string]string{}}}

	for actionType, config := range testCases {
		contract, _ := registry.Get(actionType)
		t.Run(actionType, func(t *testing.T) {
			result, err := a.ExecuteAction(context.Background(), "", actionType, config, "team-1", nil)
			if err != nil || result == nil {
				return // can't validate output if error or nil
			}
			// Every key in result should be in contract outputs
			for key := range result {
				require.True(t, contract.HasOutputField(key),
					"action %q returned unexpected output key %q; declared outputs: %s",
					actionType, key, contract.OutputFieldNames())
			}
		})
	}
}
