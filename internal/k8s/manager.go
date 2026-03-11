package k8s

import (
	"context"
	"sort"
	"sync"
)

// TunnelManager is a thread-safe registry of active tunnels.
type TunnelManager struct {
	mu      sync.RWMutex
	tunnels map[string]*Tunnel
}

func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make(map[string]*Tunnel),
	}
}

func (m *TunnelManager) Add(t *Tunnel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels[t.ID] = t
}

// Stop cancels the tunnel goroutine (emits StatusStopped). Use Remove to delete from registry.
func (m *TunnelManager) Stop(id string) {
	m.mu.RLock()
	t, ok := m.tunnels[id]
	m.mu.RUnlock()
	if ok {
		t.Stop()
	}
}

// Pause stops forwarding but keeps the tunnel visible as StatusPaused.
func (m *TunnelManager) Pause(id string) {
	m.mu.RLock()
	t, ok := m.tunnels[id]
	m.mu.RUnlock()
	if ok {
		t.Pause()
	}
}

// Resume restarts a paused or errored tunnel.
func (m *TunnelManager) Resume(id string, ctx context.Context) error {
	m.mu.RLock()
	t, ok := m.tunnels[id]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return t.Resume(ctx)
}

func (m *TunnelManager) StopAll() {
	m.mu.RLock()
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.mu.RUnlock()
	for _, t := range tunnels {
		t.Stop()
	}
}

// PauseAll pauses every tunnel that is currently active (running/connecting/reconnecting).
func (m *TunnelManager) PauseAll() {
	m.mu.RLock()
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.mu.RUnlock()
	for _, t := range tunnels {
		switch t.Status {
		case StatusRunning, StatusConnecting, StatusReconnecting:
			t.Pause()
		}
	}
}

// ResumeAll restarts every tunnel that is paused, stopped, or errored.
func (m *TunnelManager) ResumeAll(ctx context.Context) []error {
	m.mu.RLock()
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.mu.RUnlock()
	var errs []error
	for _, t := range tunnels {
		switch t.Status {
		case StatusPaused, StatusStopped, StatusError:
			if err := t.Resume(ctx); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// RemoveAll stops and removes every tunnel from the registry.
func (m *TunnelManager) RemoveAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tunnels {
		t.Stop()
	}
	m.tunnels = make(map[string]*Tunnel)
}

// Remove deletes a tunnel entry from the registry (after stopping it).
func (m *TunnelManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tunnels[id]; ok {
		t.Stop()
	}
	delete(m.tunnels, id)
}

func (m *TunnelManager) List() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	sort.Slice(tunnels, func(i, j int) bool {
		return tunnels[i].ID < tunnels[j].ID
	})
	return tunnels
}

func (m *TunnelManager) Get(id string) (*Tunnel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tunnels[id]
	return t, ok
}
