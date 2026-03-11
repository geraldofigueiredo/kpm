package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
)

type DetailModel struct {
	width  int
	height int
	tunnel *k8s.Tunnel
}

func NewDetailModel(width, height int) DetailModel {
	return DetailModel{width: width, height: height}
}

func (m *DetailModel) SetTunnel(t *k8s.Tunnel) {
	m.tunnel = t
}

func (m *DetailModel) Resize(width, height int) {
	m.width = width
	m.height = height
}

func (m DetailModel) View() string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorGray).
		Width(m.width - 2).
		Height(m.height - 2)

	titleStyle := lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	labelStyle := DimStyle
	valueStyle := lipgloss.NewStyle().Foreground(colorWhite)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("DETAIL") + "\n\n")

	if m.tunnel == nil {
		sb.WriteString(DimStyle.Render("  select a forward to inspect"))
		return border.Render(sb.String())
	}

	t := m.tunnel
	const labelW = 12

	row := func(label, value string) {
		sb.WriteString(
			labelStyle.Render(fmt.Sprintf("  %-*s", labelW, label)) +
				" " +
				valueStyle.Render(value) + "\n",
		)
	}

	row("service", t.ID)
	row("namespace", t.Namespace)
	if t.ClusterEnvName != "" {
		row("cluster", t.ClusterEnvName)
	}
	row("local port", fmt.Sprintf("%d", t.LocalPort))
	row("remote port", fmt.Sprintf("%d", t.RemotePort))

	icon := StatusIcon(t.Status.String())
	sb.WriteString(
		labelStyle.Render(fmt.Sprintf("  %-*s", labelW, "status")) +
			" " + icon + " " + t.Status.String() + "\n",
	)

	// Health indicator (only meaningful when running).
	if t.Status == k8s.StatusRunning {
		var healthStr string
		switch t.Health {
		case k8s.HealthOK:
			healthStr = HealthOKStyle.Render("✓ ok")
		case k8s.HealthDegraded:
			healthStr = HealthDegradedStyle.Render("⚠ degraded")
		default:
			healthStr = DimStyle.Render("… checking")
		}
		if !t.HealthCheckedAt.IsZero() {
			ago := time.Since(t.HealthCheckedAt).Round(time.Second)
			healthStr += DimStyle.Render(fmt.Sprintf("  (%s ago)", ago))
		}
		sb.WriteString(
			labelStyle.Render(fmt.Sprintf("  %-*s", labelW, "health")) +
				" " + healthStr + "\n",
		)
	}

	if t.CurrentPod != "" {
		row("pod", t.CurrentPod)
	}

	if !t.StartedAt.IsZero() && t.Status == k8s.StatusRunning {
		uptime := time.Since(t.StartedAt).Round(time.Second)
		row("uptime", uptime.String())
	}

	if t.RetryCount > 0 {
		row("retries", fmt.Sprintf("%d / %d", t.RetryCount, t.MaxRetries))
	}

	return border.Render(sb.String())
}
