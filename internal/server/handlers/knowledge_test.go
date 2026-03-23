package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func newTestKnowledgeHandler() (*KnowledgeHandler, *knowledge.MemoryStore) {
	store := knowledge.NewMemoryStore()
	return NewKnowledgeHandler(store), store
}

func seedKnowledgeItem(t *testing.T, store *knowledge.MemoryStore, teamID string) model.KnowledgeItem {
	t.Helper()
	item, err := store.Save(context.Background(), model.KnowledgeItem{
		TeamID:  teamID,
		Type:    model.KnowledgeTypePattern,
		Summary: "test summary",
		Source:  model.KnowledgeSourceManual,
		Status:  model.KnowledgeStatusPending,
	})
	require.NoError(t, err)
	return item
}

// TestKnowledgeList_NilClaims verifies that List returns 401 when no auth claims are present.
func TestKnowledgeList_NilClaims(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/knowledge", nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestKnowledgeList_ReturnsItems verifies that List returns 200 with the correct items.
func TestKnowledgeList_ReturnsItems(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	seedKnowledgeItem(t, store, "team-1")

	req := httptest.NewRequest(http.MethodGet, "/api/knowledge", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	items, ok := resp["items"].([]any)
	require.True(t, ok, "expected items array in response")
	assert.Len(t, items, 1)
}

// TestKnowledgeList_EmptyItems verifies that List returns 200 with a null or empty items value.
// MemoryStore returns a nil slice when no items match, which encodes as JSON null.
func TestKnowledgeList_EmptyItems(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/knowledge", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// items key must be present; when no results exist the store returns nil → JSON null.
	_, exists := resp["items"]
	assert.True(t, exists, "expected items key in response")
	// Items must be null or an empty array — not a non-empty array.
	if rawItems := resp["items"]; rawItems != nil {
		items, ok := rawItems.([]any)
		require.True(t, ok, "items must be an array when non-null")
		assert.Empty(t, items)
	}
}

// TestKnowledgeList_CrossTeamIsolation verifies that List only returns items for the requesting team.
func TestKnowledgeList_CrossTeamIsolation(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	// Seed items for two different teams.
	seedKnowledgeItem(t, store, "team-1")
	seedKnowledgeItem(t, store, "team-2")

	// Request as team-1 — should only see team-1's item.
	req := httptest.NewRequest(http.MethodGet, "/api/knowledge", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	items, ok := resp["items"].([]any)
	require.True(t, ok)
	assert.Len(t, items, 1, "team-1 should only see its own items")
}

// TestKnowledgeList_WrongTeamHeader verifies that List returns 403 when X-Team-ID is not in the JWT.
func TestKnowledgeList_WrongTeamHeader(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/knowledge", nil)
	req = claimsCtx(req, "team-1") // JWT has team-1
	req.Header.Set("X-Team-ID", "team-999") // header claims a different team
	w := httptest.NewRecorder()
	h.List(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestKnowledgeList_StatusFilter verifies that List filters by the ?status= query param.
func TestKnowledgeList_StatusFilter(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	pending := seedKnowledgeItem(t, store, "team-1") // Status: pending
	// Approve one item via the store directly.
	require.NoError(t, store.UpdateStatus(context.Background(), pending.ID, "team-1", model.KnowledgeStatusApproved))

	// Seed a second item that stays pending.
	seedKnowledgeItem(t, store, "team-1")

	req := httptest.NewRequest(http.MethodGet, "/api/knowledge?status=approved", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	items, ok := resp["items"].([]any)
	require.True(t, ok)
	assert.Len(t, items, 1, "only the approved item should be returned")
}

// TestKnowledgeUpdateStatus_NilClaims verifies that UpdateStatus returns 401 without auth.
func TestKnowledgeUpdateStatus_NilClaims(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/item-1/status",
		strings.NewReader(`{"status":"approved"}`))
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestKnowledgeUpdateStatus_InvalidStatus verifies that UpdateStatus rejects invalid status values with 400.
func TestKnowledgeUpdateStatus_InvalidStatus(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	item := seedKnowledgeItem(t, store, "team-1")

	cases := []string{"pending", "unknown", "", "APPROVED", "Approved", "null"}
	for _, status := range cases {
		t.Run("status="+status, func(t *testing.T) {
			bodyBytes, err := json.Marshal(map[string]string{"status": status})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/"+item.ID+"/status",
				strings.NewReader(string(bodyBytes)))
			req = claimsCtx(req, "team-1")
			req.Header.Set("X-Team-ID", "team-1")
			w := httptest.NewRecorder()
			r := chi.NewRouter()
			r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "status=%q", status)
		})
	}
}

// TestKnowledgeUpdateStatus_MissingStatusKey verifies that UpdateStatus returns 400 when the body
// omits the "status" key entirely (decodes as empty string, which is invalid).
func TestKnowledgeUpdateStatus_MissingStatusKey(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/item-1/status",
		strings.NewReader(`{}`))
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestKnowledgeUpdateStatus_Approved verifies that UpdateStatus accepts "approved" and mutates the store.
func TestKnowledgeUpdateStatus_Approved(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	item := seedKnowledgeItem(t, store, "team-1")

	bodyBytes, err := json.Marshal(map[string]string{"status": "approved"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/"+item.ID+"/status",
		strings.NewReader(string(bodyBytes)))
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the store was actually mutated.
	items, err := store.ListByTeam(context.Background(), "team-1", "approved")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, item.ID, items[0].ID)
}

// TestKnowledgeUpdateStatus_Rejected verifies that UpdateStatus accepts "rejected" and mutates the store.
func TestKnowledgeUpdateStatus_Rejected(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	item := seedKnowledgeItem(t, store, "team-1")

	bodyBytes, err := json.Marshal(map[string]string{"status": "rejected"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/"+item.ID+"/status",
		strings.NewReader(string(bodyBytes)))
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the store was actually mutated.
	items, err := store.ListByTeam(context.Background(), "team-1", "rejected")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, item.ID, items[0].ID)
}

// TestKnowledgeUpdateStatus_MalformedBody verifies that UpdateStatus returns 400 for invalid JSON.
func TestKnowledgeUpdateStatus_MalformedBody(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/item-1/status",
		strings.NewReader(`not-json`))
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestKnowledgeUpdateStatus_NonExistentItem verifies that UpdateStatus returns 500 when the item is not found
// (MemoryStore returns an error, which the handler converts to 500).
func TestKnowledgeUpdateStatus_NonExistentItem(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	bodyBytes, err := json.Marshal(map[string]string{"status": "approved"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/nonexistent-id/status",
		strings.NewReader(string(bodyBytes)))
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestKnowledgeUpdateStatus_CrossTeamRejected verifies that UpdateStatus cannot update another team's item.
func TestKnowledgeUpdateStatus_CrossTeamRejected(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	item := seedKnowledgeItem(t, store, "team-1") // owned by team-1

	bodyBytes, err := json.Marshal(map[string]string{"status": "approved"})
	require.NoError(t, err)
	// Authenticated as team-2 trying to update team-1's item.
	req := httptest.NewRequest(http.MethodPatch, "/api/knowledge/"+item.ID+"/status",
		strings.NewReader(string(bodyBytes)))
	req = claimsCtx(req, "team-2")
	req.Header.Set("X-Team-ID", "team-2")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/knowledge/{id}/status", h.UpdateStatus)
	r.ServeHTTP(w, req)
	// Store returns not-found for cross-team access → handler returns 500.
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Verify the item's status was NOT changed in the store.
	items, err := store.ListByTeam(context.Background(), "team-1", "approved")
	require.NoError(t, err)
	assert.Empty(t, items, "team-1 item should not be approved by team-2")
}

// TestKnowledgeDelete_NilClaims verifies that Delete returns 401 without auth.
func TestKnowledgeDelete_NilClaims(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodDelete, "/api/knowledge/item-1", nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/knowledge/{id}", h.Delete)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestKnowledgeDelete_Success verifies that Delete returns 204 and removes the item from the store.
func TestKnowledgeDelete_Success(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	item := seedKnowledgeItem(t, store, "team-1")

	req := httptest.NewRequest(http.MethodDelete, "/api/knowledge/"+item.ID, nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/knowledge/{id}", h.Delete)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the item was actually removed from the store.
	remaining, err := store.ListByTeam(context.Background(), "team-1", "")
	require.NoError(t, err)
	assert.Empty(t, remaining, "item should have been deleted")
}

// TestKnowledgeDelete_NonExistentItem verifies that Delete returns 204 even for a missing item
// (MemoryStore.Delete returns nil for missing items).
func TestKnowledgeDelete_NonExistentItem(t *testing.T) {
	h, _ := newTestKnowledgeHandler()
	req := httptest.NewRequest(http.MethodDelete, "/api/knowledge/nonexistent-id", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/knowledge/{id}", h.Delete)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// TestKnowledgeDelete_CrossTeamIsolation verifies that Delete does not remove another team's item.
func TestKnowledgeDelete_CrossTeamIsolation(t *testing.T) {
	h, store := newTestKnowledgeHandler()
	item := seedKnowledgeItem(t, store, "team-1") // owned by team-1

	// Authenticated as team-2 trying to delete team-1's item.
	req := httptest.NewRequest(http.MethodDelete, "/api/knowledge/"+item.ID, nil)
	req = claimsCtx(req, "team-2")
	req.Header.Set("X-Team-ID", "team-2")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Delete("/api/knowledge/{id}", h.Delete)
	r.ServeHTTP(w, req)
	// MemoryStore.Delete returns nil for wrong-team items (idempotent), so handler returns 204.
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify team-1's item still exists.
	remaining, err := store.ListByTeam(context.Background(), "team-1", "")
	require.NoError(t, err)
	require.Len(t, remaining, 1, "team-1 item should not be deleted by team-2")
	assert.Equal(t, item.ID, remaining[0].ID)
}
