package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

func TestBuildJobSpec_Labels(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-123",
		Image:  "ubuntu:22.04",
	}
	job := buildJobSpec(opts, "sandbox-isolated", "fleetlift-agent:latest")

	assert.Equal(t, "fleetlift-sandbox-task-123", job.Name)
	assert.Equal(t, "sandbox-isolated", job.Namespace)
	assert.Equal(t, "task-123", job.Labels[labelTaskID])
	assert.Equal(t, "fleetlift", job.Labels[labelManagedBy])
	assert.Equal(t, "task-123", job.Spec.Template.Labels[labelTaskID])
}

func TestBuildJobSpec_InitContainer(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID:       "task-456",
		Image:        "ubuntu:22.04",
		UseAgentMode: true,
	}
	job := buildJobSpec(opts, "sandbox-isolated", "my-agent:v1")

	require.Len(t, job.Spec.Template.Spec.InitContainers, 1)
	init := job.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, initContainerName, init.Name)
	assert.Equal(t, "my-agent:v1", init.Image)
	assert.Contains(t, init.Command, "cp")

	// Verify shared volume mount.
	require.Len(t, init.VolumeMounts, 1)
	assert.Equal(t, agentBinVolume, init.VolumeMounts[0].Name)
	assert.Equal(t, agentBinMountPath, init.VolumeMounts[0].MountPath)
}

func TestBuildJobSpec_AgentMode(t *testing.T) {
	t.Run("agent mode runs agent binary", func(t *testing.T) {
		opts := sandbox.ProvisionOptions{
			TaskID:       "task-1",
			Image:        "ubuntu:22.04",
			UseAgentMode: true,
		}
		job := buildJobSpec(opts, "test-ns", "agent:v1")

		container := job.Spec.Template.Spec.Containers[0]
		assert.Equal(t, mainContainerName, container.Name)
		assert.Equal(t, []string{"/agent-bin/fleetlift-agent", "serve"}, container.Command)
	})

	t.Run("non-agent mode runs idle", func(t *testing.T) {
		opts := sandbox.ProvisionOptions{
			TaskID: "task-2",
			Image:  "ubuntu:22.04",
		}
		job := buildJobSpec(opts, "test-ns", "agent:v1")

		container := job.Spec.Template.Spec.Containers[0]
		assert.Equal(t, []string{"sh", "-c", "tail -f /dev/null"}, container.Command)
	})
}

func TestBuildJobSpec_SecurityContext(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-sec",
		Image:  "ubuntu:22.04",
	}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")

	podSec := job.Spec.Template.Spec.SecurityContext
	require.NotNil(t, podSec)
	assert.True(t, *podSec.RunAsNonRoot)
	assert.Equal(t, int64(1000), *podSec.RunAsUser)
	assert.Equal(t, int64(1000), *podSec.FSGroup)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, podSec.SeccompProfile.Type)

	assert.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)

	containerSec := job.Spec.Template.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSec)
	assert.Equal(t, []corev1.Capability{"ALL"}, containerSec.Capabilities.Drop)
}

func TestBuildJobSpec_ServiceAccount(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "test",
		Image:  "ubuntu:22.04",
	}
	job := buildJobSpec(opts, "test-ns", "agent:v1")

	assert.Equal(t, "sandbox-runner", job.Spec.Template.Spec.ServiceAccountName)
	assert.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)
}

func TestBuildJobSpec_Resources(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-res",
		Image:  "ubuntu:22.04",
		Resources: sandbox.ResourceLimits{
			MemoryBytes: 4 * 1024 * 1024 * 1024, // 4Gi
			CPUQuota:    200000,                 // 2 CPUs
		},
	}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")

	limits := job.Spec.Template.Spec.Containers[0].Resources.Limits
	require.NotNil(t, limits)

	mem := limits[corev1.ResourceMemory]
	assert.Equal(t, int64(4*1024*1024*1024), mem.Value())

	cpu := limits[corev1.ResourceCPU]
	assert.Equal(t, int64(2000), cpu.MilliValue())
}

func TestBuildJobSpec_BackoffLimit(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-bo",
		Image:  "ubuntu:22.04",
	}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")

	require.NotNil(t, job.Spec.BackoffLimit)
	assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
}

func TestBuildJobSpec_EnvVars(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-env",
		Image:  "ubuntu:22.04",
		Env: map[string]string{
			"FOO": "bar",
			"BAZ": "qux",
		},
	}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")

	envVars := job.Spec.Template.Spec.Containers[0].Env
	envMap := make(map[string]string)
	for _, e := range envVars {
		envMap[e.Name] = e.Value
	}
	assert.Equal(t, "bar", envMap["FOO"])
	assert.Equal(t, "qux", envMap["BAZ"])
}

