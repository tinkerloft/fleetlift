package k8s

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

func jobLabelSelector(jobName string) string {
	return fmt.Sprintf("job-name=%s", jobName)
}

// waitForPodRunning watches pods for a job until one reaches the Running phase.
// The context deadline controls the timeout.
func waitForPodRunning(ctx context.Context, clientset kubernetes.Interface, namespace, jobName string, cache *sync.Map) (string, error) {
	selector := jobLabelSelector(jobName)

	// Check if a pod is already running.
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			cache.Store(jobName, pod.Name)
			return pod.Name, nil
		}
	}

	watcher, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector:   selector,
		ResourceVersion: pods.ResourceVersion,
	})
	if err != nil {
		return "", fmt.Errorf("failed to watch pods for job %s: %w", jobName, err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for pod to be running: %w", ctx.Err())
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return "", fmt.Errorf("watch channel closed for job %s", jobName)
			}
			if event.Type == watch.Error {
				return "", fmt.Errorf("watch error for job %s", jobName)
			}

			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}

			switch pod.Status.Phase {
			case corev1.PodRunning:
				cache.Store(jobName, pod.Name)
				return pod.Name, nil
			case corev1.PodFailed:
				return "", fmt.Errorf("pod %s failed: %s", pod.Name, pod.Status.Message)
			case corev1.PodSucceeded:
				return "", fmt.Errorf("pod %s completed before reaching running state", pod.Name)
			}
		}
	}
}

// findPodForJob returns the pod name for a job, using the cache.
func findPodForJob(ctx context.Context, clientset kubernetes.Interface, namespace, jobName string, cache *sync.Map) (string, error) {
	if cached, ok := cache.Load(jobName); ok {
		return cached.(string), nil
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: jobLabelSelector(jobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s", jobName)
	}

	podName := pods.Items[0].Name
	cache.Store(jobName, podName)
	return podName, nil
}

func clearPodCache(jobName string, cache *sync.Map) {
	cache.Delete(jobName)
}
