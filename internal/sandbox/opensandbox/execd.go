package opensandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxFileSize            = 10 << 20 // 10 MB
	execdAccessTokenHeader = "X-EXECD-ACCESS-TOKEN"
)

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

// execdCommandRequest is the body for POST /command.
type execdCommandRequest struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

// execdCommandEvent is a single SSE event from POST /command.
// Events are raw JSON objects separated by double newlines (no "data:" prefix).
type execdCommandEvent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// commandStatusResponse is returned by GET /command/status/{id}.
type commandStatusResponse struct {
	ID       string `json:"id"`
	Running  bool   `json:"running"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

// fileMetadata is a single entry in the multipart upload metadata JSON array.
type fileMetadata struct {
	Path       string     `json:"path"`
	Permission permission `json:",inline"`
}

// permission holds Unix file ownership and mode.
type permission struct {
	Owner string `json:"owner"`
	Group string `json:"group"`
	Mode  int    `json:"mode"`
}

// CommandResult holds aggregated command output.
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func (c *ExecdClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.accessToken != "" {
		req.Header.Set(execdAccessTokenHeader, c.accessToken)
	}
	return req, nil
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
	req, err := c.newRequest(ctx, method, path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// RunCommand executes a shell command and returns aggregated stdout/stderr/exit code.
// execd streams output as raw JSON objects separated by double newlines.
// The first event is "init" carrying the session ID, used to fetch the exit code after completion.
func (c *ExecdClient) RunCommand(ctx context.Context, cmd string, args []string) (*CommandResult, error) {
	command := cmd
	if len(args) > 0 {
		command = strings.Join(append([]string{cmd}, args...), " ")
	}
	resp, err := c.doJSON(ctx, http.MethodPost, "/command", execdCommandRequest{Command: command})
	if err != nil {
		return nil, fmt.Errorf("execd command: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("execd command: HTTP %d", resp.StatusCode)
	}

	result := &CommandResult{}
	var commandID string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event execdCommandEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // skip malformed events
		}
		switch event.Type {
		case "init":
			commandID = event.Text
		case "stdout":
			result.Stdout += event.Text
		case "stderr":
			result.Stderr += event.Text
		case "execution_complete":
			// exit code fetched below
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading command SSE: %w", err)
	}

	// Fetch exit code from status endpoint.
	if commandID != "" {
		status, err := c.getCommandStatus(ctx, commandID)
		if err == nil && status.ExitCode != nil {
			result.ExitCode = *status.ExitCode
		}
	}

	return result, nil
}

// getCommandStatus fetches the exit code and status for a completed command.
func (c *ExecdClient) getCommandStatus(ctx context.Context, id string) (*commandStatusResponse, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, "/command/status/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("execd command status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("execd command status: HTTP %d", resp.StatusCode)
	}
	var out commandStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode command status: %w", err)
	}
	return &out, nil
}

// WriteFile writes content to a path inside the sandbox via multipart upload.
func (c *ExecdClient) WriteFile(ctx context.Context, path string, content []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Write metadata part: JSON array of FileMetadata.
	meta := []fileMetadata{{
		Path:       path,
		Permission: permission{Owner: "", Group: "", Mode: 0644},
	}}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal file metadata: %w", err)
	}
	metaPart, err := writer.CreateFormField("metadata")
	if err != nil {
		return fmt.Errorf("create metadata field: %w", err)
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	// Write file part.
	filePart, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return fmt.Errorf("create file field: %w", err)
	}
	if _, err := filePart.Write(content); err != nil {
		return fmt.Errorf("write file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/files/upload", &body)
	if err != nil {
		return fmt.Errorf("execd upload %s: %w", path, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	uploadResp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execd upload %s: %w", path, err)
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode >= 300 {
		return fmt.Errorf("execd upload %s: HTTP %d", path, uploadResp.StatusCode)
	}
	return nil
}

// ReadFile reads a file from the sandbox. Returns nil, nil if the file does not exist.
func (c *ExecdClient) ReadFile(ctx context.Context, path string) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/files/download", nil)
	if err != nil {
		return nil, err
	}
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
// The request body is a map from path to permission (matching execd's MakeDirs API).
func (c *ExecdClient) MakeDir(ctx context.Context, path string) error {
	body := map[string]permission{
		path: {Owner: "", Group: "", Mode: 0755},
	}
	resp, err := c.doJSON(ctx, http.MethodPost, "/directories", body)
	if err != nil {
		return fmt.Errorf("execd mkdir %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("execd mkdir %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}
