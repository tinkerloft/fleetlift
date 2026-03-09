package opensandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// CreateSandboxRequest is sent to POST /v1/sandboxes.
type CreateSandboxRequest struct {
	Image          SandboxImage      `json:"image"`
	Entrypoint     []string          `json:"entrypoint"`
	Timeout        int               `json:"timeout"` // seconds, 60–86400
	Env            map[string]string `json:"env,omitempty"`
	ResourceLimits map[string]string `json:"resourceLimits,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// sandboxStatusField is the nested status object in lifecycle API responses.
type sandboxStatusField struct {
	State string `json:"state"`
}

// SandboxResponse is returned by GET/POST /v1/sandboxes.
type SandboxResponse struct {
	ID     string             `json:"id"`
	Status sandboxStatusField `json:"status"`
}

// SandboxPhase converts OpenSandbox state to our internal SandboxPhase.
func (r *SandboxResponse) SandboxPhase() sandbox.SandboxPhase {
	switch r.Status.State {
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
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// renewExpirationRequest is the body for POST /v1/sandboxes/{id}/renew-expiration.
type renewExpirationRequest struct {
	ExpiresAt string `json:"expiresAt"`
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

// Create creates a new sandbox.
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

// RenewExpiration extends the sandbox TTL by one hour.
func (c *LifecycleClient) RenewExpiration(ctx context.Context, id string) error {
	body := renewExpirationRequest{
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/sandboxes/"+id+"/renew-expiration", body)
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
	endpoint := out.Endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	return endpoint, nil
}
