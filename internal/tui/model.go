package tui

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geraldofigueiredo/kportmaster/internal/config"
	"github.com/geraldofigueiredo/kportmaster/internal/history"
	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
	"github.com/geraldofigueiredo/kportmaster/internal/portmgr"
	"github.com/geraldofigueiredo/kportmaster/internal/profiles"
	"github.com/geraldofigueiredo/kportmaster/internal/registry"
)

type FocusArea int

const (
	FocusForwards    FocusArea = iota
	FocusServiceLogs FocusArea = iota
	FocusLogs        FocusArea = iota
)

// MsgHistoryLoaded carries tunnels pre-populated from history.
type MsgHistoryLoaded struct {
	Tunnels []*k8s.Tunnel
}

// confirmDialog holds state for a pending yes/no confirmation.
type confirmDialog struct {
	active  bool
	message string
	onYes   tea.Cmd
}

// portEditDialog holds state for the inline port-edit modal.
type portEditDialog struct {
	active     bool
	tunnelID   string
	wasRunning bool
	input      textinput.Model
}

// Model is the root Bubble Tea model.
type Model struct {
	width  int
	height int

	cfg     *config.Config
	manager *k8s.TunnelManager

	tunnelEventCh chan k8s.TunnelEvent

	wizard      WizardModel
	profilesMdl ProfilesModel
	menuMdl     MenuModel
	forwards    ForwardsModel
	detail      DetailModel
	cluster     ClusterSummaryModel
	statusBar   StatusBarModel
	logs        LogsModel
	serviceLogs ServiceLogsModel
	help        HelpModel

	showLogs        bool
	focus           FocusArea
	confirm         confirmDialog
	portEdit        portEditDialog
	serviceLogCancel context.CancelFunc
}

func NewModel(cfg *config.Config) Model {
	ch := make(chan k8s.TunnelEvent, 64)
	manager := k8s.NewTunnelManager()

	return Model{
		cfg:           cfg,
		manager:       manager,
		tunnelEventCh: ch,
		wizard:        NewWizardModel(cfg),
		profilesMdl:   NewProfilesModel(),
		forwards:      NewForwardsModel(48, 20, true),
		detail:        NewDetailModel(32, 20),
		cluster:       NewClusterSummaryModel(80, 3),
		statusBar:     NewStatusBarModel(80),
		logs:          NewLogsModel(80, 5),
		serviceLogs:   NewServiceLogsModel(80, 10),
		help:          NewHelpModel(),
		showLogs:      true,
		focus:         FocusForwards,
	}
}

func (m Model) Manager() *k8s.TunnelManager {
	return m.manager
}

func listenTunnelEvents(ch chan k8s.TunnelEvent) tea.Cmd {
	return func() tea.Msg {
		event := <-ch
		return MsgTunnelEvent{
			ID:     event.ID,
			Status: event.Status,
			Retry:  event.RetryCount,
			Err:    event.Err,
		}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		listenTunnelEvents(m.tunnelEventCh),
		loadRegistryTunnels(m.tunnelEventCh, m.cfg),
	)
}

