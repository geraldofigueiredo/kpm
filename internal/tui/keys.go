package tui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap defines application-wide keybindings.
type GlobalKeyMap struct {
	Add        key.Binding
	Stop       key.Binding
	Retry      key.Binding
	Resume     key.Binding
	ToggleLogs key.Binding
	Help       key.Binding
	Quit       key.Binding
}

// WizardKeyMap defines wizard-specific keybindings.
type WizardKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding
	Search key.Binding
}

var GlobalKeys = GlobalKeyMap{
	Add: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add forward"),
	),
	Stop: key.NewBinding(
		key.WithKeys("d", "delete"),
		key.WithHelp("d", "stop forward"),
	),
	Retry: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "retry"),
	),
	Resume: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "resume last session"),
	),
	ToggleLogs: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "toggle logs"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

var WizardKeys = WizardKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
}
