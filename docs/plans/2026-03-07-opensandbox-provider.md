# OpenSandbox Provider Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace fleetlift's docker and k8s sandbox providers with a single OpenSandbox provider. OpenSandbox already wraps Docker and Kubernetes as its own runtime backends — maintaining parallel implementations in fleetlift is dead code.

**Architecture:** Implement `internal/sandbox/opensandbox/` with a lifecycle HTTP client, an execd HTTP client, and a `Provider` that satisfies `sandbox.AgentProvider`. Wire it into the worker. Then delete `internal/sandbox/docker/` and `internal/sandbox/k8s/` entirely, and simplify `factory.go` to remove the routing abstraction that only existed to support multiple providers.

**Tech Stack:** Go stdlib `net/http`, `bufio` (SSE), `encoding/json`, `sync`, `net/http/httptest` (tests). No new dependencies.

---

## Pre-work: Verify OpenSandbox API Contracts

**Before writing any code**, check `components/execd/` in the OpenSandbox source at https://github.com/alibaba/OpenSandbox. Verify:

1. `POST /command` — exact request body fields and SSE event format (`type`, `data`, `exitCode` field names)
2. `POST /files/replace` — body shape (JSON with string content? base64? multipart?)
3. `GET /files/download` — query param name for path
4. `POST /v1/sandboxes` response — does it include `accessToken` for execd auth?
5. State machine values — exact strings for sandbox states (e.g. `"Running"` vs `"running"`)

Adjust the types in Tasks 1 and 2 if reality differs from the plan's assumptions.

---

## Task 1: Lifecycle API Client

**Files:**
- Create: `internal/sandbox/opensandbox/lifecycle.go`
- Create: `internal/sandbox/opensandbox/lifecycle_test.go`

### Step 1: Write the failing tests

```go
// internal/sandbox/opensandbox/lifecycle_test.go
package opensandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLifecycleClient_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sandboxes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("OPEN-SANDBOX-API-KEY") != "test-key" {
			t.Errorf("missing or wrong API key header")
		}
		var req CreateSandboxRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Image.URI != "myimage:latest" {
			t.Errorf("unexpected image: %s", req.Image.URI)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SandboxResponse{
			ID:          "sb-123",
			State:       "Running",
			AccessToken: "tok-abc",
		})
	}))
	defer srv.Close()

	c := NewLifecycleClient(srv.URL, "test-key")
	resp, err := c.Create(context.Background(), CreateSandboxRequest{
		Image:   SandboxImage{URI: "myimage:latest"},
		Timeout: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "sb-123" {
		t.Errorf("got ID %q, want sb-123", resp.ID)
	}
	if resp.AccessToken != "tok-abc" {
		t.Errorf("got AccessToken %q, want tok-abc", resp.AccessToken)
	}
}

func TestLifecycleClient_GetEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sandboxes/sb-123/endpoints/44772" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(EndpointResponse{URL: "http://proxy:8080/sb-123/44772"})
	}))
	defer srv.Close()

	c := NewLifecycleClient(srv.URL, "test-key")
	url, err := c.GetEndpoint(context.Background(), "sb-123", 44772, true)
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://proxy:8080/sb-123/44772" {
		t.Errorf("got %q", url)
	}
}

func TestLifecycleClient_Delete(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/v1/sandboxes/sb-123" {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := NewLifecycleClient(srv.URL, "test-key")
	if err := c.Delete(context.Background(), "sb-123"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("DELETE was not called")
	}
}

func TestLifecycleClient_Get_StateMapping(t *testing.T) {
	cases := []struct {
		state     string
		wantPhase string
	}{
		{"Pending", "pending"},
		{"Running", "running"},
		{"Terminated", "succeeded"},
		{"Failed", "failed"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(SandboxResponse{ID: "sb-1", State: tc.state})
		}))
		c := NewLifecycleClient(srv.URL, "test-key")
		resp, err := c.Get(context.Background(), "sb-1")
		if err != nil {
			t.Fatalf("state %q: %v", tc.state, err)
		}
		got := resp.SandboxPhase()
		if string(got) != tc.wantPhase {
			t.Errorf("state %q: got phase %q, want %q", tc.state, got, tc.wantPhase)
		}
		srv.Close()
	}
}
```

### Step 2: Run to confirm failure

```bash
cd /Users/andrew/dev/projects/fleetlift/.worktrees/feat/opensandbox-provider
go test ./internal/sandbox/opensandbox/... 2>&1
```
Expected: compile error — package does not exist yet.

### Step 3: Implement