// loadRegistryTunnels reads the persistent registry and creates a stopped tunnel
// entry for every known service. These appear immediately in the forwards panel.
func loadRegistryTunnels(ch chan<- k8s.TunnelEvent, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		entries, err := registry.Load()
		if err != nil || len(entries) == 0 {
			return nil
		}

		tunnels := make([]*k8s.Tunnel, 0, len(entries))
		for _, e := range entries {
			port := e.Port
			if override, ok := cfg.PortOverrides[e.ServiceName]; ok {
				port = override
			}
			t := k8s.NewTunnel(k8s.TunnelConfig{
				ServiceName:     e.ServiceName,
				Namespace:       e.Namespace,
				LocalPort:       port,
				RemotePort:      port,
				MaxRetries:      cfg.Defaults.ReconnectRetries,
				BackoffSecs:     cfg.Defaults.ReconnectBackoffSeconds,
				ClusterEndpoint: e.ClusterEndpoint,
				ClusterCAData:   e.ClusterCAData,
				ClusterName:     e.ClusterName,
				ClusterEnvName:  e.ClusterEnvName,
			}, ch)
			tunnels = append(tunnels, t)
		}

		return MsgHistoryLoaded{Tunnels: tunnels}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil

	case MsgHistoryLoaded:
		for _, t := range msg.Tunnels {
			m.manager.Add(t)
		}
		m.syncTunnelPanels()
		// Ensure the forwards panel is focused and cursor is on the first tunnel.
		m.focus = FocusForwards
		m.forwards.SetFocused(true)
		if len(msg.Tunnels) > 0 {
			cmds = append(cmds, emitLog("INFO", fmt.Sprintf("Loaded %d known forward(s) — press [R] to resume all", len(msg.Tunnels))))
		}

	case tea.KeyMsg:
		// Confirm dialog intercepts all keys when active.
		if m.confirm.active {
			switch msg.String() {
			case "y", "Y":
				if m.confirm.onYes != nil {
					cmds = append(cmds, m.confirm.onYes)
				}
				m.confirm = confirmDialog{}
			default:
				m.confirm = confirmDialog{}
				cmds = append(cmds, emitLog("INFO", "Cancelled"))
			}
			return m, tea.Batch(cmds...)
		}

		// Port edit dialog intercepts all keys when active.
		if m.portEdit.active {
			switch msg.String() {
			case "enter":
				cmds = append(cmds, m.applyPortEdit())
			case "esc":
				m.portEdit = portEditDialog{}
				cmds = append(cmds, emitLog("INFO", "Port edit cancelled"))
			default:
				var tiCmd tea.Cmd
				m.portEdit.input, tiCmd = m.portEdit.input.Update(msg)
				if tiCmd != nil {
					cmds = append(cmds, tiCmd)
				}
			}
			return m, tea.Batch(cmds...)
		}

		if m.wizard.Visible() {
			newWizard, cmd := m.wizard.Update(msg)
			m.wizard = newWizard
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		if m.profilesMdl.Visible() {
			newP, cmd := m.profilesMdl.Update(msg)
			m.profilesMdl = newP
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		if m.help.Visible() {
			newHelp, cmd := m.help.Update(msg)
			m.help = newHelp
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Command menu intercepts all keys.
		if m.menuMdl.Visible() {
			newMenu, actionKey := m.menuMdl.Update(msg)
			m.menuMdl = newMenu
			if actionKey != "" {
				cmds = append(cmds, m.execMenuAction(actionKey))
			}
			return m, tea.Batch(cmds...)
		}

		// Pod picker intercepts all keys.
		if m.serviceLogs.PodPickerVisible() {
			newSvcLogs, cmd := m.serviceLogs.Update(msg)
			m.serviceLogs = newSvcLogs
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Full-screen detail overlay intercepts all keys.
		if m.serviceLogs.DetailVisible() {
			newSvcLogs, cmd := m.serviceLogs.Update(msg)
			m.serviceLogs = newSvcLogs
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// When service logs panel is focused, route all non-critical keys to it.
		// This prevents global shortcuts (r, p, s, o, c, d, etc.) from firing
		// when the user intends to interact with the log panel.
		if m.focus == FocusServiceLogs {
			switch msg.String() {
			case "q", "ctrl+c", "tab", "l", "?", "a":
				// Fall through to global handling.
			default:
				newSvcLogs, cmd := m.serviceLogs.Update(msg)
				m.serviceLogs = newSvcLogs
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.manager.StopAll()
			if m.serviceLogCancel != nil {
				m.serviceLogCancel()
			}
			return m, tea.Quit

		case "a":
			m.wizard.Show()

		case "s":
			// Save current forwards as a named profile.
			if len(m.manager.List()) > 0 {
				m.profilesMdl.ShowSave()
			} else {
				cmds = append(cmds, emitLog("WARN", "No active forwards to save as profile"))
			}

		case "o":
			// Open / load a saved profile.
			profs, err := profiles.Load()
			if err != nil {
				profs = []profiles.Profile{}
			}
			m.profilesMdl.ShowLoad(profs)

		case "c":
			// Copy localhost:<port> of selected tunnel to clipboard.
			if t := m.forwards.SelectedTunnel(); t != nil {
				text := fmt.Sprintf("localhost:%d", t.LocalPort)
				if err := copyToClipboard(text); err != nil {
					cmds = append(cmds, emitLog("WARN", fmt.Sprintf("Clipboard unavailable: %v", err)))
				} else {
					cmds = append(cmds, emitLog("INFO", fmt.Sprintf("Copied: %s", text)))
				}
			}

		case "l":
			m.showLogs = !m.showLogs
			if !m.showLogs && m.focus != FocusForwards {
				m.focus = FocusForwards
				m.forwards.SetFocused(true)
				m.serviceLogs.SetFocused(false)
			}
			m.relayout()

		case "m":
			m.menuMdl.Show()

		case "?":
			m.help.Toggle()

		case "tab":
			if m.showLogs {
				switch m.focus {
				case FocusForwards:
					m.focus = FocusServiceLogs
					m.forwards.SetFocused(false)
					m.serviceLogs.SetFocused(true)
				case FocusServiceLogs:
					m.focus = FocusLogs
					m.serviceLogs.SetFocused(false)
				case FocusLogs:
					m.focus = FocusForwards
					m.forwards.SetFocused(true)
				}
			}

		// --- per-item actions ---
		case "p":
			if t := m.forwards.SelectedTunnel(); t != nil {
				switch t.Status {
				case k8s.StatusRunning, k8s.StatusConnecting, k8s.StatusReconnecting:
					m.manager.Pause(t.ID)
					cmds = append(cmds, emitLog("INFO", fmt.Sprintf("Paused: %s", t.ID)))
				}
			}

		case "r":
			if t := m.forwards.SelectedTunnel(); t != nil {
				switch t.Status {
				case k8s.StatusPaused, k8s.StatusError, k8s.StatusStopped:
					if err := m.manager.Resume(t.ID, context.Background()); err != nil {
						cmds = append(cmds, emitLog("ERROR", fmt.Sprintf("Resume failed for %s: %v", t.ID, err)))
					} else {
						cmds = append(cmds, emitLog("INFO", fmt.Sprintf("Resuming: %s", t.ID)))
					}
				}
			}

		case "e":
			if t := m.forwards.SelectedTunnel(); t != nil && m.focus == FocusForwards {
				cmds = append(cmds, m.openPortEditDialog(t))
			}

		case "d", "delete":
			if t := m.forwards.SelectedTunnel(); t != nil {
				id, endpoint, ns := t.ID, t.ClusterEndpoint, t.Namespace
				m.manager.Remove(id)
				m.syncTunnelPanels()
				cmds = append(cmds, func() tea.Msg {
					_ = registry.Remove(endpoint, ns, id)
					return MsgLogEntry{Level: "INFO", Text: fmt.Sprintf("Removed: %s", id)}
				})
			}

		// --- bulk actions ---
		case "P":
			manager := m.manager
			cmds = append(cmds, func() tea.Msg {
				manager.PauseAll()
				return MsgLogEntry{Level: "INFO", Text: "Paused all active forwards"}
			})

		case "R":
			manager := m.manager
			cmds = append(cmds, func() tea.Msg {
				errs := manager.ResumeAll(context.Background())
				if len(errs) > 0 {
					return MsgLogEntry{Level: "WARN", Text: fmt.Sprintf("Resume all: %d error(s)", len(errs))}
				}
				return MsgLogEntry{Level: "INFO", Text: "Resumed all forwards"}
			})

		case "X":
			n := len(m.manager.List())
			if n == 0 {
				break
			}
			manager := m.manager
			m.confirm = confirmDialog{
				active:  true,
				message: fmt.Sprintf("Remove all %d forward(s)? [y/N]", n),
				onYes: func() tea.Msg {
					manager.RemoveAll()
					_ = registry.Clear()
					return MsgLogEntry{Level: "INFO", Text: "Removed all forwards"}
				},
			}

		default:
			switch m.focus {
			case FocusForwards:
				prevID := ""
				if prev := m.forwards.SelectedTunnel(); prev != nil {
					prevID = prev.ID
				}
				newFwd, cmd := m.forwards.Update(msg)
				m.forwards = newFwd
				selected := m.forwards.SelectedTunnel()
				m.detail.SetTunnel(selected)
				// Only reset service logs and restart stream when selection changes.
				newID := ""
				if selected != nil {
					newID = selected.ID
				}
				if newID != prevID {
					m.serviceLogs.SetTunnel(selected)
					cmds = append(cmds, m.restartLogStream(selected))
					if selected != nil {
						cmds = append(cmds, listPodsForService(selected))
					}
				}
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusServiceLogs:
				newSvcLogs, cmd := m.serviceLogs.Update(msg)
				m.serviceLogs = newSvcLogs
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusLogs:
				newLogs, cmd := m.logs.Update(msg)
				m.logs = newLogs
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

	case MsgTunnelEvent:
		newFwd, cmd := m.forwards.Update(msg)
		m.forwards = newFwd
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.syncTunnelPanels()
		cmds = append(cmds, listenTunnelEvents(m.tunnelEventCh))
		// Kick off health checks when a tunnel becomes running.
		if msg.Status == k8s.StatusRunning {
			if t, ok := m.manager.Get(msg.ID); ok {
				cmds = append(cmds, startHealthCheck(msg.ID, t.LocalPort))
			}
			// If this is the selected tunnel, start streaming logs and fetch pod list.
			if selected := m.forwards.SelectedTunnel(); selected != nil && selected.ID == msg.ID {
				if t, ok := m.manager.Get(msg.ID); ok {
					m.serviceLogs.SetTunnel(t)
					cmds = append(cmds, m.restartLogStream(t))
					cmds = append(cmds, listPodsForService(t))
				}
			}
		}

	case MsgServiceLogStreamReady:
		scanner := bufio.NewScanner(msg.Stream)
		cmds = append(cmds, readNextLogLine(msg.TunnelID, scanner))

	case MsgServiceLogLine:
		m.serviceLogs = m.serviceLogs.AddLine(msg.TunnelID, msg.Line)
		if msg.next != nil {
			cmds = append(cmds, msg.next)
		}

	case MsgServiceLogsEnded:
		// Stream ended (context canceled, pod gone, or port-forward stopped).
		// Restart whenever possible so logs stay live.
		if selected := m.forwards.SelectedTunnel(); selected != nil && selected.ID == msg.TunnelID {
			if t, ok := m.manager.Get(msg.TunnelID); ok {
				podName := m.serviceLogs.SelectedPodName()
				if podName == "" {
					podName = t.CurrentPod
				}
				if podName != "" {
					cmds = append(cmds, m.restartLogStream(t))
				}
			}
		}

	case MsgPodsLoaded:
		m.serviceLogs.SetPods(msg.TunnelID, msg.Pods)
		// If the selected tunnel just got pods and has no active stream, start one now.
		if selected := m.forwards.SelectedTunnel(); selected != nil && selected.ID == msg.TunnelID {
			if t, ok := m.manager.Get(msg.TunnelID); ok {
				if m.serviceLogs.NeedsStreamRestart() {
					cmds = append(cmds, m.restartLogStream(t))
				}
			}
		}

	case MsgServiceLogStreamErr:
		m.serviceLogs.SetStreamError(msg.TunnelID, msg.Err.Error())

	case MsgClearCopyFeedback:
		newSvcLogs, cmd := m.serviceLogs.Update(msg)
		m.serviceLogs = newSvcLogs
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case MsgLogEntry:
		m.syncTunnelPanels()
		newLogs, cmd := m.logs.Update(msg)
		m.logs = newLogs
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case MsgWizardDone:
		cmds = append(cmds, m.startTunnels(msg))

	case MsgWizardCancelled:
		// nothing

	case MsgHealthResult:
		if t, ok := m.manager.Get(msg.ID); ok {
			if msg.Healthy {
				t.Health = k8s.HealthOK
			} else {
				t.Health = k8s.HealthDegraded
			}
			t.HealthCheckedAt = time.Now()
		}
		m.syncTunnelPanels()
		// Re-schedule while tunnel is still running.
		if t, ok := m.manager.Get(msg.ID); ok && t.Status == k8s.StatusRunning {
			cmds = append(cmds, continueHealthCheck(msg.ID, t.LocalPort))
		}

	case MsgProfileLoad:
		// Stop every running tunnel and wipe the registry before applying the profile.
		m.manager.RemoveAll()
		_ = registry.Clear()
		m.syncTunnelPanels()
		cmds = append(cmds, m.startProfileTunnels(msg.Profile))

	case MsgSaveProfileAs:
		entries, err := registry.Load()
		if err == nil && len(entries) > 0 {
			p := profiles.Profile{Name: msg.Name, Services: entries}
			if saveErr := profiles.Upsert(p); saveErr == nil {
				cmds = append(cmds, emitLog("INFO", fmt.Sprintf("Profile saved: %s (%d services)", msg.Name, len(entries))))
			}
		}

	case MsgClustersLoaded, MsgNamespacesLoaded, MsgServicesLoaded:
		newWizard, cmd := m.wizard.Update(msg)
		m.wizard = newWizard
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case nil:
		// ignore
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) relayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	statusH := 1
	hintH := 1
	clusterH := 3
	available := m.height - statusH - hintH

	var mainH, appLogsH, svcLogsH int
	if m.showLogs {
		appLogsH = max(available*12/100, 3)
		svcLogsH = max(available*28/100, 5)
		mainH = available - appLogsH - svcLogsH - clusterH
	} else {
		mainH = available - clusterH
		appLogsH = 0
		svcLogsH = 0
	}
	mainH = max(mainH, 6)

	fwdW := max(m.width*60/100, 20)
	detailW := m.width - fwdW

	m.statusBar.Resize(m.width)
	m.forwards.Resize(fwdW, mainH)
	m.detail.Resize(detailW, mainH)
	m.cluster.Resize(m.width, clusterH)
	if appLogsH > 0 {
		m.logs.Resize(m.width, appLogsH)
	}
	if svcLogsH > 0 {
		m.serviceLogs.Resize(m.width, svcLogsH)
	}
	m.wizard.Resize(m.width, m.height)
	m.profilesMdl.Resize(m.width, m.height)
	m.help.Resize(m.width, m.height)
	m.menuMdl.Resize(m.width, m.height)
}

// syncTunnelPanels keeps forwards, detail, cluster summary, and status bar in sync
// with the current manager state.
func (m *Model) syncTunnelPanels() {
	tunnels := m.manager.List()
	m.forwards.SetTunnels(tunnels)
	selected := m.forwards.SelectedTunnel()
	m.detail.SetTunnel(selected)
	// Update service logs state without clearing lines (selection didn't change).
	m.serviceLogs.UpdateTunnelState(selected)
	m.cluster.SetTunnels(tunnels)
	m.statusBar.SetTunnels(tunnels)
}

func (m Model) View() string {
	// Modals take over the full screen — overlaying ANSI-colored strings
	// character-by-character corrupts escape sequences and looks broken.
	if m.serviceLogs.PodPickerVisible() {
		return m.serviceLogs.PodPickerView(m.width, m.height)
	}
	if m.serviceLogs.DetailVisible() {
		return m.serviceLogs.DetailView(m.width, m.height)
	}
	if m.wizard.Visible() {
		return placeCenter(m.width, m.height, m.wizard.View())
	}
	if m.profilesMdl.Visible() {
		return placeCenter(m.width, m.height, m.profilesMdl.View())
	}
	if m.help.Visible() {
		return placeCenter(m.width, m.height, m.help.View())
	}
	if m.menuMdl.Visible() {
		return m.menuMdl.View()
	}
	if m.confirm.active {
		return placeCenter(m.width, m.height, confirmView(m.confirm.message))
	}
	if m.portEdit.active {
		return placeCenter(m.width, m.height, portEditView(m.portEdit))
	}

	// Status bar (1 line, full width)
	statusBar := m.statusBar.View()

	// Main row: forwards (left) + detail (right)
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, m.forwards.View(), m.detail.View())

	var parts []string
	parts = append(parts, statusBar)
	parts = append(parts, mainRow)
	parts = append(parts, m.cluster.View())
	if m.showLogs {
		parts = append(parts, m.serviceLogs.View())
		parts = append(parts, m.logs.View())
	}
	parts = append(parts, m.hintBar())

	return strings.Join(parts, "\n")
}

func confirmView(message string) string {
	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f9fafb")).
		Bold(true).
		Render(message) +
		"\n\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Render("[y] Yes") +
		"  " +
		DimStyle.Render("[any] Cancel")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#ef4444")).
		Padding(1, 3).
		Render(content)
}

func (m Model) hintBar() string {
	hints := "[tab] panels  [a] add  [m] menu  [?] help  [q] quit"
	return HintBarStyle.Render(hints)
}

// placeCenter renders content centered on a dark full-screen background using
// lipgloss.Place, which correctly handles ANSI escape codes.
func placeCenter(width, height int, content string) string {
	if width == 0 || height == 0 {
		return content
	}
	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		content,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0f172a")),
		lipgloss.WithWhitespaceChars(" "),
	)
}

func (m Model) startTunnels(msg MsgWizardDone) tea.Cmd {
	cfg := m.cfg
	manager := m.manager
	ch := m.tunnelEventCh
	return func() tea.Msg {
		restCfg, err := k8s.BuildRESTConfig(context.Background(), msg.Cluster)
		if err != nil {
			return MsgLogEntry{
				Level: "ERROR",
				Text:  fmt.Sprintf("Failed to build REST config: %v", err),
				Time:  time.Now(),
			}
		}

		for _, svc := range msg.Services {
			port := 0
			if len(svc.Spec.Ports) > 0 {
				port = int(svc.Spec.Ports[0].Port)
			}
			if override, ok := cfg.PortOverrides[svc.Name]; ok {
				port = override
			}

			localPort, err := portmgr.NextFreePort(port)
			if err != nil {
				localPort = port
			}
			if localPort != port {
				ch <- k8s.TunnelEvent{
					ID:     svc.Name,
					Status: k8s.StatusConnecting,
					Err:    fmt.Errorf("port %d in use, using %d", port, localPort),
				}
			}

			// If tunnel already exists in the manager (from registry), resume it.
			// Otherwise create a new entry and add to registry.
			if existing, ok := manager.Get(svc.Name); ok {
				existing.Start(context.Background(), restCfg)
			} else {
				t := k8s.NewTunnel(k8s.TunnelConfig{
					ServiceName:     svc.Name,
					Namespace:       msg.Namespace,
					LocalPort:       localPort,
					RemotePort:      port,
					MaxRetries:      cfg.Defaults.ReconnectRetries,
					BackoffSecs:     cfg.Defaults.ReconnectBackoffSeconds,
					ClusterEndpoint: msg.Cluster.Endpoint,
					ClusterCAData:   msg.Cluster.CAData,
					ClusterName:     msg.Cluster.Name,
					ClusterEnvName:  msg.Cluster.EnvName,
				}, ch)
				manager.Add(t)
				t.Start(context.Background(), restCfg)
			}

			// Persist to registry so it survives restarts.
			_ = registry.Add(registry.Entry{
				ServiceName:     svc.Name,
				Namespace:       msg.Namespace,
				Port:            port,
				ClusterName:     msg.Cluster.Name,
				ClusterEnvName:  msg.Cluster.EnvName,
				ClusterEndpoint: msg.Cluster.Endpoint,
				ClusterCAData:   msg.Cluster.CAData,
			})
		}

		// Also keep session history for audit purposes.
		svcEntries := make([]history.ServiceEntry, 0, len(msg.Services))
		for _, svc := range msg.Services {
			port := 0
			if len(svc.Spec.Ports) > 0 {
				port = int(svc.Spec.Ports[0].Port)
			}
			svcEntries = append(svcEntries, history.ServiceEntry{Name: svc.Name, Namespace: msg.Namespace, Port: port})
		}
		_ = history.Append(history.Session{
			Timestamp:       time.Now(),
			Project:         msg.Project,
			ClusterName:     msg.Cluster.Name,
			ClusterEnvName:  msg.Cluster.EnvName,
			ClusterEndpoint: msg.Cluster.Endpoint,
			ClusterCAData:   msg.Cluster.CAData,
			Services:        svcEntries,
		})

		return MsgLogEntry{
			Level: "INFO",
			Text:  fmt.Sprintf("Started %d tunnel(s) for cluster %s", len(msg.Services), msg.Cluster.EnvName),
			Time:  time.Now(),
		}
	}
}

func (m Model) startProfileTunnels(prof profiles.Profile) tea.Cmd {
	cfg := m.cfg
	manager := m.manager
	ch := m.tunnelEventCh
	return func() tea.Msg {
		// Group services by cluster endpoint to build REST config once per cluster.
		byCluster := map[string][]registry.Entry{}
		for _, e := range prof.Services {
			byCluster[e.ClusterEndpoint] = append(byCluster[e.ClusterEndpoint], e)
		}

		started := 0
		for endpoint, entries := range byCluster {
			restCfg, err := k8s.BuildRESTConfigFromParts(context.Background(), endpoint, entries[0].ClusterCAData)
			if err != nil {
				continue
			}
			for _, e := range entries {
				port := e.Port
				if override, ok := cfg.PortOverrides[e.ServiceName]; ok {
					port = override
				}
				localPort, err := portmgr.NextFreePort(port)
				if err != nil {
					localPort = port
				}
				if existing, ok := manager.Get(e.ServiceName); ok {
					existing.Start(context.Background(), restCfg)
				} else {
					t := k8s.NewTunnel(k8s.TunnelConfig{
						ServiceName:     e.ServiceName,
						Namespace:       e.Namespace,
						LocalPort:       localPort,
						RemotePort:      port,
						MaxRetries:      cfg.Defaults.ReconnectRetries,
						BackoffSecs:     cfg.Defaults.ReconnectBackoffSeconds,
						ClusterEndpoint: endpoint,
						ClusterCAData:   entries[0].ClusterCAData,
						ClusterName:     e.ClusterName,
						ClusterEnvName:  e.ClusterEnvName,
					}, ch)
					manager.Add(t)
					t.Start(context.Background(), restCfg)
				}
				_ = registry.Add(e)
				started++
			}
		}
		return MsgLogEntry{
			Level: "INFO",
			Text:  fmt.Sprintf("Profile '%s' loaded: %d tunnel(s) started", prof.Name, started),
			Time:  time.Now(),
		}
	}
}

// restartLogStream cancels any existing log stream and starts a new one for t.
// Uses the service logs panel's pod selection override if set.
func (m *Model) restartLogStream(t *k8s.Tunnel) tea.Cmd {
	if m.serviceLogCancel != nil {
		m.serviceLogCancel()
		m.serviceLogCancel = nil
	}
	if t == nil {
		return nil
	}
	podName := m.serviceLogs.SelectedPodName()
	containerName := m.serviceLogs.SelectedPodContainer()
	if podName == "" && t.CurrentPod == "" {
		// No pod known yet — wait for MsgPodsLoaded.
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.serviceLogCancel = cancel
	return streamServiceLogs(ctx, t, podName, containerName)
}

// execMenuAction executes an action from the command menu.
// The key matches the shortcut shown in MenuModel.
func (m *Model) execMenuAction(key string) tea.Cmd {
	switch key {
	case "s":
		if len(m.manager.List()) > 0 {
			m.profilesMdl.ShowSave()
		} else {
			return emitLog("WARN", "No active forwards to save as profile")
		}
	case "o":
		profs, err := profiles.Load()
		if err != nil {
			profs = []profiles.Profile{}
		}
		m.profilesMdl.ShowLoad(profs)
	case "R":
		manager := m.manager
		return func() tea.Msg {
			errs := manager.ResumeAll(context.Background())
			if len(errs) > 0 {
				return MsgLogEntry{Level: "WARN", Text: fmt.Sprintf("Resume all: %d error(s)", len(errs)), Time: time.Now()}
			}
			return MsgLogEntry{Level: "INFO", Text: "Resumed all forwards", Time: time.Now()}
		}
	case "P":
		manager := m.manager
		return func() tea.Msg {
			manager.PauseAll()
			return MsgLogEntry{Level: "INFO", Text: "Paused all active forwards", Time: time.Now()}
		}
	case "X":
		n := len(m.manager.List())
		if n > 0 {
			manager := m.manager
			m.confirm = confirmDialog{
				active:  true,
				message: fmt.Sprintf("Remove all %d forward(s)? [y/N]", n),
				onYes: func() tea.Msg {
					manager.RemoveAll()
					_ = registry.Clear()
					return MsgLogEntry{Level: "INFO", Text: "Removed all forwards", Time: time.Now()}
				},
			}
		}
	case "l":
		m.showLogs = !m.showLogs
		if !m.showLogs && m.focus != FocusForwards {
			m.focus = FocusForwards
			m.forwards.SetFocused(true)
			m.serviceLogs.SetFocused(false)
		}
		m.relayout()
	case "a":
		m.wizard.Show()
	}
	return nil
}

func emitLog(level, text string) tea.Cmd {
	return func() tea.Msg {
		return MsgLogEntry{Level: level, Text: text, Time: time.Now()}
	}
}

// openPortEditDialog opens the port edit modal pre-filled with the tunnel's current local port.
func (m *Model) openPortEditDialog(t *k8s.Tunnel) tea.Cmd {
	ti := textinput.New()
	ti.Placeholder = "local port"
	ti.CharLimit = 5
	ti.SetValue(strconv.Itoa(t.LocalPort))
	ti.Focus()
	wasRunning := t.Status == k8s.StatusRunning ||
		t.Status == k8s.StatusConnecting ||
		t.Status == k8s.StatusReconnecting
	m.portEdit = portEditDialog{
		active:     true,
		tunnelID:   t.ID,
		wasRunning: wasRunning,
		input:      ti,
	}
	return nil
}

// applyPortEdit validates and applies the new local port from the edit dialog.
func (m *Model) applyPortEdit() tea.Cmd {
	raw := strings.TrimSpace(m.portEdit.input.Value())
	newPort, err := strconv.Atoi(raw)
	if err != nil || newPort < 1 || newPort > 65535 {
		m.portEdit = portEditDialog{}
		return emitLog("ERROR", fmt.Sprintf("Invalid port: %q", raw))
	}

	id := m.portEdit.tunnelID
	wasRunning := m.portEdit.wasRunning
	m.portEdit = portEditDialog{}

	t, ok := m.manager.Get(id)
	if !ok {
		return emitLog("WARN", fmt.Sprintf("Tunnel %s not found", id))
	}

	// Stop the tunnel before changing the port.
	m.manager.Pause(id)

	resolvedPort, resolveErr := portmgr.NextFreePort(newPort)
	if resolveErr != nil {
		resolvedPort = newPort
	}

	t.LocalPort = resolvedPort

	logText := fmt.Sprintf("%s: local port changed to %d", id, resolvedPort)
	if resolvedPort != newPort {
		logText = fmt.Sprintf("%s: port %d in use, using %d", id, newPort, resolvedPort)
	}

	if wasRunning {
		manager := m.manager
		ctx := context.Background()
		return func() tea.Msg {
			_ = manager.Resume(id, ctx)
			return MsgLogEntry{Level: "INFO", Text: logText, Time: time.Now()}
		}
	}
	return emitLog("INFO", logText)
}

// portEditView renders the port edit modal.
func portEditView(d portEditDialog) string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f9fafb")).
		Bold(true).
		Render(fmt.Sprintf("Edit local port for  %s", d.tunnelID))

	content := title +
		"\n\n" +
		d.input.View() +
		"\n\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Render("[enter] Apply") +
		"  " +
		DimStyle.Render("[esc] Cancel")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3).
		Width(44).
		Render(content)
}
