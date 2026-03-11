package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIClient_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/workflows", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{{"slug": "test-wf"}})
	}))
	defer srv.Close()

	c := &apiClient{base: srv.URL, token: "test-token", http: srv.Client()}
	var result []map[string]string
	err := c.get("/api/workflows", &result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "test-wf", result[0]["slug"])
}

func TestAPIClient_Post(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "test-wf", body["workflow_id"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "run-123"})
	}))
	defer srv.Close()

	c := &apiClient{base: srv.URL, token: "test-token", http: srv.Client()}
	var result map[string]string
	err := c.post("/api/runs", map[string]any{"workflow_id": "test-wf"}, &result)
	require.NoError(t, err)
	assert.Equal(t, "run-123", result["id"])
}

func TestAPIClient_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := &apiClient{base: srv.URL, token: "", http: srv.Client()}
	err := c.get("/api/missing", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestSplitParam(t *testing.T) {
	tests := []struct {
		input string
		key   string
		val   string
	}{
		{"foo=bar", "foo", "bar"},
		{"key=val=ue", "key", "val=ue"},
		{"noequals", "noequals", ""},
	}
	for _, tt := range tests {
		k, v := splitParam(tt.input)
		assert.Equal(t, tt.key, k)
		assert.Equal(t, tt.val, v)
	}
}
