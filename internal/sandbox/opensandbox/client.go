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
	"net/url"
	"strings"
	"time"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// Client implements sandbox.Client using the OpenSandbox REST API.
type Client struct {
	domain     string
	apiKey     string
	http       *http.Client
	streamHTTP *http.Client
}

// New creates a new OpenSandbox client.
func New(domain, apiKey string) *Client {
	return &Client{
		domain:     strings.TrimRight(domain, "/"),
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 30 * time.Second},
		streamHTTP: &http.Client{}, // context-controlled for streaming
	}
}

func (c *Client) baseURL() string {
	return "https://" + c.domain
}

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func (c *Client) Create(ctx context.Context, opts sandbox.CreateOpts) (string, error) {
	body := map[string]any{
		"image":        opts.Image,
		"env":          opts.Env,
		"timeout_mins": opts.TimeoutMins,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("opensandbox: marshal create body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/sandboxes", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("opensandbox: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("opensandbox: create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("opensandbox: create returned %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("opensandbox: decode create response: %w", err)
	}
	return result.ID, nil
}

func (c *Client) sandboxURL(id string) string {
	return fmt.Sprintf("https://%s.%s", id, c.domain)
}

func (c *Client) ExecStream(ctx context.Context, id, cmd, workDir string, onLine func(string)) error {
	body := map[string]any{
		"cmd":        cmd,
		"workdir":    workDir,
		"background": false,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("opensandbox: marshal exec body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sandboxURL(id)+"/command", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("opensandbox: exec request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: exec returned %d: %s", resp.StatusCode, string(b))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		// SSE format: lines prefixed with "data: "
		if strings.HasPrefix(line, "data: ") {
			onLine(strings.TrimPrefix(line, "data: "))
		} else if line != "" && !strings.HasPrefix(line, ":") {
			onLine(line)
		}
	}
	return scanner.Err()
}

func (c *Client) Exec(ctx context.Context, id, cmd, workDir string) (string, string, error) {
	var stdout, stderr strings.Builder
	err := c.ExecStream(ctx, id, cmd, workDir, func(line string) {
		var msg struct {
			Stream  string `json:"stream"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil && msg.Stream != "" {
			switch msg.Stream {
			case "stderr":
				stderr.WriteString(msg.Content)
			default:
				stdout.WriteString(msg.Content)
			}
		} else {
			stdout.WriteString(line)
			stdout.WriteString("\n")
		}
	})
	return stdout.String(), stderr.String(), err
}

func (c *Client) WriteFile(ctx context.Context, id, path, content string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("path", path); err != nil {
		return fmt.Errorf("opensandbox: write path field: %w", err)
	}

	fw, err := w.CreateFormFile("file", "upload")
	if err != nil {
		return fmt.Errorf("opensandbox: create form file: %w", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		return fmt.Errorf("opensandbox: write file content: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sandboxURL(id)+"/files/upload", &buf)
	if err != nil {
		return fmt.Errorf("opensandbox: write file request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: write file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: write file returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) ReadFile(ctx context.Context, id, path string) (string, error) {
	b, err := c.ReadBytes(ctx, id, path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) ReadBytes(ctx context.Context, id, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.sandboxURL(id)+"/files/download?path="+url.QueryEscape(path), nil)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: read file request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: read file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opensandbox: read file returned %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) Kill(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL()+"/v1/sandboxes/"+id, nil)
	if err != nil {
		return fmt.Errorf("opensandbox: kill request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: kill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: kill returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) RenewExpiration(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/sandboxes/"+id+"/renew-expiration", nil)
	if err != nil {
		return fmt.Errorf("opensandbox: renew request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: renew: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: renew returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
