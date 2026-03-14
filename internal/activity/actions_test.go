package activity

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCredStore struct {
	data        map[string]string
	batchCalled bool
}

func (m *mockCredStore) Get(_ context.Context, _, name string) (string, error) {
	if v, ok := m.data[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockCredStore) GetBatch(_ context.Context, _ string, names []string) (map[string]string, error) {
	m.batchCalled = true
	out := map[string]string{}
	for _, n := range names {
		if v, ok := m.data[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

type errCredStore struct{}

func (e *errCredStore) Get(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("store error")
}

func (e *errCredStore) GetBatch(_ context.Context, _ string, _ []string) (map[string]string, error) {
	return nil, fmt.Errorf("store unavailable")
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
	store := &mockCredStore{data: map[string]string{"MY_TOKEN": "secret"}}
	a := &Activities{CredStore: store}
	// slack_notify will skip (no channel) but creds are fetched first
	result, err := a.ExecuteAction(context.Background(), "", "slack_notify",
		map[string]any{"channel": "", "message": ""}, "team-1", []string{"MY_TOKEN"})
	require.NoError(t, err)
	assert.Equal(t, "skipped", result["status"])
	assert.True(t, store.batchCalled, "expected GetBatch to be called")
}

func TestExecuteAction_CredentialFetchError(t *testing.T) {
	a := &Activities{CredStore: &errCredStore{}}
	_, err := a.ExecuteAction(context.Background(), "", "slack_notify",
		map[string]any{}, "team-1", []string{"SOME_CRED"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch credentials")
}
