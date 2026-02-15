package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

const (
	defaultNamespace  = "sandbox-isolated"
	defaultAgentImage = "fleetlift-agent:latest"
)

// Compile-time check that Provider implements sandbox.AgentProvider.
var _ sandbox.AgentProvider = (*Provider)(nil)

// Provider implements sandbox.AgentProvider using Kubernetes Jobs.
type Provider struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
	namespace  string
	agentImage string
	podCache   *sync.Map // instance-scoped cache mapping job name to pod name
}

// NewProvider creates a new Kubernetes sandbox provider.
func NewProvider(cfg sandbox.ProviderConfig) (*Provider, error) {
	namespace := valueOrDefault(cfg.Namespace, defaultNamespace)
	agentImage := valueOrDefault(cfg.AgentImage, defaultAgentImage)

	var restConfig *rest.Config
	var err error

	if cfg.KubeconfigPath != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s clientset: %w", err)
	}

	return &Provider{
		clientset:  clientset,
		restConfig: restConfig,
		namespace:  namespace,
		agentImage: agentImage,
		podCache:   &sync.Map{},
	}, nil
}

func valueOrDefault(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

// newProviderFromClient creates a provider with an injected clientset (for testing).
func newProviderFromClient(clientset kubernetes.Interface, restConfig *rest.Config, namespace, agentImage string) *Provider {
	return &Provider{
		clientset:  clientset,
		restConfig: restConfig,
		namespace:  valueOrDefault(namespace, defaultNamespace),
		agentImage: valueOrDefault(agentImage, defaultAgentImage),
		podCache:   &sync.Map{},
	}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "kubernetes"
}

// Provision creates a new Kubernetes Job for sandbox execution.
func (p *Provider) Provision(ctx context.Context, opts sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	job := buildJobSpec(opts, p.namespace, p.agentImage)

	created, err := p.clientset.BatchV1().Jobs(p.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	// waitForPodRunning caches the pod name for later lookups.
	if _, err := waitForPodRunning(ctx, p.clientset, p.namespace, created.Name, p.podCache); err != nil {
		_ = p.Cleanup(ctx, created.Name)
		return nil, fmt.Errorf("failed waiting for pod: %w", err)
	}

	return &sandbox.Sandbox{
		ID:         created.Name,
		Provider:   "kubernetes",
		WorkingDir: opts.WorkingDir,
	}, nil
}

// Exec executes a command in the sandbox pod.
//
// Kubernetes exec limitations vs Docker:
//   - User: Ignored (pod runs as UID 1000, cannot switch per-exec)
//   - WorkingDir: Supported via shell wrapper when specified
//   - Env: Ignored (set at pod provision time, cannot override per-exec)
//   - Timeout: Ignored (use context cancellation instead)
func (p *Provider) Exec(ctx context.Context, id string, cmd sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}

	// K8s exec doesn't support User switching - pod runs as configured user (UID 1000).
	// Silently ignore User field since pod is already configured correctly.

	// Support WorkingDir via shell wrapper if specified
	execCmd := cmd.Command
	if cmd.WorkingDir != "" {
		// Wrap command to cd into working directory first
		shell := fmt.Sprintf("cd %q && exec \"$@\"", cmd.WorkingDir)
		execCmd = append([]string{"sh", "-c", shell, "--"}, cmd.Command...)
	}

	// Env and Timeout are not supported - document in function comment

	return execCommand(ctx, p.clientset, p.restConfig, p.namespace, podName, mainContainerName, execCmd, nil)
}

// ExecShell executes a shell command in the sandbox pod.
func (p *Provider) ExecShell(ctx context.Context, id string, command string, user string) (*sandbox.ExecResult, error) {
	return p.Exec(ctx, id, sandbox.ExecCommand{
		Command: []string{"bash", "-c", command},
		User:    user,
	})
}

// CopyTo writes data into the sandbox pod via stdin pipe.
func (p *Provider) CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return err
	}

	// Bounded read to prevent OOM.
	limited := io.LimitReader(src, MaxFileReadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("failed to read source data: %w", err)
	}
	if len(data) > MaxFileReadSize {
		return fmt.Errorf("source data exceeds maximum size (%d bytes)", MaxFileReadSize)
	}

	return execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, destPath, data)
}

// CopyFrom reads a file from the sandbox pod.
func (p *Provider) CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}

	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, srcPath)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("file not found: %s", srcPath)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// Status returns the current sandbox pod status.
func (p *Provider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return &sandbox.SandboxStatus{
			Phase:   sandbox.SandboxPhaseUnknown,
			Message: fmt.Sprintf("pod not found: %v", err),
		}, nil
	}

	pod, err := p.clientset.CoreV1().Pods(p.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return &sandbox.SandboxStatus{
				Phase:   sandbox.SandboxPhaseUnknown,
				Message: "pod not found",
			}, nil
		}
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	phase, message := mapPodPhase(pod.Status.Phase, pod.Status.Message)
	return &sandbox.SandboxStatus{
		Phase:   phase,
		Message: message,
	}, nil
}

// Cleanup deletes the Job and its pods.
func (p *Provider) Cleanup(ctx context.Context, id string) error {
	propagation := metav1.DeletePropagationForeground
	err := p.clientset.BatchV1().Jobs(p.namespace).Delete(ctx, id, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete job %s: %w", id, err)
	}

	clearPodCache(id, p.podCache)
	return nil
}

// SubmitManifest writes the task manifest to the sandbox.
func (p *Provider) SubmitManifest(ctx context.Context, id string, manifest []byte) error {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return err
	}

	return execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.ManifestPath, manifest)
}

// PollStatus reads the agent's current status from the sandbox.
func (p *Provider) PollStatus(ctx context.Context, id string) (*protocol.AgentStatus, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}

	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.StatusPath)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return &protocol.AgentStatus{
			Phase:   protocol.PhaseInitializing,
			Message: "Waiting for agent to start",
		}, nil
	}

	var status protocol.AgentStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to parse status.json: %w", err)
	}
	return &status, nil
}

// ReadResult reads the agent's full result from the sandbox.
func (p *Provider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}

	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.ResultPath)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("result.json not found")
	}
	return data, nil
}

// SubmitSteering writes a steering instruction for the agent.
func (p *Provider) SubmitSteering(ctx context.Context, id string, instruction []byte) error {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return err
	}

	return execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.SteeringPath, instruction)
}

// mapPodPhase converts a Kubernetes pod phase to a sandbox phase.
func mapPodPhase(phase corev1.PodPhase, message string) (sandbox.SandboxPhase, string) {
	switch phase {
	case corev1.PodPending:
		return sandbox.SandboxPhasePending, "pod is pending"
	case corev1.PodRunning:
		return sandbox.SandboxPhaseRunning, "pod is running"
	case corev1.PodSucceeded:
		return sandbox.SandboxPhaseSucceeded, "pod completed successfully"
	case corev1.PodFailed:
		msg := "pod failed"
		if message != "" {
			msg = fmt.Sprintf("pod failed: %s", message)
		}
		return sandbox.SandboxPhaseFailed, msg
	default:
		return sandbox.SandboxPhaseUnknown, fmt.Sprintf("unknown phase: %s", phase)
	}
}
