package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cmdMenuItem struct {
	key      string
	label    string
	isSep    bool
	sepLabel string
}

var menuItemList = []cmdMenuItem{
	{isSep: true, sepLabel: "Profiles"},
	{key: "s", label: "Save forwards as profile"},
	{key: "o", label: "Load a saved profile"},
	{isSep: true, sepLabel: "Bulk Actions"},
	{key: "R", label: "Resume all forwards"},
	{key: "P", label: "Pause all forwards"},
	{key: "X", label: "Remove all forwards"},
	{isSep: true, sepLabel: "View"},
	{key: "l", label: "Toggle log panels"},
	{key: "a", label: "Add port forward"},
}

// selectableIdxs returns the menuItemList indices that are selectable (non-sep).
func selectableIdxs() []int {
	var out []int
	for i, item := range menuItemList {
		if !item.isSep {
			out = append(out, i)
		}
	}
	return out
}

// MenuModel is the full-screen command menu opened with [m].
type MenuModel struct {
	visible bool
	cursor  int // index into selectableIdxs()
	width   int
	height  int
}

func (m MenuModel) Visible() bool { return m.visible }

func (m *MenuModel) Show() {
	m.visible = true
	m.cursor = 0
}

func (m *MenuModel) Resize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events while the menu is open.
// Returns the updated model and the action key to execute ("" = no action).
func (m MenuModel) Update(msg tea.KeyMsg) (MenuModel, string) {
	sel := selectableIdxs()
	switch msg.String() {
	case "esc", "q", "m":
		m.visible = false
		return m, ""
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(sel)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor < len(sel) {
			key := menuItemList[sel[m.cursor]].key
			m.visible = false
			return m, key
		}
	default:
		// Direct shortcut key.
		for _, item := range menuItemList {
			if !item.isSep && item.key == msg.String() {
				m.visible = false
				return m, item.key
			}
		}
		// Unknown key — just close without action.
		m.visible = false
	}
	return m, ""
}

func (m MenuModel) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9fafb")).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f1f5f9"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	selBG := lipgloss.Color("#1e3a5f")
	selKeyStyle := lipgloss.NewStyle().Background(selBG).Foreground(lipgloss.Color("#93c5fd")).Bold(true)
	selLabelStyle := lipgloss.NewStyle().Background(selBG).Foreground(lipgloss.Color("#f1f5f9"))

	var lines []string
	lines = append(lines, titleStyle.Render("Command Menu"))
	lines = append(lines, "")

	selIdx := 0
	for _, item := range menuItemList {
		if item.isSep {
			if len(lines) > 2 {
				lines = append(lines, "")
			}
			lines = append(lines, sepStyle.Render("  "+item.sepLabel))
		} else {
			selected := selIdx == m.cursor
			badge := fmt.Sprintf("[%s]", item.key)
			var row string
			if selected {
				row = "▶ " + selKeyStyle.Render(badge) + " " + selLabelStyle.Render(item.label)
			} else {
				row = "  " + keyStyle.Render(badge) + " " + labelStyle.Render(item.label)
			}
			lines = append(lines, row)
			selIdx++
		}
	}

	lines = append(lines, "")
	lines = append(lines, DimStyle.Render("  [j/k] navigate  [enter] select  [esc] close"))

	content := strings.Join(lines, "\n")
	boxW := max(m.width*45/100, 42)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Width(boxW-2).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0f172a")),
		lipgloss.WithWhitespaceChars(" "),
	)
}
