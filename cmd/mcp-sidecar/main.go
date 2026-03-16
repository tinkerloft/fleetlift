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

// registerTools registers all 9 MCP tools on the given server.
func (s *Shim) registerTools(srv *server.MCPServer) {
	// 1. context.get_run
	srv.AddTool(mcp.NewTool("context.get_run",
		mcp.WithDescription("Get the current run context. Returns: run_id, workflow name, parameters, current running step, and all steps with their statuses (pending/running/complete/failed). Use this at the start of your work to understand the workflow you are executing and what parameters were provided."),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := s.call("GET", "/api/mcp/run", nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 2. context.get_step_output
	srv.AddTool(mcp.NewTool("context.get_step_output",
		mcp.WithDescription("Get the output and diff from a previously completed step in this workflow run. Use this to access results from upstream steps that your step depends on. Returns the step's stdout/stderr output and any git diff it produced. Only works for steps with status 'complete'."),
		mcp.WithString("step_id", mcp.Required(), mcp.Description("The step ID (from the workflow template, e.g. 'clone_repo' or 'run_tests') — use context.get_run to see available step IDs and their statuses")),
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
		mcp.WithDescription("Get approved knowledge items for the current workflow. Returns items previously captured and approved by the team, scoped to the workflow template this run is based on. Use this to check for known patterns, conventions, or gotchas before making changes."),
		mcp.WithString("query", mcp.Description("Optional text to filter results by summary or details content")),
		mcp.WithNumber("max_items", mcp.Description("Maximum items to return (default 10, max 100)")),
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
		mcp.WithDescription("Create a persistent artifact (file, report, analysis, etc.) from this step. The artifact is stored in the platform and visible in the run results UI. Content must be UTF-8 text, max 1MB. Use this for deliverables like reports, diffs, configs — anything the user should see as a discrete output."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Artifact filename (e.g. 'analysis-report.md', 'config.yaml'). Must not contain '..'")),
		mcp.WithString("content", mcp.Required(), mcp.Description("UTF-8 text content of the artifact, max 1MB")),
		mcp.WithString("content_type", mcp.Description("MIME type (default: text/plain). Use text/markdown for reports, application/json for structured data")),
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
		mcp.WithDescription("Record a learning or insight discovered during this step. Creates a knowledge item with status 'pending' — it will be reviewed by the team before becoming visible to future runs via context.get_knowledge. Use this to capture patterns, conventions, pitfalls, or important context you discover while working."),
		mcp.WithString("type", mcp.Required(), mcp.Description("One of: pattern (recurring code pattern), correction (a mistake to avoid), gotcha (surprising behavior), context (background information)")),
		mcp.WithString("summary", mcp.Required(), mcp.Description("One-line summary of the learning (shown in search results)")),
		mcp.WithString("details", mcp.Description("Detailed explanation with examples, code snippets, or references")),
		mcp.WithNumber("confidence", mcp.Description("How confident you are in this learning, 0.0 to 1.0 (default: unset)")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags for categorization (e.g. 'go,error-handling,testing')")),
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
		mcp.WithDescription("Search all approved knowledge items across the team, not limited to the current workflow. Use this to find learnings from other workflows that might be relevant to your current task."),
		mcp.WithString("query", mcp.Description("Search text matched against summary and details")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags to filter by (e.g. 'go,testing')")),
		mcp.WithNumber("max_items", mcp.Description("Maximum items to return (default 10, max 100)")),
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
		mcp.WithDescription("Report progress on the current step. Updates the percentage and message shown in the run UI. Call this periodically during long-running steps to keep the user informed. The percentage should reflect meaningful progress milestones, not arbitrary increments."),
		mcp.WithNumber("percentage", mcp.Required(), mcp.Description("Progress percentage, integer 0-100. Use 0 at start, 100 when complete.")),
		mcp.WithString("message", mcp.Description("Human-readable progress message (e.g. 'Cloning repository', 'Running 15/30 tests')")),
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

	// 8. inbox.notify
	srv.AddTool(mcp.NewTool("inbox.notify",
		mcp.WithDescription("Send a notification to the team inbox without blocking execution."),
		mcp.WithString("title", mcp.Required()),
		mcp.WithString("summary", mcp.Required()),
		mcp.WithString("urgency", mcp.Description("low | normal | high; default: normal")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		summary, err := req.RequireString("summary")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body := map[string]any{
			"title":   title,
			"summary": summary,
		}
		if u := req.GetString("urgency", ""); u != "" {
			body["urgency"] = u
		}
		result, err := s.call("POST", "/api/mcp/inbox/notify", body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result), nil
	})

	// 9. inbox.request_input
	srv.AddTool(mcp.NewTool("inbox.request_input",
		mcp.WithDescription(
			"Request human input. This ends the current step. "+
				"A continuation step will run in a fresh sandbox with the human's answer once they respond. "+
				"IMPORTANT: Call this as your FINAL action before exiting. Do not continue work after this call.",
		),
		mcp.WithString("question", mcp.Required()),
		mcp.WithString("state_summary", mcp.Description("What you've done so far")),
		mcp.WithString("urgency", mcp.Description("low | normal | high; default: normal")),
		mcp.WithString("checkpoint_branch", mcp.Description("Git branch with committed working state")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		question, err := req.RequireString("question")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body := map[string]any{
			"question": question,
		}
		if ss := req.GetString("state_summary", ""); ss != "" {
			body["state_summary"] = ss
		}
		if u := req.GetString("urgency", ""); u != "" {
			body["urgency"] = u
		}
		if cb := req.GetString("checkpoint_branch", ""); cb != "" {
			body["checkpoint_branch"] = cb
		}
		result, err := s.call("POST", "/api/mcp/inbox/request_input", body)
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