```go
// internal/sandbox/opensandbox/lifecycle.go
package opensandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

const execdPort = 44772

// LifecycleClient calls the OpenSandbox Lifecycle API.
type LifecycleClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewLifecycleClient creates a LifecycleClient.
func NewLifecycleClient(baseURL, apiKey string) *LifecycleClient {
	return &LifecycleClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SandboxImage specifies the container image.
type SandboxImage struct {
	URI string `json:"uri"`
}

// ResourceLimits maps to OpenSandbox resource limit strings.
type ResourceLimits struct {
	Memory string `json:"memory,omitempty"` // e.g. "4Gi"
	CPU    string `json:"cpu,omitempty"`    // e.g. "2"
}

// CreateSandboxRequest is sent to POST /v1/sandboxes.
type CreateSandboxRequest struct {
	Image          SandboxImage      `json:"image"`
	Entrypoint     []string          `json:"entrypoint,omitempty"`
	Timeout        int               `json:"timeout"` // seconds, 60–86400
	Env            map[string]string `json:"env,omitempty"`
	ResourceLimits *ResourceLimits   `json:"resourceLimits,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// SandboxResponse is returned by GET/POST /v1/sandboxes.
// NOTE: verify exact field names against OpenSandbox source.
type SandboxResponse struct {
	ID          string `json:"id"`
	State       string `json:"state"` // Pending, Running, Terminated, Failed
	AccessToken string `json:"accessToken,omitempty"`
}

// SandboxPhase converts OpenSandbox state to our internal SandboxPhase.
func (r *SandboxResponse) SandboxPhase() sandbox.SandboxPhase {
	switch r.State {
	case "Pending", "Pausing", "Paused":
		return sandbox.SandboxPhasePending
	case "Running":
		return sandbox.SandboxPhaseRunning
	case "Stopping", "Terminated":
		return sandbox.SandboxPhaseSucceeded
	case "Failed":
		return sandbox.SandboxPhaseFailed
	default:
		return sandbox.SandboxPhaseUnknown
	}
}

// EndpointResponse is returned by GET /v1/sandboxes/{id}/endpoints/{port}.
type EndpointResponse struct {
	URL string `json:"url"`
}

func (c *LifecycleClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("OPEN-SANDBOX-API-KEY", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// Create creates a new sandbox and returns its details including the execd access token.
func (c *LifecycleClient) Create(ctx context.Context, req CreateSandboxRequest) (*SandboxResponse, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/sandboxes", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create sandbox: HTTP %d", resp.StatusCode)
	}
	var out SandboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}
	return &out, nil
}

// Get returns the current state of a sandbox.
func (c *LifecycleClient) Get(ctx context.Context, id string) (*SandboxResponse, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/sandboxes/"+id, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get sandbox %s: HTTP %d", id, resp.StatusCode)
	}
	var out SandboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode get response: %w", err)
	}
	return &out, nil
}

// Delete terminates and removes a sandbox.
func (c *LifecycleClient) Delete(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/v1/sandboxes/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete sandbox %s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

// RenewExpiration extends the sandbox TTL.
func (c *LifecycleClient) RenewExpiration(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "/v1/sandboxes/"+id+"/renew-expiration", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("renew sandbox %s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

// GetEndpoint returns the URL for a given port on the sandbox.
// When useServerProxy is true, routes through the lifecycle server.
func (c *LifecycleClient) GetEndpoint(ctx context.Context, id string, port int, useServerProxy bool) (string, error) {
	path := fmt.Sprintf("/v1/sandboxes/%s/endpoints/%d", id, port)
	if useServerProxy {
		path += "?use_server_proxy=true"
	}
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("get endpoint %s:%d: HTTP %d", id, port, resp.StatusCode)
	}
	var out EndpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode endpoint response: %w", err)
	}
	return out.URL, nil
}
```

### Step 4: Run tests

```bash
go test ./internal/sandbox/opensandbox/... -run TestLifecycleClient -v
```
Expected: all 4 lifecycle tests pass.

### Step 5: Commit

```bash
git add internal/sandbox/opensandbox/lifecycle.go internal/sandbox/opensandbox/lifecycle_test.go
git commit -m "feat(sandbox/opensandbox): add lifecycle API client"
```

---

## Task 2: execd API Client

The execd daemon runs inside each sandbox on port 44772. It handles command execution (SSE-streamed output) and file I/O.

> **⚠️ Verify first:** Check `components/execd/` in the OpenSandbox repo for the exact SSE event format and `files/replace` body shape before implementing. Update `ExecdCommandEvent` and `fileReplaceRequest` if needed.

**Files:**
- Create: `internal/sandbox/opensandbox/execd.go`
- Create: `internal/sandbox/opensandbox/execd_test.go`

### Step 1: Write the failing tests

```go
// internal/sandbox/opensandbox/execd_test.go
package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestExecdClient_RunCommand_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/command" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("AccessToken") != "tok-abc" {
			t.Errorf("missing access token header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(ExecdCommandEvent{Type: "stdout", Data: "hello\n"}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(ExecdCommandEvent{Type: "stderr", Data: "warn\n"}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(ExecdCommandEvent{Type: "exit", ExitCode: 0}))
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	result, err := c.RunCommand(context.Background(), "bash", []string{"-c", "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("stdout %q", result.Stdout)
	}
	if result.Stderr != "warn\n" {
		t.Errorf("stderr %q", result.Stderr)
	}
}

func TestExecdClient_RunCommand_NonZeroExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(ExecdCommandEvent{Type: "exit", ExitCode: 2}))
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	result, err := c.RunCommand(context.Background(), "bash", []string{"-c", "exit 2"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 2 {
		t.Errorf("want exit code 2, got %d", result.ExitCode)
	}
}

func TestExecdClient_WriteFile(t *testing.T) {
	var gotPath, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/files/replace" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		var req fileReplaceRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotPath = req.Path
		gotContent = req.Content
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	if err := c.WriteFile(context.Background(), "/workspace/.fleetlift/manifest.json", []byte(`{"task_id":"t1"}`)); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/workspace/.fleetlift/manifest.json" {
		t.Errorf("path %q", gotPath)
	}
	if gotContent != `{"task_id":"t1"}` {
		t.Errorf("content %q", gotContent)
	}
}

func TestExecdClient_ReadFile_Exists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/download" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("path") != "/workspace/.fleetlift/status.json" {
			t.Errorf("wrong path query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"phase":"executing"}`))
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	data, err := c.ReadFile(context.Background(), "/workspace/.fleetlift/status.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"phase":"executing"}` {
		t.Errorf("data %q", data)
	}
}

func TestExecdClient_ReadFile_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	data, err := c.ReadFile(context.Background(), "/no/such/file")
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		t.Errorf("expected nil for missing file, got %q", data)
	}
}

