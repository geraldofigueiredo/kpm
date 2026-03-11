package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type HelpModel struct {
	visible bool
	width   int
	height  int
}

func NewHelpModel() HelpModel {
	return HelpModel{}
}

func (m HelpModel) Init() tea.Cmd {
	return nil
}

func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "?" || msg.String() == "esc" {
			m.visible = false
		}
	}
	return m, nil
}

func (m *HelpModel) Toggle() {
	m.visible = !m.visible
}

func (m HelpModel) Visible() bool {
	return m.visible
}

func (m *HelpModel) Resize(width, height int) {
	m.width = width
	m.height = height
}

func (m HelpModel) View() string {
	if !m.visible {
		return ""
	}

	entries := []struct{ key, desc string }{
		{"a", "Add new forward(s) via wizard"},
		{"p", "Pause selected forward"},
		{"P", "Pause all active forwards"},
		{"r", "Resume selected forward"},
		{"R", "Resume all paused/stopped/errored"},
		{"d / Delete", "Remove selected forward"},
		{"X", "Remove all forwards (with confirmation)"},
		{"c", "Copy localhost:port to clipboard"},
		{"s", "Save current forwards as a profile"},
		{"o", "Open / load a saved profile"},
		{"Tab", "Switch panel focus"},
		{"l", "Toggle log panels"},
		{"/", "Filter service logs"},
		{"G", "Jump to end of service logs"},
		{"q / Ctrl+C", "Quit (stops all forwards)"},
		{"?", "Toggle this help"},
	}

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3b82f6")).
		Width(22)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9fafb"))
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9fafb")).Bold(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Keybindings") + "\n\n")
	for _, e := range entries {
		sb.WriteString(keyStyle.Render(e.key))
		sb.WriteString(descStyle.Render(e.desc))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(DimStyle.Render("Press ? or Esc to close"))

	return WizardOverlayStyle.Render(sb.String())
}
