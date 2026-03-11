package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorGreen  = lipgloss.Color("#22c55e")
	colorYellow = lipgloss.Color("#eab308")
	colorRed    = lipgloss.Color("#ef4444")
	colorGray   = lipgloss.Color("#6b7280")
	colorBlue   = lipgloss.Color("#3b82f6")
	colorWhite  = lipgloss.Color("#f9fafb")
	colorDim    = lipgloss.Color("#9ca3af")

	StatusRunningStyle      = lipgloss.NewStyle().Foreground(colorGreen)
	StatusReconnectingStyle = lipgloss.NewStyle().Foreground(colorYellow)
	StatusErrorStyle        = lipgloss.NewStyle().Foreground(colorRed)
	StatusStoppedStyle      = lipgloss.NewStyle().Foreground(colorGray)
	StatusConnectingStyle   = lipgloss.NewStyle().Foreground(colorBlue)
	StatusPausedStyle       = lipgloss.NewStyle().Foreground(colorYellow)

	PanelBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorGray)

	PanelTitleStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true).
			Padding(0, 1)

	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(colorBlue).
				Bold(true)

	NormalItemStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	DimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	HintBarStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Padding(0, 1)

	WizardOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBlue).
				Padding(1, 2)

	ErrorTextStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	LogInfoStyle  = lipgloss.NewStyle().Foreground(colorDim)
	LogWarnStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	LogErrorStyle = lipgloss.NewStyle().Foreground(colorRed)

	HealthOKStyle       = lipgloss.NewStyle().Foreground(colorGreen)
	HealthDegradedStyle = lipgloss.NewStyle().Foreground(colorYellow)
)

// StatusIcon returns the display icon for a given status string.
func StatusIcon(status string) string {
	switch status {
	case "running":
		return StatusRunningStyle.Render("●")
	case "reconnecting":
		return StatusReconnectingStyle.Render("↺")
	case "error":
		return StatusErrorStyle.Render("✗")
	case "paused":
		return StatusPausedStyle.Render("⏸")
	case "stopped":
		return StatusStoppedStyle.Render("○")
	case "connecting":
		return StatusConnectingStyle.Render("◌")
	default:
		return DimStyle.Render("?")
	}
}