func TestExecdClient_MakeDir(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/directories" {
			called = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	if err := c.MakeDir(context.Background(), "/workspace/.fleetlift"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("POST /directories not called")
	}
}
```

### Step 2: Run to confirm failure

```bash
go test ./internal/sandbox/opensandbox/... -run TestExecdClient -v 2>&1
```
Expected: compile error.

### Step 3: Implement

```go
// internal/sandbox/opensandbox/execd.go
package opensandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxFileSize = 10 << 20 // 10 MB

// ExecdClient calls the execd daemon API inside an OpenSandbox sandbox.
type ExecdClient struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

// NewExecdClient creates an ExecdClient.
func NewExecdClient(baseURL, accessToken string) *ExecdClient {
	return &ExecdClient{
		baseURL:     baseURL,
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: 10 * time.Minute},
	}
}

// ExecdCommandRequest is the body for POST /command.
// NOTE: verify exact field names against OpenSandbox execd source.
type ExecdCommandRequest struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args,omitempty"`
}

// ExecdCommandEvent is a single SSE event from POST /command.
// NOTE: verify Type values against OpenSandbox execd source.
type ExecdCommandEvent struct {
	Type     string `json:"type"`               // "stdout", "stderr", "exit"
	Data     string `json:"data,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"` // present when Type == "exit"
}

type fileReplaceRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"` // raw string — verify against source (may need base64)
}

type makeDirRequest struct {
	Path string `json:"path"`
}

