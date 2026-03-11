package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PodInfo holds the name and main container of a running pod.
type PodInfo struct {
	Name      string
	Container string
}

// sidecarNames is the set of well-known sidecar container names to skip when
// selecting the "main" application container.
var sidecarNames = map[string]bool{
	"istio-proxy":   true,
	"envoy":         true,
	"linkerd-proxy": true,
	"istio-init":    true,
}

// mainContainer returns the first non-sidecar container name in the pod spec.
func mainContainer(pod corev1.Pod) string {
	for _, c := range pod.Spec.Containers {
		if !sidecarNames[c.Name] {
			return c.Name
		}
	}
	if len(pod.Spec.Containers) > 0 {
		return pod.Spec.Containers[0].Name
	}
	return ""
}

// ResolveServiceToPod finds a running pod for the given Service's selector labels.
// Returns podName, containerName, and the targetPort number for the service's first port.
func ResolveServiceToPod(ctx context.Context, cfg *rest.Config, namespace, serviceName string) (string, string, int, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", "", 0, fmt.Errorf("creating clientset: %w", err)
	}

	svc, err := cs.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return "", "", 0, fmt.Errorf("getting service %s: %w", serviceName, err)
	}

	if len(svc.Spec.Ports) == 0 {
		return "", "", 0, fmt.Errorf("service %s has no ports", serviceName)
	}
	targetPort := int(svc.Spec.Ports[0].TargetPort.IntVal)
	if targetPort == 0 {
		targetPort = int(svc.Spec.Ports[0].Port)
	}

	if len(svc.Spec.Selector) == 0 {
		return "", "", 0, fmt.Errorf("service %s has no selector", serviceName)
	}

	sel := labels.Set(svc.Spec.Selector).AsSelector()
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: sel.String(),
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("listing pods for service %s: %w", serviceName, err)
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					return pod.Name, mainContainer(pod), targetPort, nil
				}
			}
		}
	}

	return "", "", 0, fmt.Errorf("no running/ready pod found for service %s", serviceName)
}

// ListPodsForService returns all running/ready pods for the given service,
// with their main container names.
func ListPodsForService(ctx context.Context, cfg *rest.Config, namespace, serviceName string) ([]PodInfo, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}

	svc, err := cs.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting service %s: %w", serviceName, err)
	}

	if len(svc.Spec.Selector) == 0 {
		return nil, fmt.Errorf("service %s has no selector", serviceName)
	}

	sel := labels.Set(svc.Spec.Selector).AsSelector()
	podList, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: sel.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	var result []PodInfo
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			result = append(result, PodInfo{
				Name:      pod.Name,
				Container: mainContainer(pod),
			})
		}
	}
	return result, nil
}

// StartPortForward establishes a port-forward stream to a pod and blocks until ctx is cancelled.
func StartPortForward(ctx context.Context, cfg *rest.Config, namespace, podName string, localPort, remotePort int, out, errOut io.Writer) error {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating clientset: %w", err)
	}

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return fmt.Errorf("creating SPDY round tripper: %w", err)
	}

	serverURL, err := url.Parse(cfg.Host)
	if err != nil {
		return fmt.Errorf("parsing host URL: %w", err)
	}
	_ = cs // clientset used for validation above

	serverURL.Path = fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)

	stopCh := ctx.Done()
	readyCh := make(chan struct{})

	fw, err := portforward.New(dialer,
		[]string{fmt.Sprintf("%d:%d", localPort, remotePort)},
		stopCh, readyCh, out, errOut,
	)
	if err != nil {
		return fmt.Errorf("creating port forwarder: %w", err)
	}

	return fw.ForwardPorts()
}
