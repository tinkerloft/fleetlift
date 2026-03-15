package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShim_CallProxiesToBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/mcp/run", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"run_id": "run-1", "workflow": "bug-fix"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	result, err := shim.call("GET", "/api/mcp/run", nil)
	require.NoError(t, err)
	assert.Equal(t, "run-1", result["run_id"])
}

func TestShim_CallForwardsErrorResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "run is terminated"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	_, err := shim.call("GET", "/api/mcp/run", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "run is terminated")
}

func TestShim_CallWithBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "test-artifact", body["name"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"artifact_id": "art-1"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	result, err := shim.call("POST", "/api/mcp/artifacts", map[string]any{"name": "test-artifact"})
	require.NoError(t, err)
	assert.Equal(t, "art-1", result["artifact_id"])
}

func TestShim_CallErrorWithoutErrorField(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "something broke"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	_, err := shim.call("GET", "/api/mcp/run", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api error (500)")
}

func TestShim_RegisterTools(t *testing.T) {
	srv := server.NewMCPServer("test", "0.1.0")
	shim := &Shim{apiURL: "http://localhost:9999", token: "tok", httpClient: http.DefaultClient}
	shim.registerTools(srv)
	// Verify all 7 tools are registered by initializing and listing tools
	// We can't easily list tools from MCPServer, but we verify no panic and build succeeds
}

func TestShim_GetRunToolHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/mcp/run", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"run_id":   "run-123",
			"workflow": "code-review",
			"status":   "running",
		})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	// Invoke the tool via the server's HandleMessage
	result := callTool(t, srv, "context.get_run", nil)
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestShim_GetStepOutputToolHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/mcp/steps/step-42/output", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"output": "some result"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	result := callTool(t, srv, "context.get_step_output", map[string]any{"step_id": "step-42"})
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestShim_ArtifactCreateToolHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/mcp/artifacts", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "report.md", body["name"])
		assert.Equal(t, "# Report", body["content"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"artifact_id": "art-99"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	result := callTool(t, srv, "artifact.create", map[string]any{
		"name":    "report.md",
		"content": "# Report",
	})
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestShim_ProgressUpdateToolHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/mcp/progress", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, float64(50), body["percentage"])
		assert.Equal(t, "halfway done", body["message"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	result := callTool(t, srv, "progress.update", map[string]any{
		"percentage": float64(50),
		"message":    "halfway done",
	})
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestShim_MemoryAddLearningToolHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/mcp/knowledge", r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "pattern", body["type"])
		assert.Equal(t, "always check nil", body["summary"])
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "k-1"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	result := callTool(t, srv, "memory.add_learning", map[string]any{
		"type":    "pattern",
		"summary": "always check nil",
	})
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestShim_MemorySearchToolHandler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/mcp/knowledge/search", r.URL.Path)
		assert.Equal(t, "nil checks", r.URL.Query().Get("q"))
		assert.Equal(t, "go,patterns", r.URL.Query().Get("tags"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	result := callTool(t, srv, "memory.search", map[string]any{
		"query": "nil checks",
		"tags":  "go,patterns",
	})
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestShim_ToolReturnsErrorOnBackendFailure(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "run not found"})
	}))
	defer backend.Close()

	shim := &Shim{apiURL: backend.URL, token: "test-token", httpClient: http.DefaultClient}
	srv := server.NewMCPServer("test", "0.1.0")
	shim.registerTools(srv)

	result := callTool(t, srv, "context.get_run", nil)
	assert.NotNil(t, result)
	assert.True(t, result.IsError)
}

// callTool invokes a tool on the MCPServer by constructing an MCP JSON-RPC request.
func callTool(t *testing.T, srv *server.MCPServer, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	if args == nil {
		args = map[string]any{}
	}

	// First, initialize the server (required before tool calls)
	initMsg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"clientInfo":      map[string]any{"name": "test", "version": "0.1"},
		},
	})
	require.NoError(t, err)

	ctx := context.Background()
	srv.HandleMessage(ctx, json.RawMessage(initMsg))

	// Build the tools/call request as raw JSON
	reqMap := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	reqBytes, err := json.Marshal(reqMap)
	require.NoError(t, err)

	respMsg := srv.HandleMessage(ctx, json.RawMessage(reqBytes))

	respJSON, err := json.Marshal(respMsg)
	require.NoError(t, err)

	var rpcResp struct {
		Result *mcp.CallToolResult `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	err = json.Unmarshal(respJSON, &rpcResp)
	require.NoError(t, err)

	if rpcResp.Error != nil {
		t.Fatalf("JSON-RPC error: %d %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	require.NotNil(t, rpcResp.Result)
	return rpcResp.Result
}