// CommandResult holds aggregated command output.
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func (c *ExecdClient) doJSON(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("AccessToken", c.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// RunCommand executes a command and returns aggregated stdout/stderr/exit code.
// execd streams output as Server-Sent Events.
func (c *ExecdClient) RunCommand(ctx context.Context, cmd string, args []string) (*CommandResult, error) {
	resp, err := c.doJSON(ctx, http.MethodPost, "/command", ExecdCommandRequest{Cmd: cmd, Args: args})
	if err != nil {
		return nil, fmt.Errorf("execd command: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("execd command: HTTP %d", resp.StatusCode)
	}

	result := &CommandResult{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(payload) == "" {
			continue
		}
		var event ExecdCommandEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue // skip malformed events
		}
		switch event.Type {
		case "stdout":
			result.Stdout += event.Data
		case "stderr":
			result.Stderr += event.Data
		case "exit":
			result.ExitCode = event.ExitCode
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading command SSE: %w", err)
	}
	return result, nil
}

// WriteFile writes content to a path inside the sandbox.
func (c *ExecdClient) WriteFile(ctx context.Context, path string, content []byte) error {
	resp, err := c.doJSON(ctx, http.MethodPost, "/files/replace", fileReplaceRequest{
		Path:    path,
		Content: string(content),
	})
	if err != nil {
		return fmt.Errorf("execd write %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("execd write %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// ReadFile reads a file from the sandbox. Returns nil, nil if the file does not exist.
func (c *ExecdClient) ReadFile(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/files/download", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("AccessToken", c.accessToken)
	q := req.URL.Query()
	q.Set("path", path)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execd read %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("execd read %s: HTTP %d", path, resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("execd read %s body: %w", path, err)
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("execd read %s: exceeds 10 MB limit", path)
	}
	return data, nil
}

// MakeDir creates a directory inside the sandbox.
func (c *ExecdClient) MakeDir(ctx context.Context, path string) error {
	resp, err := c.doJSON(ctx, http.MethodPost, "/directories", makeDirRequest{Path: path})
	if err != nil {
		return fmt.Errorf("execd mkdir %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("execd mkdir %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}
```

### Step 4: Run tests

```bash
go test ./internal/sandbox/opensandbox/... -run TestExecdClient -v
```
Expected: all 5 execd tests pass.

### Step 5: Commit

```bash
git add internal/sandbox/opensandbox/execd.go internal/sandbox/opensandbox/execd_test.go
git commit -m "feat(sandbox/opensandbox): add execd API client"
```

---

## Task 3: Provider Implementation

**Files:**
- Create: `internal/sandbox/opensandbox/provider.go`
- Create: `internal/sandbox/opensandbox/provider_test.go`

### Step 1: Write the failing tests

```go
// internal/sandbox/opensandbox/provider_test.go
package opensandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// newTestServers returns wired lifecycle + execd test servers and a provider pointing at them.
// The caller must call close() when done.
func newTestServers(t *testing.T) (*Provider, func()) {
	t.Helper()

	execdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/directories":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/files/replace":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	lifecycleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			json.NewEncoder(w).Encode(SandboxResponse{ID: "sb-test", State: "Running", AccessToken: "tok-test"})
		case r.URL.Path == "/v1/sandboxes/sb-test/endpoints/44772":
			json.NewEncoder(w).Encode(EndpointResponse{URL: execdSrv.URL})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes/sb-test":
			json.NewEncoder(w).Encode(SandboxResponse{ID: "sb-test", State: "Running"})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sandboxes/sb-test":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	p := NewProvider(Config{
		Domain:         lifecycleSrv.URL,
		APIKey:         "test-key",
		UseServerProxy: false,
	})
	return p, func() {
		lifecycleSrv.Close()
		execdSrv.Close()
	}
}

func TestProvider_Name(t *testing.T) {
	p, close := newTestServers(t)
	defer close()
	if p.Name() != "opensandbox" {
		t.Errorf("Name() = %q, want opensandbox", p.Name())
	}
}

func TestProvider_Provision(t *testing.T) {
	p, close := newTestServers(t)
	defer close()

	sb, err := p.Provision(context.Background(), sandbox.ProvisionOptions{
		TaskID: "task-1",
		Image:  "myimage:latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sb.ID != "sb-test" {
		t.Errorf("ID = %q, want sb-test", sb.ID)
	}
	if sb.Provider != "opensandbox" {
		t.Errorf("Provider = %q", sb.Provider)
	}
}

func TestProvider_Status(t *testing.T) {
	p, close := newTestServers(t)
	defer close()

	_, err := p.Provision(context.Background(), sandbox.ProvisionOptions{TaskID: "t1", Image: "img:1"})
	if err != nil {
		t.Fatal(err)
	}
	status, err := p.Status(context.Background(), "sb-test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != sandbox.SandboxPhaseRunning {
		t.Errorf("phase %q, want running", status.Phase)
	}
}

func TestProvider_Cleanup(t *testing.T) {
	p, close := newTestServers(t)
	defer close()

	_, err := p.Provision(context.Background(), sandbox.ProvisionOptions{TaskID: "t1", Image: "img:1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Cleanup(context.Background(), "sb-test"); err != nil {
		t.Fatal(err)
	}
}

func TestProvider_AgentProtocol(t *testing.T) {
	var writtenFiles = map[string]string{}

	execdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/directories":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/files/replace":
			var req fileReplaceRequest
			json.NewDecoder(r.Body).Decode(&req)
			writtenFiles[req.Path] = req.Content
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			path := r.URL.Query().Get("path")
			switch path {
			case "/workspace/.fleetlift/status.json":
				w.Write([]byte(`{"phase":"executing"}`))
			case "/workspace/.fleetlift/result.json":
				w.Write([]byte(`{"status":"complete"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer execdSrv.Close()

	lifecycleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			json.NewEncoder(w).Encode(SandboxResponse{ID: "sb-x", State: "Running", AccessToken: "tok-x"})
		case r.URL.Path == "/v1/sandboxes/sb-x/endpoints/44772":
			json.NewEncoder(w).Encode(EndpointResponse{URL: execdSrv.URL})
		}
	}))
	defer lifecycleSrv.Close()

	p := NewProvider(Config{Domain: lifecycleSrv.URL, APIKey: "k"})
	_, err := p.Provision(context.Background(), sandbox.ProvisionOptions{TaskID: "t", Image: "i"})
	if err != nil {
		t.Fatal(err)
	}

	if err := p.SubmitManifest(context.Background(), "sb-x", []byte(`{"task_id":"t"}`)); err != nil {
		t.Fatalf("SubmitManifest: %v", err)
	}
	if writtenFiles["/workspace/.fleetlift/manifest.json"] != `{"task_id":"t"}` {
		t.Errorf("manifest not written correctly")
	}

	agentStatus, err := p.PollStatus(context.Background(), "sb-x")
	if err != nil {
		t.Fatalf("PollStatus: %v", err)
	}
	if string(agentStatus.Phase) != "executing" {
		t.Errorf("phase %q, want executing", agentStatus.Phase)
	}

	result, err := p.ReadResult(context.Background(), "sb-x")
	if err != nil {
		t.Fatalf("ReadResult: %v", err)
	}
	if string(result) != `{"status":"complete"}` {
		t.Errorf("result %q", result)
	}
}
```

### Step 2: Run to confirm failure

```bash
go test ./internal/sandbox/opensandbox/... -run TestProvider -v 2>&1
```
Expected: compile error — `Provider`, `Config`, `NewProvider` undefined.

### Step 3: Implement

```go
// internal/sandbox/opensandbox/provider.go
package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// Config holds OpenSandbox provider configuration.
type Config struct {
	// Domain is the OpenSandbox lifecycle server base URL (e.g. "http://localhost:8080").
	Domain string
	// APIKey is sent as the OPEN-SANDBOX-API-KEY header.
	APIKey string
	// UseServerProxy routes execd calls through the lifecycle server.
	// Required when the worker cannot reach sandbox containers directly (typical production setup).
	UseServerProxy bool
	// DefaultTimeoutSeconds is used when ProvisionOptions.Timeout is zero. Clamped to 60–86400.
	DefaultTimeoutSeconds int
}

func (c Config) timeoutFor(d interface{ Seconds() float64 }) int {
	if d != nil {
		s := int(d.Seconds())
		if s >= 60 {
			return clampTimeout(s)
		}
	}
	if c.DefaultTimeoutSeconds > 0 {
		return clampTimeout(c.DefaultTimeoutSeconds)
	}
	return 3600
}

func clampTimeout(s int) int {
	if s < 60 {
		return 60
	}
	if s > 86400 {
		return 86400
	}
	return s
}

// Provider implements sandbox.AgentProvider using OpenSandbox REST APIs.
type Provider struct {
	lifecycle  *LifecycleClient
	cfg        Config
	execdCache sync.Map // sandboxID → *ExecdClient
}

// Compile-time interface check.
var _ sandbox.AgentProvider = (*Provider)(nil)

// NewProvider creates an OpenSandbox provider.
func NewProvider(cfg Config) *Provider {
	return &Provider{
		lifecycle: NewLifecycleClient(cfg.Domain, cfg.APIKey),
		cfg:       cfg,
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "opensandbox" }

// Provision creates a new sandbox and caches its execd client.
func (p *Provider) Provision(ctx context.Context, opts sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	req := CreateSandboxRequest{
		Image:    SandboxImage{URI: opts.Image},
		Timeout:  clampTimeout(int(opts.Timeout.Seconds())),
		Env:      opts.Env,
		Metadata: map[string]string{"task_id": opts.TaskID},
	}
	if req.Timeout < 60 {
		req.Timeout = p.cfg.DefaultTimeoutSeconds
		if req.Timeout == 0 {
			req.Timeout = 3600
		}
	}
	// Agent mode: omit Entrypoint so the Dockerfile CMD runs the agent.
	// Non-agent mode: keep container alive for exec commands.
	if !opts.UseAgentMode {
		req.Entrypoint = []string{"sh", "-c", "touch /tmp/fleetlift.log && tail -f /tmp/fleetlift.log"}
	}
	if opts.Resources.MemoryBytes > 0 || opts.Resources.CPUQuota > 0 {
		limits := &ResourceLimits{}
		if opts.Resources.MemoryBytes > 0 {
			limits.Memory = fmt.Sprintf("%dMi", opts.Resources.MemoryBytes/(1024*1024))
		}
		if opts.Resources.CPUQuota > 0 {
			limits.CPU = fmt.Sprintf("%.2f", float64(opts.Resources.CPUQuota)/100000.0)
		}
		req.ResourceLimits = limits
	}

	resp, err := p.lifecycle.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("opensandbox provision: %w", err)
	}

	execdURL, err := p.lifecycle.GetEndpoint(ctx, resp.ID, execdPort, p.cfg.UseServerProxy)
	if err != nil {
		_ = p.lifecycle.Delete(ctx, resp.ID)
		return nil, fmt.Errorf("opensandbox get execd endpoint: %w", err)
	}
	p.execdCache.Store(resp.ID, NewExecdClient(execdURL, resp.AccessToken))

	return &sandbox.Sandbox{
		ID:         resp.ID,
		Provider:   "opensandbox",
		WorkingDir: opts.WorkingDir,
	}, nil
}

func (p *Provider) execd(sandboxID string) (*ExecdClient, error) {
	v, ok := p.execdCache.Load(sandboxID)
	if !ok {
		return nil, fmt.Errorf("no execd client for sandbox %q", sandboxID)
	}
	return v.(*ExecdClient), nil
}

// ExecShell runs a shell command string in the sandbox.
func (p *Provider) ExecShell(ctx context.Context, id string, command string, _ string) (*sandbox.ExecResult, error) {
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	result, err := e.RunCommand(ctx, "sh", []string{"-c", command})
	if err != nil {
		return nil, err
	}
	return &sandbox.ExecResult{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, nil
}

// Exec runs a structured command in the sandbox.
func (p *Provider) Exec(ctx context.Context, id string, cmd sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	if len(cmd.Command) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	result, err := e.RunCommand(ctx, cmd.Command[0], cmd.Command[1:])
	if err != nil {
		return nil, err
	}
	return &sandbox.ExecResult{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, nil
}

// CopyTo writes data to a path inside the sandbox.
func (p *Provider) CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error {
	e, err := p.execd(id)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(src, maxFileSize+1))
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	if len(data) > maxFileSize {
		return fmt.Errorf("source exceeds 10 MB limit")
	}
	return e.WriteFile(ctx, destPath, data)
}

// CopyFrom reads a file from the sandbox.
func (p *Provider) CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error) {
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	data, err := e.ReadFile(ctx, srcPath)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("file not found: %s", srcPath)
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

// Status returns the current phase of the sandbox.
func (p *Provider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	resp, err := p.lifecycle.Get(ctx, id)
	if err != nil {
		return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseUnknown, Message: err.Error()}, nil
	}
	return &sandbox.SandboxStatus{Phase: resp.SandboxPhase(), Message: resp.State}, nil
}

// Cleanup terminates and removes the sandbox.
func (p *Provider) Cleanup(ctx context.Context, id string) error {
	p.execdCache.Delete(id)
	return p.lifecycle.Delete(ctx, id)
}

// RenewTTL extends the sandbox expiration. Call during long-running activity heartbeats.
func (p *Provider) RenewTTL(ctx context.Context, id string) error {
	return p.lifecycle.RenewExpiration(ctx, id)
}

// --- AgentProvider protocol ---

// SubmitManifest writes the task manifest JSON into the sandbox.
func (p *Provider) SubmitManifest(ctx context.Context, id string, manifest []byte) error {
	e, err := p.execd(id)
	if err != nil {
		return err
	}
	if err := e.MakeDir(ctx, protocol.BasePath); err != nil {
		return fmt.Errorf("mkdir %s: %w", protocol.BasePath, err)
	}
	return e.WriteFile(ctx, protocol.ManifestPath, manifest)
}

// PollStatus reads the agent's current status from the sandbox.
func (p *Provider) PollStatus(ctx context.Context, id string) (*protocol.AgentStatus, error) {
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	data, err := e.ReadFile(ctx, protocol.StatusPath)
	if err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}
	if data == nil {
		return &protocol.AgentStatus{Phase: protocol.PhaseInitializing, Message: "Waiting for agent to start"}, nil
	}
	var status protocol.AgentStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("parse status.json: %w", err)
	}
	return &status, nil
}

// ReadResult reads the agent's full result JSON.
func (p *Provider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	data, err := e.ReadFile(ctx, protocol.ResultPath)
	if err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("result.json not found")
	}
	return data, nil
}

// SubmitSteering writes a steering instruction for the agent.
func (p *Provider) SubmitSteering(ctx context.Context, id string, instruction []byte) error {
	e, err := p.execd(id)
	if err != nil {
		return err
	}
	return e.WriteFile(ctx, protocol.SteeringPath, instruction)
}
```

### Step 4: Run tests

```bash
go test ./internal/sandbox/opensandbox/... -v
```
Expected: all tests pass.

### Step 5: Commit

```bash
git add internal/sandbox/opensandbox/provider.go internal/sandbox/opensandbox/provider_test.go
git commit -m "feat(sandbox/opensandbox): implement AgentProvider"
```

---

## Task 4: Replace factory and worker wiring

The routing abstraction in `factory.go` only exists because there were multiple providers. With one provider, simplify it to a direct construction function.

**Files:**
- Create: `internal/sandbox/opensandbox/register.go`
- Modify: `internal/sandbox/factory.go` — replace ProviderConfig with OpenSandbox-only config; simplify NewProvider
- Modify: `internal/sandbox/factory_test.go` — remove docker/k8s tests, update for new config
- Modify: `cmd/worker/main.go` — remove old blank imports; read OpenSandbox env vars

### Step 1: Check existing factory_test.go to understand what to remove

```bash
cat internal/sandbox/factory_test.go
```

Note all test cases that reference "docker", "kubernetes", or K8s-specific config fields — they will be deleted.

### Step 2: Write the replacement factory test

Replace `internal/sandbox/factory_test.go` with only:

```go
package sandbox_test

import (
	"testing"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	_ "github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox"
)

func TestNewProvider_OpenSandbox(t *testing.T) {
	p, err := sandbox.NewProvider(sandbox.ProviderConfig{
		OpenSandboxDomain: "http://localhost:8080",
		OpenSandboxAPIKey: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "opensandbox" {
		t.Errorf("Name() = %q, want opensandbox", p.Name())
	}
}

func TestNewProvider_MissingDomain(t *testing.T) {
	_, err := sandbox.NewProvider(sandbox.ProviderConfig{
		OpenSandboxAPIKey: "key",
		// Domain intentionally empty
	})
	if err == nil {
		t.Error("expected error for missing domain")
	}
}
```

Run to confirm failure:
```bash
go test ./internal/sandbox/... -run TestNewProvider -v 2>&1
```

### Step 3: Replace factory.go

```go
// internal/sandbox/factory.go
package sandbox

import "fmt"

// ProviderConfig contains configuration for the OpenSandbox provider.
type ProviderConfig struct {
	// OpenSandboxDomain is the lifecycle server URL (e.g. "http://localhost:8080").
	OpenSandboxDomain string
	// OpenSandboxAPIKey is the OPEN-SANDBOX-API-KEY.
	OpenSandboxAPIKey string
	// OpenSandboxUseServerProxy routes execd calls through the lifecycle server.
	// Set true when the worker cannot reach sandbox containers directly.
	OpenSandboxUseServerProxy bool
	// OpenSandboxDefaultTimeout is the sandbox TTL in seconds (60–86400). Default: 3600.
	OpenSandboxDefaultTimeout int
}

// ProviderFactory is a function that creates an AgentProvider from config.
type ProviderFactory func(cfg ProviderConfig) (AgentProvider, error)

var providerFactory ProviderFactory

// RegisterProvider registers the single provider factory.
// Called by internal/sandbox/opensandbox init().
func RegisterProvider(_ string, factory ProviderFactory) {
	providerFactory = factory
}

// NewProvider creates the AgentProvider from config.
func NewProvider(cfg ProviderConfig) (AgentProvider, error) {
	if cfg.OpenSandboxDomain == "" {
		return nil, fmt.Errorf("OpenSandboxDomain is required")
	}
	if providerFactory == nil {
		return nil, fmt.Errorf("no provider registered (import _ \"github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox\")")
	}
	return providerFactory(cfg)
}
```

### Step 4: Create register.go

```go
// internal/sandbox/opensandbox/register.go
package opensandbox

import "github.com/tinkerloft/fleetlift/internal/sandbox"

func init() {
	sandbox.RegisterProvider("opensandbox", func(cfg sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider(Config{
			Domain:                cfg.OpenSandboxDomain,
			APIKey:                cfg.OpenSandboxAPIKey,
			UseServerProxy:        cfg.OpenSandboxUseServerProxy,
			DefaultTimeoutSeconds: cfg.OpenSandboxDefaultTimeout,
		}), nil
	})
}
```

### Step 5: Update cmd/worker/main.go

Replace the sandbox-related imports and config block:

```go
// Remove these two lines:
_ "github.com/tinkerloft/fleetlift/internal/sandbox/docker"
_ "github.com/tinkerloft/fleetlift/internal/sandbox/k8s"

// Add this line:
_ "github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox"
```

Replace the ProviderConfig construction:

```go
// Remove:
providerName := os.Getenv("SANDBOX_PROVIDER")
cfg := sandbox.ProviderConfig{
    Namespace:      getEnvOrDefault("SANDBOX_NAMESPACE", "sandbox-isolated"),
    AgentImage:     getEnvOrDefault("AGENT_IMAGE", "fleetlift-agent:latest"),
    KubeconfigPath: os.Getenv("KUBECONFIG"),
}
provider, err := sandbox.NewProvider(providerName, cfg)

// Replace with:
cfg := sandbox.ProviderConfig{
    OpenSandboxDomain:         getEnvOrDefault("OPEN_SANDBOX_DOMAIN", "http://localhost:8080"),
    OpenSandboxAPIKey:         os.Getenv("OPEN_SANDBOX_API_KEY"),
    OpenSandboxUseServerProxy: os.Getenv("OPEN_SANDBOX_USE_SERVER_PROXY") == "true",
}
provider, err := sandbox.NewProvider(cfg)
```

### Step 6: Verify everything still compiles and tests pass

```bash
go test ./internal/sandbox/... -v
go build ./...
```
Expected: all pass. If `factory_test.go` references old types, fix them now.

### Step 7: Commit

```bash
git add internal/sandbox/factory.go internal/sandbox/factory_test.go \
        internal/sandbox/opensandbox/register.go cmd/worker/main.go
git commit -m "feat(sandbox): replace provider factory with OpenSandbox-only config"
```

---

## Task 5: Delete Docker and Kubernetes Providers

The docker and k8s packages are now dead code. Delete them.

### Step 1: Delete the packages

```bash
rm -rf internal/sandbox/docker/
rm -rf internal/sandbox/k8s/
```

### Step 2: Verify no remaining references

```bash
grep -r "sandbox/docker\|sandbox/k8s" --include="*.go" .
```
Expected: no output. If any references remain, remove them.

### Step 3: Verify build and tests

```bash
go build ./...
go test ./...
```
Expected: clean build, all tests pass. The `web` embed failure is pre-existing and unrelated.

### Step 4: Commit

```bash
git add -A
git commit -m "chore(sandbox): delete docker and k8s providers (superseded by OpenSandbox)"
```

---

## Task 6: Integration Test

Skipped in normal CI via build tag. Requires a running OpenSandbox server.

**Files:**
- Create: `internal/sandbox/opensandbox/provider_integration_test.go`

```go
//go:build integration

package opensandbox_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox"
)

func TestIntegration_FullLifecycle(t *testing.T) {
	domain := os.Getenv("OPEN_SANDBOX_DOMAIN")
	apiKey := os.Getenv("OPEN_SANDBOX_API_KEY")
	if domain == "" || apiKey == "" {
		t.Skip("OPEN_SANDBOX_DOMAIN or OPEN_SANDBOX_API_KEY not set")
	}

	p := opensandbox.NewProvider(opensandbox.Config{
		Domain:         domain,
		APIKey:         apiKey,
		UseServerProxy: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:       "integration-test",
		Image:        "ubuntu:22.04",
		UseAgentMode: false,
		Timeout:      5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Logf("Sandbox ID: %s", sb.ID)
	defer func() {
		if err := p.Cleanup(context.Background(), sb.ID); err != nil {
			t.Logf("Cleanup: %v", err)
		}
	}()

	// ExecShell round-trip
	result, err := p.ExecShell(ctx, sb.ID, "echo hello_from_sandbox", "")
	if err != nil {
		t.Fatalf("ExecShell: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	t.Logf("stdout: %q", result.Stdout)

	// SubmitManifest + PollStatus (no agent running, expect initializing)
	if err := p.SubmitManifest(ctx, sb.ID, []byte(`{"task_id":"integration-test"}`)); err != nil {
		t.Fatalf("SubmitManifest: %v", err)
	}
	agentStatus, err := p.PollStatus(ctx, sb.ID)
	if err != nil {
		t.Fatalf("PollStatus: %v", err)
	}
	t.Logf("agent phase: %s", agentStatus.Phase)

	// Sandbox status
	status, err := p.Status(ctx, sb.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	t.Logf("sandbox: %s (%s)", status.Phase, status.Message)
}
```

Run with a live server:
```bash
OPEN_SANDBOX_DOMAIN=http://localhost:8080 OPEN_SANDBOX_API_KEY=your-key \
  go test ./internal/sandbox/opensandbox/... -tags integration -v -run TestIntegration
```

### Commit

```bash
git add internal/sandbox/opensandbox/provider_integration_test.go
git commit -m "test(sandbox/opensandbox): add integration test"
```

---

## Task 7: Final Verification

```bash
cd /Users/andrew/dev/projects/fleetlift/.worktrees/feat/opensandbox-provider
make lint           # or: golangci-lint run ./...
go test ./...
go build ./...
```

All must pass before opening a PR.

---

## Unanswered Questions

1. **execd SSE format** — exact field names for `ExecdCommandEvent` must be verified against `components/execd/` before implementing Task 2. The plan assumes `type`/`data`/`exitCode` but the real names may differ.
2. **files/replace body** — is `content` a raw string or base64? Verify before Task 2.
3. **AccessToken in create response** — does `POST /v1/sandboxes` return the execd access token? Or is it derived some other way (e.g. the sandbox ID itself)? Verify before Task 3.
4. **execd readiness timing** — calling `GetEndpoint` immediately after `Create` may fail if the sandbox is still `Pending`. The `Provision` method may need a short poll loop waiting for `Running` state before resolving the execd URL.
5. **TTL renewal** — tasks that run longer than the initial TTL will have their sandbox terminated. For v1, set TTL = task timeout + buffer. A follow-up can add `RenewTTL` calls inside `WaitForAgentPhase` heartbeats.
