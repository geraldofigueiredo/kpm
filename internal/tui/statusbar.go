package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
)

type StatusBarModel struct {
	width   int
	tunnels []*k8s.Tunnel
}

func NewStatusBarModel(width int) StatusBarModel {
	return StatusBarModel{width: width}
}

func (m *StatusBarModel) SetTunnels(tunnels []*k8s.Tunnel) {
	m.tunnels = tunnels
}

func (m *StatusBarModel) Resize(width int) {
	m.width = width
}

func (m StatusBarModel) View() string {
	running, paused, errored := 0, 0, 0
	clusterSet := map[string]struct{}{}
	for _, t := range m.tunnels {
		if t.ClusterEnvName != "" {
			clusterSet[t.ClusterEnvName] = struct{}{}
		}
		switch t.Status {
		case k8s.StatusRunning:
			running++
		case k8s.StatusPaused:
			paused++
		case k8s.StatusError:
			errored++
		}
	}

	clusterNames := make([]string, 0, len(clusterSet))
	for c := range clusterSet {
		clusterNames = append(clusterNames, c)
	}
	sort.Strings(clusterNames)

	appStyle := lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	left := appStyle.Render("kpm")
	if len(clusterNames) > 0 {
		left += DimStyle.Render("  " + strings.Join(clusterNames, ", "))
	}

	var countParts []string
	if running > 0 {
		countParts = append(countParts, StatusRunningStyle.Render(fmt.Sprintf("● %d running", running)))
	}
	if paused > 0 {
		countParts = append(countParts, StatusPausedStyle.Render(fmt.Sprintf("⏸ %d paused", paused)))
	}
	if errored > 0 {
		countParts = append(countParts, StatusErrorStyle.Render(fmt.Sprintf("✗ %d error", errored)))
	}
	if len(m.tunnels) == 0 {
		countParts = append(countParts, DimStyle.Render("no forwards"))
	}
	right := strings.Join(countParts, "  ")

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW - 2
	if gap < 1 {
		gap = 1
	}

	return lipgloss.NewStyle().
		Background(lipgloss.Color("#111827")).
		Padding(0, 1).
		Width(m.width).
		Render(left + strings.Repeat(" ", gap) + right)
}
