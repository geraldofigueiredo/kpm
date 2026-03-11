package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
)

type clusterStats struct {
	name    string
	running int
	paused  int
	errored int
	stopped int
}

type ClusterSummaryModel struct {
	width   int
	height  int
	tunnels []*k8s.Tunnel
}

func NewClusterSummaryModel(width, height int) ClusterSummaryModel {
	return ClusterSummaryModel{width: width, height: height}
}

func (m *ClusterSummaryModel) SetTunnels(tunnels []*k8s.Tunnel) {
	m.tunnels = tunnels
}

func (m *ClusterSummaryModel) Resize(width, height int) {
	m.width = width
	m.height = height
}

func (m ClusterSummaryModel) View() string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorGray).
		Width(m.width - 2)

	titleStyle := lipgloss.NewStyle().Foreground(colorWhite).Bold(true)

	statsMap := map[string]*clusterStats{}
	for _, t := range m.tunnels {
		name := t.ClusterEnvName
		if name == "" {
			name = "unknown"
		}
		s, ok := statsMap[name]
		if !ok {
			s = &clusterStats{name: name}
			statsMap[name] = s
		}
		switch t.Status {
		case k8s.StatusRunning:
			s.running++
		case k8s.StatusPaused:
			s.paused++
		case k8s.StatusError:
			s.errored++
		default:
			s.stopped++
		}
	}

	line := titleStyle.Render("CLUSTERS")

	if len(statsMap) == 0 {
		line += "  " + DimStyle.Render("no clusters active")
		return border.Render(line)
	}

	names := make([]string, 0, len(statsMap))
	for n := range statsMap {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		s := statsMap[name]
		var counts []string
		if s.running > 0 {
			counts = append(counts, StatusRunningStyle.Render(fmt.Sprintf("●%d", s.running)))
		}
		if s.paused > 0 {
			counts = append(counts, StatusPausedStyle.Render(fmt.Sprintf("⏸%d", s.paused)))
		}
		if s.errored > 0 {
			counts = append(counts, StatusErrorStyle.Render(fmt.Sprintf("✗%d", s.errored)))
		}
		if s.stopped > 0 {
			counts = append(counts, DimStyle.Render(fmt.Sprintf("○%d", s.stopped)))
		}
		entry := lipgloss.NewStyle().Foreground(colorWhite).Render(name) +
			" " + strings.Join(counts, " ")
		line += "    " + entry
	}

	return border.Render(line)
}
