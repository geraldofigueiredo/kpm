package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
)

const fwdRowCap = 16 // max visual rows shown (tunnels + headers combined)

type ForwardsModel struct {
	tunnels   []*k8s.Tunnel
	cursor    int
	scrollOff int
	width     int
	height    int
	focused   bool
}

func NewForwardsModel(width, height int, focused bool) ForwardsModel {
	return ForwardsModel{
		tunnels: []*k8s.Tunnel{},
		width:   width,
		height:  height,
		focused: focused,
	}
}

func (m ForwardsModel) Init() tea.Cmd { return nil }

func (m ForwardsModel) Update(msg tea.Msg) (ForwardsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MsgTunnelEvent:
		for _, t := range m.tunnels {
			if t.ID == msg.ID {
				t.Status = msg.Status
				t.RetryCount = msg.Retry
			}
		}

	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scrollOff {
					m.scrollOff = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.tunnels)-1 {
				m.cursor++
				if m.cursor > m.lastVisibleTunnelIdx() {
					m.scrollOff++
				}
			}
		}
	}
	return m, nil
}

// lastVisibleTunnelIdx returns the tunnel index of the last item that fits
// in the visible window given the current scrollOff. Used for scroll-down logic.
func (m ForwardsModel) lastVisibleTunnelIdx() int {
	if len(m.tunnels) == 0 {
		return -1
	}
	rows := 0
	lastCluster := ""
	for i := m.scrollOff; i < len(m.tunnels); i++ {
		c := m.tunnels[i].ClusterEnvName
		if c == "" {
			c = "unknown"
		}
		if c != lastCluster {
			rows++
			lastCluster = c
			if rows >= fwdRowCap {
				return i - 1
			}
		}
		rows++
		if rows >= fwdRowCap {
			return i
		}
	}
	return len(m.tunnels) - 1
}

func (m *ForwardsModel) SetTunnels(tunnels []*k8s.Tunnel) {
	firstLoad := len(m.tunnels) == 0 && len(tunnels) > 0
	m.tunnels = make([]*k8s.Tunnel, len(tunnels))
	copy(m.tunnels, tunnels)
	// Group by cluster, then sort alphabetically within each cluster.
	sort.Slice(m.tunnels, func(i, j int) bool {
		ci, cj := m.tunnels[i].ClusterEnvName, m.tunnels[j].ClusterEnvName
		if ci != cj {
			return ci < cj
		}
		return m.tunnels[i].ID < m.tunnels[j].ID
	})
	switch {
	case len(m.tunnels) == 0:
		m.cursor = 0
		m.scrollOff = 0
	case firstLoad:
		// Always start on the first tunnel when the list populates for the first time.
		m.cursor = 0
		m.scrollOff = 0
	case m.cursor >= len(m.tunnels):
		m.cursor = len(m.tunnels) - 1
	}
}

func (m *ForwardsModel) SetFocused(focused bool) { m.focused = focused }

func (m *ForwardsModel) Resize(width, height int) {
	m.width = width
	m.height = height
}

func (m ForwardsModel) SelectedTunnel() *k8s.Tunnel {
	if len(m.tunnels) == 0 || m.cursor >= len(m.tunnels) {
		return nil
	}
	return m.tunnels[m.cursor]
}

