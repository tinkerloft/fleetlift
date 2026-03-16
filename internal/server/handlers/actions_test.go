package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestActionsHandler_List(t *testing.T) {
	registry := model.DefaultActionRegistry()
	h := NewActionsHandler(registry)

	req := httptest.NewRequest(http.MethodGet, "/api/action-types", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Items []model.ActionContract `json:"items"`
	}
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Items, 6)

	// Verify first item has expected fields
	found := false
	for _, item := range resp.Items {
		if item.Type == "slack_notify" {
			found = true
			assert.NotEmpty(t, item.Inputs)
			assert.NotEmpty(t, item.Outputs)
			break
		}
	}
	assert.True(t, found, "expected slack_notify in response")
}
