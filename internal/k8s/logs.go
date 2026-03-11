package k8s

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// RESTConfig returns the stored *rest.Config (set by Start()).
func (t *Tunnel) RESTConfig() *rest.Config { return t.restCfg }

// StreamPodLogs opens a following log stream for the pod.
// Returns an io.ReadCloser; caller must close it (done via ctx cancellation).
func StreamPodLogs(ctx context.Context, restCfg *rest.Config, namespace, podName, containerName string, tailLines int64) (io.ReadCloser, error) {
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}
	opts := &corev1.PodLogOptions{
		Follow:    true,
		TailLines: &tailLines,
	}
	if containerName != "" {
		opts.Container = containerName
	}
	stream, err := cs.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening log stream: %w", err)
	}
	return stream, nil
}
