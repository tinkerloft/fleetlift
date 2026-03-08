package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	fleetproto "github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// Config holds OpenSandbox provider configuration.
type Config struct {
	// Domain is the OpenSandbox lifecycle server base URL (e.g. "http://localhost:8080").
	Domain string
	// APIKey is sent as the OPEN-SANDBOX-API-KEY header to the lifecycle server.
	APIKey string
	// ExecdAccessToken is sent as X-EXECD-ACCESS-TOKEN to the execd daemon.
	// Leave empty if the execd is running without authentication (the default).
	ExecdAccessToken string
	// UseServerProxy routes execd calls through the lifecycle server.
	// Required when the worker cannot reach sandbox containers directly (typical production setup).
	UseServerProxy bool
	// DefaultTimeoutSeconds is used when ProvisionOptions.Timeout is zero. Clamped to 60–86400.
	DefaultTimeoutSeconds int
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
	timeout := int(opts.Timeout.Seconds())
	if timeout <= 0 {
		timeout = p.cfg.DefaultTimeoutSeconds
		if timeout <= 0 {
			timeout = 3600
		}
	} else {
		timeout = clampTimeout(timeout)
	}

	// Entrypoint is required by the lifecycle API.
	// Agent mode: use the image's default CMD to run the agent.
	// Non-agent mode: keep container alive for exec commands.
	entrypoint := []string{"sh", "-c", "touch /tmp/fleetlift.log && tail -f /tmp/fleetlift.log"}
	if opts.UseAgentMode {
		entrypoint = []string{"/bin/sh", "-c", "exec \"$@\"", "--"}
	}

	req := CreateSandboxRequest{
		Image:      SandboxImage{URI: opts.Image},
		Entrypoint: entrypoint,
		Timeout:    timeout,
		Env:        opts.Env,
		Metadata:   map[string]string{"task_id": opts.TaskID},
	}
	if opts.Resources.MemoryBytes > 0 || opts.Resources.CPUQuota > 0 {
		limits := map[string]string{}
		if opts.Resources.MemoryBytes > 0 {
			limits["memory"] = fmt.Sprintf("%dMi", opts.Resources.MemoryBytes/(1024*1024))
		}
		if opts.Resources.CPUQuota > 0 {
			limits["cpu"] = fmt.Sprintf("%.2f", float64(opts.Resources.CPUQuota)/100000.0)
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
	p.execdCache.Store(resp.ID, NewExecdClient(execdURL, p.cfg.ExecdAccessToken))

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
	return &sandbox.SandboxStatus{Phase: resp.SandboxPhase(), Message: resp.Status.State}, nil
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
	if err := e.MakeDir(ctx, fleetproto.DefaultBasePath); err != nil {
		return fmt.Errorf("mkdir %s: %w", fleetproto.DefaultBasePath, err)
	}
	return e.WriteFile(ctx, fleetproto.ManifestPath(fleetproto.DefaultBasePath), manifest)
}

// PollStatus reads the agent's current status from the sandbox. Returns raw JSON bytes.
func (p *Provider) PollStatus(ctx context.Context, id string) ([]byte, error) {
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	data, err := e.ReadFile(ctx, fleetproto.StatusPath(fleetproto.DefaultBasePath))
	if err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}
	if data == nil {
		// Status file not yet written — agent is still initializing.
		status := fleetproto.AgentStatus{Phase: fleetproto.PhaseInitializing, Message: "Waiting for agent to start"}
		return json.Marshal(status)
	}
	return data, nil
}

// ReadResult reads the agent's full result JSON.
func (p *Provider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	e, err := p.execd(id)
	if err != nil {
		return nil, err
	}
	data, err := e.ReadFile(ctx, fleetproto.ResultPath(fleetproto.DefaultBasePath))
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
	return e.WriteFile(ctx, fleetproto.SteeringPath(fleetproto.DefaultBasePath), instruction)
}
