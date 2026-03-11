package k8s

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"time"

	"k8s.io/client-go/rest"
)


type HealthStatus int

const (
	HealthUnknown  HealthStatus = iota
	HealthOK
	HealthDegraded
)

type TunnelStatus int

const (
	StatusConnecting   TunnelStatus = iota
	StatusRunning      TunnelStatus = iota
	StatusReconnecting TunnelStatus = iota
	StatusError        TunnelStatus = iota
	StatusPaused       TunnelStatus = iota
	StatusStopped      TunnelStatus = iota
)

func (s TunnelStatus) String() string {
	switch s {
	case StatusConnecting:
		return "connecting"
	case StatusRunning:
		return "running"
	case StatusReconnecting:
		return "reconnecting"
	case StatusError:
		return "error"
	case StatusPaused:
		return "paused"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type TunnelEvent struct {
	ID         string
	Status     TunnelStatus
	RetryCount int
	Err        error
}

type TunnelConfig struct {
	ServiceName     string
	Namespace       string
	LocalPort       int
	RemotePort      int
	MaxRetries      int
	BackoffSecs     int
	// Stored for offline resume (from history).
	ClusterEndpoint string
	ClusterCAData   []byte
	ClusterName     string
	ClusterEnvName  string
}

type Tunnel struct {
	ID         string
	Namespace  string
	LocalPort  int
	RemotePort int
	Status     TunnelStatus
	RetryCount int
	MaxRetries int
	backoff    int

	// Stored for Resume() calls and registry operations.
	ClusterEndpoint string
	ClusterName     string
	ClusterEnvName  string
	clusterCAData   []byte
	restCfg         *rest.Config

	// Runtime info updated as tunnel runs.
	StartedAt        time.Time
	CurrentPod       string
	CurrentContainer string
	Health           HealthStatus
	HealthCheckedAt  time.Time

	cancel   context.CancelFunc
	statusCh chan<- TunnelEvent
}

func NewTunnel(cfg TunnelConfig, statusCh chan<- TunnelEvent) *Tunnel {
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	backoff := cfg.BackoffSecs
	if backoff == 0 {
		backoff = 2
	}
	return &Tunnel{
		ID:              cfg.ServiceName,
		Namespace:       cfg.Namespace,
		LocalPort:       cfg.LocalPort,
		RemotePort:      cfg.RemotePort,
		Status:          StatusStopped,
		MaxRetries:      maxRetries,
		backoff:         backoff,
		ClusterEndpoint: cfg.ClusterEndpoint,
		clusterCAData:   cfg.ClusterCAData,
		ClusterName:     cfg.ClusterName,
		ClusterEnvName:  cfg.ClusterEnvName,
		statusCh:        statusCh,
	}
}

// Start begins the reconnect loop. Stores restCfg for future Resume() calls.
func (t *Tunnel) Start(ctx context.Context, restCfg *rest.Config) {
	t.restCfg = restCfg
	t.RetryCount = 0
	t.Health = HealthUnknown
	childCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	go t.runWithReconnect(childCtx, restCfg)
}

// Stop cancels the goroutine and emits StatusStopped.
// The tunnel is removed from the UI — use Pause to keep it visible.
func (t *Tunnel) Stop() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// Pause stops forwarding but keeps the tunnel entry visible with StatusPaused.
func (t *Tunnel) Pause() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.sendEvent(StatusPaused, nil)
}

// Resume restarts a paused or errored tunnel. It rebuilds restCfg from stored
// cluster info if needed.
func (t *Tunnel) Resume(ctx context.Context) error {
	restCfg := t.restCfg
	if restCfg == nil {
		if t.ClusterEndpoint == "" {
			return fmt.Errorf("no cluster info stored for tunnel %s", t.ID)
		}
		var err error
		restCfg, err = BuildRESTConfigFromParts(ctx, t.ClusterEndpoint, t.clusterCAData)
		if err != nil {
			return fmt.Errorf("rebuilding REST config: %w", err)
		}
	}
	t.Start(ctx, restCfg)
	return nil
}

// RESTConfigOrBuild returns the stored REST config, or builds one from stored
// cluster credentials if the tunnel has never been started.
// Returns (nil, nil) when no credentials are stored — not an error.
func (t *Tunnel) RESTConfigOrBuild(ctx context.Context) (*rest.Config, error) {
	if t.restCfg != nil {
		return t.restCfg, nil
	}
	if t.ClusterEndpoint == "" {
		return nil, nil
	}
	cfg, err := BuildRESTConfigFromParts(ctx, t.ClusterEndpoint, t.clusterCAData)
	if err != nil {
		return nil, fmt.Errorf("building REST config: %w", err)
	}
	return cfg, nil
}

func (t *Tunnel) sendEvent(status TunnelStatus, err error) {
	t.Status = status
	if t.statusCh != nil {
		t.statusCh <- TunnelEvent{
			ID:         t.ID,
			Status:     status,
			RetryCount: t.RetryCount,
			Err:        err,
		}
	}
}

func (t *Tunnel) runWithReconnect(ctx context.Context, restCfg *rest.Config) {
	for {
		select {
		case <-ctx.Done():
			// Only emit stopped if we weren't already paused.
			if t.Status != StatusPaused {
				t.sendEvent(StatusStopped, nil)
			}
			return
		default:
		}

		t.sendEvent(StatusConnecting, nil)

		podName, containerName, targetPort, err := ResolveServiceToPod(ctx, restCfg, t.Namespace, t.ID)
		if err != nil {
			t.RetryCount++
			if t.RetryCount > t.MaxRetries {
				t.sendEvent(StatusError, fmt.Errorf("resolving pod: %w", err))
				return
			}
			t.sendEvent(StatusReconnecting, err)
			t.sleepBackoff(ctx)
			continue
		}

		remotePort := t.RemotePort
		if remotePort == 0 {
			remotePort = targetPort
		}

		t.CurrentPod = podName
		t.CurrentContainer = containerName
		t.StartedAt = time.Now()
		var outBuf, errBuf bytes.Buffer
		t.sendEvent(StatusRunning, nil)

		forwardErr := StartPortForward(ctx, restCfg, t.Namespace, podName, t.LocalPort, remotePort, &outBuf, &errBuf)

		select {
		case <-ctx.Done():
			if t.Status != StatusPaused {
				t.sendEvent(StatusStopped, nil)
			}
			return
		default:
		}

		if forwardErr != nil {
			t.RetryCount++
			if t.RetryCount > t.MaxRetries {
				t.sendEvent(StatusError, fmt.Errorf("port forward failed: %w", forwardErr))
				return
			}
			t.sendEvent(StatusReconnecting, forwardErr)
			t.sleepBackoff(ctx)
		}
	}
}

func (t *Tunnel) sleepBackoff(ctx context.Context) {
	delay := time.Duration(float64(t.backoff)*math.Pow(2, float64(t.RetryCount-1))) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}
