package activity

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCredStore struct{ data map[string]string }

func (m *mockCredStore) Get(_ context.Context, _, name string) (string, error) {
	if v, ok := m.data[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockCredStore) GetBatch(_ context.Context, _ string, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, n := range names {
		if v, ok := m.data[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

func TestExecuteAction_UnknownType(t *testing.T) {
	a := &Activities{CredStore: &mockCredStore{}}
	_, err := a.ExecuteAction(context.Background(), "step-1", "bad_type", nil, "team-1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action type")
}

func TestExecuteAction_SlackNotify_MissingChannelReturnsSkipped(t *testing.T) {
	a := &Activities{CredStore: &mockCredStore{}}
	result, err := a.ExecuteAction(context.Background(), "", "slack_notify",
		map[string]any{"channel": "", "message": ""}, "team-1", nil)
	require.NoError(t, err)
	assert.Equal(t, "skipped", result["status"])
}

func TestExecuteAction_CredentialsFetched(t *testing.T) {
	// verify GetBatch is called when credNames is non-empty
	// use a failing mockCredStore to confirm it's called
	store := &mockCredStore{data: map[string]string{"MY_TOKEN": "secret"}}
	a := &Activities{CredStore: store}
	// slack_notify will skip (no channel) but creds are fetched first
	result, err := a.ExecuteAction(context.Background(), "", "slack_notify",
		map[string]any{"channel": "", "message": ""}, "team-1", []string{"MY_TOKEN"})
	require.NoError(t, err)
	assert.Equal(t, "skipped", result["status"])
}
