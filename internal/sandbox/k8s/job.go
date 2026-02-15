// Package k8s provides a Kubernetes-based sandbox provider using Jobs.
package k8s

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

const (
	agentBinVolume    = "agent-bin"
	agentBinMountPath = "/agent-bin"
	mainContainerName = "sandbox"
	initContainerName = "inject-agent"
	labelTaskID       = "fleetlift.io/task-id"
	labelManagedBy    = "app.kubernetes.io/managed-by"
)

// buildJobSpec creates a Kubernetes Job spec for a sandbox.
func buildJobSpec(opts sandbox.ProvisionOptions, namespace, agentImage string) *batchv1.Job {
	labels := map[string]string{
		labelTaskID:    opts.TaskID,
		labelManagedBy: "fleetlift",
	}

	cmd := []string{"sh", "-c", "tail -f /dev/null"}
	if opts.UseAgentMode {
		cmd = []string{agentBinMountPath + "/fleetlift-agent", "serve"}
	}

	agentBinMount := corev1.VolumeMount{Name: agentBinVolume, MountPath: agentBinMountPath}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("fleetlift-sandbox-%s", opts.TaskID),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr(int32(0)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           "sandbox-runner",
					AutomountServiceAccountToken: ptr(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr(true),
						RunAsUser:    ptr(int64(1000)),
						FSGroup:      ptr(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:    initContainerName,
							Image:   agentImage,
							Command: []string{"cp", "/usr/local/bin/fleetlift-agent", agentBinMountPath + "/fleetlift-agent"},
							VolumeMounts: []corev1.VolumeMount{agentBinMount},
						},
					},
					Containers: []corev1.Container{
						{
							Name:      mainContainerName,
							Image:     opts.Image,
							Command:   cmd,
							Env:       buildEnvVars(opts.Env),
							Resources: buildResourceLimits(opts.Resources),
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{agentBinMount},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: agentBinVolume,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

// buildEnvVars converts a string map to Kubernetes EnvVar slice.
func buildEnvVars(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	vars := make([]corev1.EnvVar, 0, len(env))
	for k, v := range env {
		vars = append(vars, corev1.EnvVar{Name: k, Value: v})
	}
	return vars
}

// buildResourceLimits converts sandbox ResourceLimits to Kubernetes resource requirements.
func buildResourceLimits(res sandbox.ResourceLimits) corev1.ResourceRequirements {
	if res.MemoryBytes <= 0 && res.CPUQuota <= 0 {
		return corev1.ResourceRequirements{}
	}

	limits := corev1.ResourceList{}
	if res.MemoryBytes > 0 {
		limits[corev1.ResourceMemory] = *resource.NewQuantity(res.MemoryBytes, resource.BinarySI)
	}
	if res.CPUQuota > 0 {
		// CPUQuota is in units of 1/100000 CPU. Convert to millicores.
		limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(res.CPUQuota/100, resource.DecimalSI)
	}
	return corev1.ResourceRequirements{Limits: limits}
}

func ptr[T any](v T) *T {
	return &v
}
