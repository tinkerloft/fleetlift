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
	"sync"
	"time"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// Client implements sandbox.Client using the OpenSandbox REST API.
type Client struct {
	domain     string
	apiKey     string
	http       *http.Client
	streamHTTP *http.Client

	// proxyPorts caches the per-sandbox proxy port returned in create metadata.
	// Exec, file I/O, etc. go through the sandbox's proxy port, not the main API.
	mu         sync.RWMutex
	proxyPorts map[string]string // sandbox ID → "host:port" or "localhost:port"
}

// New creates a new OpenSandbox client.
func New(domain, apiKey string) *Client {
	return &Client{
		domain:     strings.TrimRight(domain, "/"),
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 30 * time.Second},
		streamHTTP: &http.Client{}, // context-controlled for streaming
		proxyPorts: make(map[string]string),
	}
}

func (c *Client) baseURL() string {
	if strings.HasPrefix(c.domain, "http://") || strings.HasPrefix(c.domain, "https://") {
		return c.domain
	}
	return "https://" + c.domain
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// sandboxProxyURL returns the base URL for per-sandbox operations (exec, file I/O).
// In local mode: http://localhost:{proxy_port}
// In production: https://{id}.{domain}
func (c *Client) sandboxProxyURL(id string) string {
	if strings.HasPrefix(c.domain, "http://") || strings.HasPrefix(c.domain, "https://") {
		c.mu.RLock()
		port, ok := c.proxyPorts[id]
		c.mu.RUnlock()
		if !ok {
			// Cache miss (e.g. worker restarted) — fetch and cache the proxy port.
			if err := c.fetchAndCacheProxyPort(context.Background(), id); err == nil {
				c.mu.RLock()
				port, ok = c.proxyPorts[id]
				c.mu.RUnlock()
			}
		}
		if ok {
			// Extract host from domain URL for proxy.
			// e.g. http://localhost:8090 → http://localhost:{port}
			u, err := url.Parse(c.domain)
			if err == nil {
				host := u.Hostname()
				return fmt.Sprintf("%s://%s:%s", u.Scheme, host, port)
			}
		}
		// Fallback: try path routing (won't work for exec, but will give a clear error)
		return c.baseURL() + "/v1/sandboxes/" + id
	}
	return fmt.Sprintf("https://%s.%s", id, c.domain)
}

func (c *Client) Create(ctx context.Context, opts sandbox.CreateOpts) (string, error) {
	timeoutSecs := opts.TimeoutMins * 60
	timeoutSecs = max(timeoutSecs, 60)

	// Resource limits: use caller-provided values or defaults.
	rl := map[string]string{"cpu": "1000m", "memory": "2Gi"}
	if opts.Resources != nil {
		if opts.Resources.CPU != "" {
			rl["cpu"] = opts.Resources.CPU
		}
		if opts.Resources.Memory != "" {
			rl["memory"] = opts.Resources.Memory
		}
	}

	body := map[string]any{
		"image":          map[string]string{"uri": opts.Image},
		"env":            opts.Env,
		"timeout":        timeoutSecs,
		"resourceLimits": rl,
		"entrypoint":     []string{"sleep", "infinity"},
	}

	// Network policy: pass through to OpenSandbox egress sidecar.
	// Field name "networkPolicy" matches OpenSandbox server v0.1.9 REST API.
	if opts.NetworkPolicy != nil {
		rules := make([]map[string]string, 0, len(opts.NetworkPolicy.Egress))
		for _, r := range opts.NetworkPolicy.Egress {
			rules = append(rules, map[string]string{
				"action": r.Action,
				"target": r.Target,
			})
		}
		body["networkPolicy"] = map[string]any{
			"defaultAction": opts.NetworkPolicy.DefaultAction,
			"egress":        rules,
		}
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("opensandbox: create returned %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		ID       string            `json:"id"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("opensandbox: decode create response: %w", err)
	}

	if result.ID == "" {
		return "", fmt.Errorf("opensandbox: create response missing sandbox ID (status %d)", resp.StatusCode)
	}

	// Cache the proxy port for subsequent operations.
	if port := result.Metadata["opensandbox.io/embedding-proxy-port"]; port != "" {
		c.mu.Lock()
		c.proxyPorts[result.ID] = port
		c.mu.Unlock()
	} else {
		// If not in create response, fetch sandbox details to get the port.
		if err := c.fetchAndCacheProxyPort(ctx, result.ID); err != nil {
			// Non-fatal: sandboxProxyURL will fall back to path routing.
			_ = err
		}
	}

	return result.ID, nil
}

// fetchAndCacheProxyPort retrieves sandbox details and caches the proxy port.
func (c *Client) fetchAndCacheProxyPort(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/v1/sandboxes/"+id, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get sandbox returned %d", resp.StatusCode)
	}

	var details struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return err
	}

	if port := details.Metadata["opensandbox.io/embedding-proxy-port"]; port != "" {
		c.mu.Lock()
		c.proxyPorts[id] = port
		c.mu.Unlock()
	}
	return nil
}

