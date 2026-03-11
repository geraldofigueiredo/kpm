package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxLogEntries = 100

type LogEntry struct {
	Level string
	Text  string
	Time  time.Time
}

type LogsModel struct {
	viewport viewport.Model
	entries  []LogEntry
	width    int
	height   int
}

func NewLogsModel(width, height int) LogsModel {
	vp := viewport.New(width-2, height-2)
	return LogsModel{
		viewport: vp,
		entries:  []LogEntry{},
		width:    width,
		height:   height,
	}
}

func (m LogsModel) Init() tea.Cmd {
	return nil
}

func (m LogsModel) Update(msg tea.Msg) (LogsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MsgLogEntry:
		entry := LogEntry{Level: msg.Level, Text: msg.Text, Time: msg.Time}
		m.entries = append(m.entries, entry)
		if len(m.entries) > maxLogEntries {
			m.entries = m.entries[len(m.entries)-maxLogEntries:]
		}
		m.viewport.SetContent(m.renderEntries())
		m.viewport.GotoBottom()
		return m, nil

	case MsgWindowResize:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 2
		m.viewport.SetContent(m.renderEntries())
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m LogsModel) View() string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6b7280")).
		Width(m.width - 2).
		Height(m.height - 2)

	return border.Render(m.viewport.View())
}

func (m LogsModel) renderEntries() string {
	if len(m.entries) == 0 {
		return DimStyle.Render("  no log entries yet")
	}
	lines := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		ts := e.Time.Format("15:04:05")
		level := formatLevel(e.Level)
		lines = append(lines, fmt.Sprintf("%s %s %s", DimStyle.Render(ts), level, e.Text))
	}
	return strings.Join(lines, "\n")
}

func formatLevel(level string) string {
	switch level {
	case "INFO":
		return LogInfoStyle.Render("[INFO] ")
	case "WARN":
		return LogWarnStyle.Render("[WARN] ")
	case "ERROR":
		return LogErrorStyle.Render("[ERROR]")
	default:
		return DimStyle.Render("[" + level + "]")
	}
}

func (m *LogsModel) Resize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = width - 4
	m.viewport.Height = height - 2
}