func (m ForwardsModel) View() string {
	borderColor := lipgloss.Color("#6b7280")
	if m.focused {
		borderColor = lipgloss.Color("#3b82f6")
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width - 2).
		Height(m.height - 2)

	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9fafb")).Bold(true)

	runningCount := 0
	for _, t := range m.tunnels {
		if t.Status == k8s.StatusRunning {
			runningCount++
		}
	}
	countLabel := DimStyle.Render(fmt.Sprintf("  %d/%d running", runningCount, len(m.tunnels)))

	var panelHint string
	if m.focused && len(m.tunnels) > 0 {
		panelHint = DimStyle.Render("  [r]resume  [p]pause  [e]edit port  [d]del  [c]copy")
	}
	sb.WriteString(titleStyle.Render("ACTIVE FORWARDS") + countLabel + panelHint + "\n\n")

	if len(m.tunnels) == 0 {
		sb.WriteString(DimStyle.Render("  no forwards yet — press [a] to start"))
		return border.Render(sb.String())
	}

	cursor := m.cursor
	if cursor >= len(m.tunnels) {
		cursor = len(m.tunnels) - 1
	}

	// Inner width: panel width minus border (2) minus padding (2).
	innerW := m.width - 4
	if innerW < 10 {
		innerW = 10
	}

	cursorStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e3a5f")).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true).
		Width(innerW)

	clusterLabelStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e3a5f")).
		Foreground(lipgloss.Color("#93c5fd")).
		Bold(true).
		Padding(0, 1)

	// Pre-compute name column width from all tunnels.
	const portW = 22
	nameW := 16
	for _, t := range m.tunnels {
		if n := len(t.ID); n > nameW {
			nameW = n
		}
	}

	// Scroll-up indicator.
	if m.scrollOff > 0 {
		sb.WriteString(DimStyle.Render(fmt.Sprintf("  ↑ %d more", m.scrollOff)) + "\n")
	}

	// Render visible rows: cluster headers + tunnel rows, capped at fwdRowCap.
	rows := 0
	lastCluster := ""
	lastRenderedTunnel := m.scrollOff - 1

	for i := m.scrollOff; i < len(m.tunnels); i++ {
		t := m.tunnels[i]
		clusterName := t.ClusterEnvName
		if clusterName == "" {
			clusterName = "unknown"
		}

		// Cluster group header — show whenever we enter a new cluster.
		if clusterName != lastCluster {
			if rows >= fwdRowCap {
				break
			}
			upperName := strings.ToUpper(clusterName)
			renderedLabel := clusterLabelStyle.Render("◆ " + upperName)
			labelW := lipgloss.Width(renderedLabel)
			dashLen := innerW - 2 - labelW - 1
			if dashLen < 1 {
				dashLen = 1
			}
			headerLine := "  " +
				renderedLabel +
				DimStyle.Render(strings.Repeat("─", dashLen))
			sb.WriteString(headerLine + "\n")
			rows++
			lastCluster = clusterName
		}

		if rows >= fwdRowCap {
			break
		}

		// Tunnel row.
		isSelected := i == cursor && m.focused

		icon := StatusIcon(t.Status.String())
		ports := fmt.Sprintf("localhost:%d → :%d", t.LocalPort, t.RemotePort)

		statusText := t.Status.String()
		switch t.Status {
		case k8s.StatusRunning:
			statusText = StatusRunningStyle.Render("running")
			if t.Health == k8s.HealthDegraded {
				statusText += " " + HealthDegradedStyle.Render("⚠")
			}
		case k8s.StatusReconnecting:
			statusText = fmt.Sprintf("reconnecting %d/%d", t.RetryCount, t.MaxRetries)
		case k8s.StatusStopped:
			statusText = DimStyle.Render("stopped")
		case k8s.StatusPaused:
			statusText = StatusPausedStyle.Render("paused")
		case k8s.StatusError:
			statusText = StatusErrorStyle.Render("error")
		}

		pointer := "  "
		if isSelected {
			pointer = "▶ "
		}

		row := fmt.Sprintf("%s%s %-*s  %-*s %s", pointer, icon, nameW, t.ID, portW, ports, statusText)

		if isSelected {
			sb.WriteString(cursorStyle.Render(row))
		} else {
			sb.WriteString(NormalItemStyle.Render(row))
		}
		sb.WriteString("\n")
		rows++
		lastRenderedTunnel = i
	}

	// Scroll-down indicator.
	remaining := len(m.tunnels) - 1 - lastRenderedTunnel
	if remaining > 0 {
		sb.WriteString(DimStyle.Render(fmt.Sprintf("  ↓ %d more", remaining)) + "\n")
	}

	// Contextual hint for the selected entry.
	if m.focused {
		sb.WriteString("\n" + DimStyle.Render("  "+m.contextualHint(m.tunnels[cursor].Status)))
	}

	return border.Render(sb.String())
}

func (m ForwardsModel) contextualHint(status k8s.TunnelStatus) string {
	switch status {
	case k8s.StatusRunning, k8s.StatusConnecting, k8s.StatusReconnecting:
		return "[p]pause  [e]edit port  [d]remove"
	case k8s.StatusPaused, k8s.StatusError, k8s.StatusStopped:
		return "[r]resume  [e]edit port  [d]remove"
	default:
		return "[e]edit port  [d]remove"
	}
}