func (c *Client) ExecStream(ctx context.Context, id, cmd, workDir string, onLine func(string)) error {
	body := map[string]any{
		"command": cmd,
		"cwd":     workDir,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("opensandbox: marshal exec body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sandboxProxyURL(id)+"/command", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("opensandbox: exec request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: exec: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: exec returned %d: %s", resp.StatusCode, string(b))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024) // 4 MiB max line size
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// OpenSandbox streams JSON lines: {"type":"stdout","text":"..."}
		// Convert to the normalized format our callers expect: {"stream":"stdout","content":"..."}
		var msg struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Error *struct {
				EValue string `json:"evalue"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil {
			switch msg.Type {
			case "stdout", "stderr":
				normalized, _ := json.Marshal(map[string]string{
					"stream":  msg.Type,
					"content": msg.Text,
				})
				onLine(string(normalized))
			case "error":
				// Surface execution errors (e.g. command not found) so callers fail loudly
				// instead of silently completing with empty output.
				detail := msg.Text
				if msg.Error != nil && msg.Error.EValue != "" {
					detail = msg.Error.EValue
				}
				// Emit error as stderr so callers capture it in their log buffers.
				if errLine, merr := json.Marshal(map[string]string{"stream": "stderr", "content": detail}); merr == nil {
					onLine(string(errLine))
				}
				return fmt.Errorf("opensandbox: exec error: %s", detail)
			default:
				// init, ping, execution_complete — skip
				continue
			}
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

	// OpenSandbox expects 'metadata' as a file upload containing JSON with the path.
	metadataJSON, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return fmt.Errorf("opensandbox: marshal metadata: %w", err)
	}
	mw, err := w.CreateFormFile("metadata", "metadata.json")
	if err != nil {
		return fmt.Errorf("opensandbox: create metadata part: %w", err)
	}
	if _, err := mw.Write(metadataJSON); err != nil {
		return fmt.Errorf("opensandbox: write metadata: %w", err)
	}

	fw, err := w.CreateFormFile("file", "upload")
	if err != nil {
		return fmt.Errorf("opensandbox: create form file: %w", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		return fmt.Errorf("opensandbox: write file content: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("opensandbox: finalise multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sandboxProxyURL(id)+"/files/upload", &buf)
	if err != nil {
		return fmt.Errorf("opensandbox: write file request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: write file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: write file returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) WriteBytes(ctx context.Context, id, path string, data []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	metadataJSON, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return fmt.Errorf("opensandbox: marshal metadata: %w", err)
	}
	mw, err := w.CreateFormFile("metadata", "metadata.json")
	if err != nil {
		return fmt.Errorf("opensandbox: create metadata part: %w", err)
	}
	if _, err := mw.Write(metadataJSON); err != nil {
		return fmt.Errorf("opensandbox: write metadata: %w", err)
	}

	fw, err := w.CreateFormFile("file", "upload")
	if err != nil {
		return fmt.Errorf("opensandbox: create form file: %w", err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("opensandbox: write file content: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("opensandbox: finalise multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sandboxProxyURL(id)+"/files/upload", &buf)
	if err != nil {
		return fmt.Errorf("opensandbox: write bytes request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: write bytes: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: write bytes returned %d: %s", resp.StatusCode, string(b))
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.sandboxProxyURL(id)+"/files/download?path="+url.QueryEscape(path), nil)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: read file request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: read file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: kill returned %d: %s", resp.StatusCode, string(b))
	}

	// Clean up cached proxy port.
	c.mu.Lock()
	delete(c.proxyPorts, id)
	c.mu.Unlock()

	return nil
}

func (c *Client) RenewExpiration(ctx context.Context, id string) error {
	// OpenSandbox requires an expiresAt timestamp in the body.
	expiresAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]string{"expiresAt": expiresAt})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/sandboxes/"+id+"/renew-expiration", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("opensandbox: renew request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: renew: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: renew returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
