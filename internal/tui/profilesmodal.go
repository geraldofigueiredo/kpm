package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geraldofigueiredo/kportmaster/internal/profiles"
)

type profilesMode int

const (
	profilesModeLoad profilesMode = iota
	profilesModeSave
)

type ProfilesModel struct {
	visible   bool
	mode      profilesMode
	list      fuzzyList
	nameInput textinput.Model
	loaded    []profiles.Profile
	err       string
	width     int
	height    int
}

func NewProfilesModel() ProfilesModel {
	ti := textinput.New()
	ti.Placeholder = "profile name..."
	ti.CharLimit = 64
	return ProfilesModel{nameInput: ti}
}

func (m *ProfilesModel) ShowLoad(profs []profiles.Profile) {
	m.visible = true
	m.mode = profilesModeLoad
	m.loaded = profs
	m.err = ""
	m.nameInput.Blur()

	labels := make([]string, len(profs))
	for i, p := range profs {
		clusters := map[string]struct{}{}
		for _, s := range p.Services {
			c := s.ClusterEnvName
			if c == "" {
				c = "unknown"
			}
			clusters[c] = struct{}{}
		}
		names := make([]string, 0, len(clusters))
		for c := range clusters {
			names = append(names, c)
		}
		sort.Strings(names)
		labels[i] = fmt.Sprintf("%-24s  %2d services   %s",
			p.Name, len(p.Services), strings.Join(names, ", "))
	}
	m.list.setItems(labels)
}

func (m *ProfilesModel) ShowSave() {
	m.visible = true
	m.mode = profilesModeSave
	m.err = ""
	m.nameInput.SetValue("")
	m.nameInput.Focus()
}

func (m *ProfilesModel) Hide() { m.visible = false }

func (m ProfilesModel) Visible() bool { return m.visible }

func (m *ProfilesModel) Resize(width, height int) {
	m.width = width
	m.height = height
	n := height - 14
	if n < 3 {
		n = 3
	}
	if n > 10 {
		n = 10
	}
	m.list.visibleMax = n
}

func (m ProfilesModel) Update(msg tea.Msg) (ProfilesModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch m.mode {
	case profilesModeLoad:
		switch keyMsg.String() {
		case "esc":
			m.visible = false
		case "enter":
			idx := m.list.selectedOrigIdx()
			if idx >= 0 && idx < len(m.loaded) {
				prof := m.loaded[idx]
				m.visible = false
				return m, func() tea.Msg { return MsgProfileLoad{Profile: prof} }
			}
		case "d":
			idx := m.list.selectedOrigIdx()
			if idx >= 0 && idx < len(m.loaded) {
				_ = profiles.Remove(m.loaded[idx].Name)
				profs, _ := profiles.Load()
				m.ShowLoad(profs)
			}
		default:
			cmd, _ := m.list.update(keyMsg)
			return m, cmd
		}

	case profilesModeSave:
		switch keyMsg.String() {
		case "esc":
			m.visible = false
		case "enter":
			name := strings.TrimSpace(m.nameInput.Value())
			if name == "" {
				m.err = "name cannot be empty"
				return m, nil
			}
			m.visible = false
			return m, func() tea.Msg { return MsgSaveProfileAs{Name: name} }
		default:
			var cmd tea.Cmd
			m.nameInput, cmd = m.nameInput.Update(keyMsg)
			return m, cmd
		}
	}
	return m, nil
}

func (m ProfilesModel) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	var sb strings.Builder

	switch m.mode {
	case profilesModeLoad:
		sb.WriteString(titleStyle.Render("PROFILES"))
		if len(m.loaded) > 0 {
			sb.WriteString(DimStyle.Render(fmt.Sprintf("  %d saved", len(m.loaded))))
		}
		sb.WriteString("\n\n")
		if m.err != "" {
			sb.WriteString(ErrorTextStyle.Render("  "+m.err) + "\n\n")
		}
		if len(m.loaded) == 0 {
			sb.WriteString(DimStyle.Render("  no profiles saved yet") + "\n\n")
			sb.WriteString(DimStyle.Render("  press [s] on the main screen to save the current forwards"))
		} else {
			sb.WriteString(m.list.view("Select a profile", nil, false))
			sb.WriteString("\n")
			sb.WriteString(DimStyle.Render("Enter load   d delete   Esc cancel"))
		}

	case profilesModeSave:
		sb.WriteString(titleStyle.Render("SAVE PROFILE") + "\n\n")
		if m.err != "" {
			sb.WriteString(ErrorTextStyle.Render("  "+m.err) + "\n\n")
		}
		sb.WriteString(DimStyle.Render("  Name:") + "\n")
		sb.WriteString("  " + m.nameInput.View() + "\n\n")
		sb.WriteString(DimStyle.Render("Enter save   Esc cancel"))
	}

	return WizardOverlayStyle.Width(64).Render(sb.String())
}
