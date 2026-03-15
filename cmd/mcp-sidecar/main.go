// Package main implements the MCP sidecar shim binary.
// It speaks the MCP protocol locally and proxies all tool calls
// to the FleetLift backend API.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Shim proxies MCP tool calls to the FleetLift backend API.
type Shim struct {
	apiURL     string
	token      string
	httpClient *http.Client
}

// call makes an HTTP request to the backend API and returns the parsed JSON response.
func (s *Shim) call(method, path string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, s.apiURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Check status before decoding — non-JSON error bodies (e.g. 502 from proxy)
	// would produce confusing "decode response" errors if decoded first.
	if resp.StatusCode >= 400 {
		var errResult map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResult); err == nil {
			if errMsg, ok := errResult["error"]; ok {
				return nil, fmt.Errorf("api error (%d): %v", resp.StatusCode, errMsg)
			}
		}
		return nil, fmt.Errorf("api error (%d)", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result, nil
}

// resultJSON marshals a map to JSON text and returns it as an MCP tool result.
func resultJSON(data map[string]any) *mcp.CallToolResult {
	b, err := json.Marshal(data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err))
	}
	return mcp.NewToolResultText(string(b))
}

// registerTools registers all 7 MCP tools on the given server.
func (s *Shim) registerTools(srv *server.MCPServer) {
	// 1. context.get_run
	srv.AddTool(mcp.NewTool("context.get_run",
		mcp.WithDescription("Get the current run context including run ID, workflow, parameters, and step statuses"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := s.call("GET", "/api/mcp/run", nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 2. context.get_step_output
	srv.AddTool(mcp.NewTool("context.get_step_output",
		mcp.WithDescription("Get the output from a completed step in the current workflow run"),
		mcp.WithString("step_id", mcp.Required(), mcp.Description("The step ID to get output for")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stepID, err := req.RequireString("step_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result, err := s.call("GET", "/api/mcp/steps/"+url.PathEscape(stepID)+"/output", nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 3. context.get_knowledge
	srv.AddTool(mcp.NewTool("context.get_knowledge",
		mcp.WithDescription("Search the team knowledge base for relevant context"),
		mcp.WithString("query", mcp.Description("Search query")),
		mcp.WithNumber("max_items", mcp.Description("Maximum number of items to return")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		if q := req.GetString("query", ""); q != "" {
			params.Set("q", q)
		}
		if max := req.GetFloat("max_items", 0); max > 0 {
			params.Set("max", fmt.Sprintf("%d", int(max)))
		}
		path := "/api/mcp/knowledge"
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}
		result, err := s.call("GET", path, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 4. artifact.create
	srv.AddTool(mcp.NewTool("artifact.create",
		mcp.WithDescription("Create an artifact (file, report, etc.) from this step"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Artifact name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Artifact content")),
		mcp.WithString("content_type", mcp.Description("MIME type of the content")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body := map[string]any{
			"name":    name,
			"content": content,
		}
		if ct := req.GetString("content_type", ""); ct != "" {
			body["content_type"] = ct
		}
		result, err := s.call("POST", "/api/mcp/artifacts", body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 5. memory.add_learning
	srv.AddTool(mcp.NewTool("memory.add_learning",
		mcp.WithDescription("Record a learning or insight discovered during this step"),
		mcp.WithString("type", mcp.Required(), mcp.Description("Type of learning (e.g. pattern, convention, pitfall)")),
		mcp.WithString("summary", mcp.Required(), mcp.Description("Short summary of the learning")),
		mcp.WithString("details", mcp.Description("Detailed explanation")),
		mcp.WithNumber("confidence", mcp.Description("Confidence score 0-1")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		typ, err := req.RequireString("type")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		summary, err := req.RequireString("summary")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body := map[string]any{
			"type":    typ,
			"summary": summary,
		}
		if d := req.GetString("details", ""); d != "" {
			body["details"] = d
		}
		if c := req.GetFloat("confidence", -1); c >= 0 {
			body["confidence"] = c
		}
		if t := req.GetString("tags", ""); t != "" {
			body["tags"] = strings.Split(t, ",")
		}
		result, err := s.call("POST", "/api/mcp/knowledge", body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 6. memory.search
	srv.AddTool(mcp.NewTool("memory.search",
		mcp.WithDescription("Search team knowledge and learnings"),
		mcp.WithString("query", mcp.Description("Search query")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags to filter by")),
		mcp.WithNumber("max_items", mcp.Description("Maximum number of items to return")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		if q := req.GetString("query", ""); q != "" {
			params.Set("q", q)
		}
		if t := req.GetString("tags", ""); t != "" {
			params.Set("tags", t)
		}
		if max := req.GetFloat("max_items", 0); max > 0 {
			params.Set("max", fmt.Sprintf("%d", int(max)))
		}
		path := "/api/mcp/knowledge/search"
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}
		result, err := s.call("GET", path, nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 7. progress.update
	srv.AddTool(mcp.NewTool("progress.update",
		mcp.WithDescription("Report progress for the current step"),
		mcp.WithNumber("percentage", mcp.Required(), mcp.Description("Progress percentage 0-100")),
		mcp.WithString("message", mcp.Description("Progress message")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pct, err := req.RequireFloat("percentage")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body := map[string]any{
			"percentage": pct,
		}
		if m := req.GetString("message", ""); m != "" {
			body["message"] = m
		}
		result, err := s.call("POST", "/api/mcp/progress", body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})
}

func main() {
	apiURL := flag.String("api-url", "http://localhost:8080", "FleetLift backend API URL")
	port := flag.Int("port", 8081, "SSE server port")
	transport := flag.String("transport", "sse", "Transport type: sse or stdio")
	flag.Parse()

	// Read token from environment variable to avoid exposing it in /proc/cmdline.
	token := os.Getenv("FLEETLIFT_MCP_TOKEN")
	if token == "" {
		log.Fatal("FLEETLIFT_MCP_TOKEN environment variable is required")
	}

	// Trim trailing slash from API URL
	*apiURL = strings.TrimRight(*apiURL, "/")

	shim := &Shim{
		apiURL:     *apiURL,
		token:      token,
		httpClient: http.DefaultClient,
	}

	srv := server.NewMCPServer("fleetlift-mcp-sidecar", "1.0.0")
	shim.registerTools(srv)

	switch *transport {
	case "stdio":
		if err := server.ServeStdio(srv); err != nil {
			log.Fatalf("stdio error: %v", err)
		}
	default: // sse
		sseServer := server.NewSSEServer(srv, server.WithBaseURL(fmt.Sprintf("http://localhost:%d", *port)))

		// Wrap with /health endpoint
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		mux.Handle("/", sseServer)

		log.Printf("MCP SSE server listening on :%d", *port)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux); err != nil {
			log.Fatalf("SSE server error: %v", err)
		}
	}
}
