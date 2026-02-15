package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// MaxFileReadSize is the maximum bytes to read from sandbox files.
const MaxFileReadSize = 10 << 20 // 10 MB

// execCommand runs a command in a pod container via SPDY exec.
func execCommand(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, container string, cmd []string, stdin io.Reader) (*sandbox.ExecResult, error) {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	streamOpts := remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	}

	err = executor.StreamWithContext(ctx, streamOpts)
	if err != nil {
		// Extract exit code from exec error if available.
		if exitErr, ok := err.(utilexec.ExitError); ok {
			return &sandbox.ExecResult{
				ExitCode: exitErr.ExitStatus(),
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
			}, nil
		}
		return nil, fmt.Errorf("exec stream failed: %w", err)
	}

	return &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// execReadFile reads a file from a pod using cat. Returns nil, nil if the file doesn't exist.
func execReadFile(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, filePath string) ([]byte, error) {
	result, err := execCommand(ctx, clientset, restConfig, namespace, podName, mainContainerName,
		[]string{"cat", filePath}, nil)
	if err != nil {
		return nil, err
	}

	if result.ExitCode != 0 {
		return nil, nil
	}
	if len(result.Stdout) > MaxFileReadSize {
		return nil, fmt.Errorf("file %s exceeds maximum size (%d bytes)", filePath, MaxFileReadSize)
	}

	return []byte(result.Stdout), nil
}

// execWriteFile writes data to a file in a pod using stdin pipe.
func execWriteFile(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, filePath string, data []byte) error {
	dir := path.Dir(filePath)

	// Create directory without shell interpolation
	mkdirResult, err := execCommand(ctx, clientset, restConfig, namespace, podName, mainContainerName,
		[]string{"mkdir", "-p", dir}, nil)
	if err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	if mkdirResult.ExitCode != 0 {
		return fmt.Errorf("create directory %s failed (exit %d): %s", dir, mkdirResult.ExitCode, mkdirResult.Stderr)
	}

	// Write file using positional parameter to avoid injection
	result, err := execCommand(ctx, clientset, restConfig, namespace, podName, mainContainerName,
		[]string{"sh", "-c", "cat > \"$1\"", "--", filePath}, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("write file %s: %w", filePath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write file %s failed (exit %d): %s", filePath, result.ExitCode, result.Stderr)
	}

	return nil
}
