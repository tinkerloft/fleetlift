package opensandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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

	// Write metadata part: single FileMetadata object as a file attachment.
	// execd reads from form.File["metadata"], which requires CreateFormFile not CreateFormField.
	meta := fileMetadata{
		Path:       path,
		Permission: permission{Owner: "", Group: "", Mode: 644},
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal file metadata: %w", err)
	}
	metaPart, err := writer.CreateFormFile("metadata", "metadata.json")
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
// It first tries the /files/download endpoint (direct execd access). If that returns
// HTTP 400 (which happens when routing through the OpenSandbox server proxy because the
// proxy strips query parameters), it falls back to reading via the /command endpoint
// using `cat | base64` to safely transfer binary content.
func (c *ExecdClient) ReadFile(ctx context.Context, path string) ([]byte, error) {
	data, err := c.readFileViaDownload(ctx, path)
	if err == nil {
		return data, nil
	}
	// If the download endpoint returned 400 (proxy strips query params), fall back
	// to reading via the command endpoint.
	if strings.Contains(err.Error(), "HTTP 400") {
		return c.readFileViaCommand(ctx, path)
	}
	return nil, err
}

// readFileViaDownload uses the GET /files/download?path=... execd endpoint.
func (c *ExecdClient) readFileViaDownload(ctx context.Context, path string) ([]byte, error) {
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

// readFileViaCommand reads a file by executing `cat | base64` via the /command endpoint.
// This works when the proxy strips query parameters from the /files/download endpoint.
// Returns nil, nil if the file does not exist.
func (c *ExecdClient) readFileViaCommand(ctx context.Context, path string) ([]byte, error) {
	const notFoundMarker = "__FLEETLIFT_NOT_FOUND__"
	// Pass the shell script directly as the command string (no args slice) so that
	// RunCommand sends it verbatim to execd without adding a "sh -c" prefix that
	// would cause double-quoting issues when execd also wraps it in a shell.
	// The path comes from internal code (manifest/status/result paths), not user input.
	cmd := fmt.Sprintf("if [ -f '%s' ]; then cat '%s' | base64 -w0; else printf '%s'; fi", path, path, notFoundMarker)
	result, err := c.RunCommand(ctx, cmd, nil)
	if err != nil {
		return nil, fmt.Errorf("execd read %s via command: %w", path, err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("execd read %s via command: exit %d: %s", path, result.ExitCode, result.Stderr)
	}
	stdout := strings.TrimSpace(result.Stdout)
	if stdout == notFoundMarker {
		return nil, nil
	}
	if stdout == "" {
		// Empty file.
		return []byte{}, nil
	}
	data, err := base64.StdEncoding.DecodeString(stdout)
	if err != nil {
		return nil, fmt.Errorf("execd read %s via command: base64 decode: %w", path, err)
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("execd read %s: exceeds 10 MB limit", path)
	}
	return data, nil
}

// TailFile streams a file from the sandbox by running `tail -f` via execd.
// Calls onLine for each output line. Blocks until ctx is cancelled or the connection closes.
func (c *ExecdClient) TailFile(ctx context.Context, path string, onLine func(string)) error {
	resp, err := c.doJSON(ctx, http.MethodPost, "/command", execdCommandRequest{
		Command: "tail -f " + path,
	})
	if err != nil {
		return fmt.Errorf("execd tail %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("execd tail %s: HTTP %d", path, resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event execdCommandEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if (event.Type == "stdout" || event.Type == "stderr") && event.Text != "" {
			onLine(event.Text)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return scanner.Err()
}

// StreamSandboxLog streams a log file from a sandbox by connecting to execd and
// running `tail -f`. Calls onLine for each output line until ctx is cancelled.
// This is a convenience wrapper around LifecycleClient.GetEndpoint + ExecdClient.TailFile.
func StreamSandboxLog(ctx context.Context, lifecycleURL, sandboxID, logPath string, useServerProxy bool, onLine func(string)) error {
	lc := NewLifecycleClient(lifecycleURL, "")
	endpoint, err := lc.GetEndpoint(ctx, sandboxID, execdPort, useServerProxy)
	if err != nil {
		return fmt.Errorf("get execd endpoint for sandbox %s: %w", sandboxID, err)
	}
	ec := NewExecdClient(endpoint, "")
	return ec.TailFile(ctx, logPath, onLine)
}

// MakeDir creates a directory inside the sandbox.
// The request body is a map from path to permission (matching execd's MakeDirs API).
func (c *ExecdClient) MakeDir(ctx context.Context, path string) error {
	body := map[string]permission{
		path: {Owner: "", Group: "", Mode: 755},
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