func TestBuildJobSpec_RestartPolicy(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-rp",
		Image:  "ubuntu:22.04",
	}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")

	assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
}

func TestProvision_CreatesJob(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

	// Pre-create a pod so waitForPodRunning finds it.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fleetlift-sandbox-task-1-abc",
			Namespace: "test-ns",
			Labels:    map[string]string{"job-name": "fleetlift-sandbox-task-1"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	_, err := clientset.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	ctx := context.Background()
	sb, err := provider.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:       "task-1",
		Image:        "ubuntu:22.04",
		UseAgentMode: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "fleetlift-sandbox-task-1", sb.ID)
	assert.Equal(t, "kubernetes", sb.Provider)

	// Verify the job was created.
	job, err := clientset.BatchV1().Jobs("test-ns").Get(ctx, "fleetlift-sandbox-task-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "task-1", job.Labels[labelTaskID])
}

func TestCleanup_Idempotent(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

	// Cleanup a non-existent job should not error.
	err := provider.Cleanup(context.Background(), "non-existent-job")
	assert.NoError(t, err)
}

func TestCleanup_DeletesJob(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

	// Create a job first.
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fleetlift-sandbox-task-2",
			Namespace: "test-ns",
		},
	}
	_, err := clientset.BatchV1().Jobs("test-ns").Create(context.Background(), job, metav1.CreateOptions{})
	require.NoError(t, err)

	// Cleanup should succeed.
	err = provider.Cleanup(context.Background(), "fleetlift-sandbox-task-2")
	assert.NoError(t, err)

	// Job should be deleted.
	_, err = clientset.BatchV1().Jobs("test-ns").Get(context.Background(), "fleetlift-sandbox-task-2", metav1.GetOptions{})
	assert.True(t, err != nil)
}

func TestStatus_PodPhases(t *testing.T) {
	tests := []struct {
		name          string
		podPhase      corev1.PodPhase
		expectedPhase sandbox.SandboxPhase
	}{
		{"pending", corev1.PodPending, sandbox.SandboxPhasePending},
		{"running", corev1.PodRunning, sandbox.SandboxPhaseRunning},
		{"succeeded", corev1.PodSucceeded, sandbox.SandboxPhaseSucceeded},
		{"failed", corev1.PodFailed, sandbox.SandboxPhaseFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobName := "fleetlift-sandbox-status-" + tt.name
			//nolint:staticcheck
			clientset := fake.NewSimpleClientset()
			provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName + "-pod",
					Namespace: "test-ns",
					Labels:    map[string]string{"job-name": jobName},
				},
				Status: corev1.PodStatus{Phase: tt.podPhase},
			}
			_, err := clientset.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
			require.NoError(t, err)

			status, err := provider.Status(context.Background(), jobName)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPhase, status.Phase)
		})
	}
}

func TestStatus_NoPod(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

	status, err := provider.Status(context.Background(), "non-existent-job")
	require.NoError(t, err)
	assert.Equal(t, sandbox.SandboxPhaseUnknown, status.Phase)
}

func TestExec_WorkingDir(t *testing.T) {
	// This is tested in integration tests (TestIntegration_ShellMetacharactersInFilePaths)
	// Unit testing WorkingDir requires mocking the K8s exec API which is complex.
	// Instead, we verify the logic by inspecting the code path:
	// - WorkingDir creates a shell wrapper: cd "$dir" && exec "$@"
	// - Empty WorkingDir passes through the command unchanged
	// This is verified in integration tests with actual K8s pods.
	t.Skip("WorkingDir support is tested in integration tests")
}

func TestName(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")
	assert.Equal(t, "kubernetes", provider.Name())
}

func TestMapPodPhase(t *testing.T) {
	phase, msg := mapPodPhase(corev1.PodRunning, "")
	assert.Equal(t, sandbox.SandboxPhaseRunning, phase)
	assert.Equal(t, "pod is running", msg)

	phase, msg = mapPodPhase(corev1.PodFailed, "OOMKilled")
	assert.Equal(t, sandbox.SandboxPhaseFailed, phase)
	assert.Contains(t, msg, "OOMKilled")

	phase, _ = mapPodPhase("SomeUnknownPhase", "")
	assert.Equal(t, sandbox.SandboxPhaseUnknown, phase)
}
