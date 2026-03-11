package tui

import (
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	corev1 "k8s.io/api/core/v1"

	"github.com/geraldofigueiredo/kportmaster/internal/gcp"
	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
	"github.com/geraldofigueiredo/kportmaster/internal/profiles"
)

type MsgClustersLoaded struct {
	Clusters []gcp.Cluster
	Err      error
}

type MsgNamespacesLoaded struct {
	Namespaces []string
	Err        error
}

type MsgServicesLoaded struct {
	Services []corev1.Service
	Err      error
}

type MsgTunnelEvent struct {
	ID     string
	Status k8s.TunnelStatus
	Retry  int
	Err    error
}

type MsgLogEntry struct {
	Level string
	Text  string
	Time  time.Time
}

type MsgWindowResize struct {
	Width  int
	Height int
}

type MsgWizardDone struct {
	Project   string
	Cluster   gcp.Cluster
	Namespace string
	Services  []corev1.Service
}

type MsgWizardCancelled struct{}

// MsgHealthResult carries the result of a TCP health probe for a tunnel.
type MsgHealthResult struct {
	ID      string
	Healthy bool
}

// MsgProfileLoad requests that all services in a profile be started.
type MsgProfileLoad struct {
	Profile profiles.Profile
}

// MsgSaveProfileAs requests saving the current registry as a named profile.
type MsgSaveProfileAs struct {
	Name string
}

// MsgServiceLogStreamReady carries the opened log stream for a tunnel.
type MsgServiceLogStreamReady struct {
	TunnelID string
	Stream   io.ReadCloser
}

// MsgServiceLogLine carries a single log line from a running pod.
// next is the cmd to read the following line (captures scanner in closure).
type MsgServiceLogLine struct {
	TunnelID string
	Line     string
	next     tea.Cmd
}

// MsgServiceLogsEnded signals that the log stream for a tunnel has ended.
type MsgServiceLogsEnded struct {
	TunnelID string
}

// MsgPodsLoaded carries the list of running pods for the selected tunnel's service.
type MsgPodsLoaded struct {
	TunnelID string
	Pods     []k8s.PodInfo
}

// MsgServiceLogStreamErr carries an error from a failed log stream attempt.
type MsgServiceLogStreamErr struct {
	TunnelID string
	Err      error
}

// MsgClearCopyFeedback clears the "✓ copied" feedback line in the detail overlay.
type MsgClearCopyFeedback struct{}
