package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/geraldofigueiredo/kportmaster/internal/config"
	"github.com/geraldofigueiredo/kportmaster/internal/gcp"
	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
)

const listVisibleDefault = 8 // fallback when terminal height is unknown

// ---------------------------------------------------------------------------
// fuzzyList — reusable filterable, scrollable list
// ---------------------------------------------------------------------------

type fuzzyList struct {
	filter     textinput.Model
	labels     []string
	cursor     int
	scrollOff  int
	visibleMax int // 0 = use listVisibleDefault
}

func (l fuzzyList) maxVisible() int {
	if l.visibleMax > 0 {
		return l.visibleMax
	}
	return listVisibleDefault
}

func newFuzzyList() fuzzyList {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.CharLimit = 64
	ti.Focus()
	return fuzzyList{filter: ti}
}

func (l *fuzzyList) setItems(labels []string) {
	l.labels = labels
	l.cursor = 0
	l.scrollOff = 0
	l.filter.SetValue("")
}

func (l *fuzzyList) reset() {
	l.cursor = 0
	l.scrollOff = 0
	l.filter.SetValue("")
}

// filteredIdx returns the indices (into l.labels) that match the current filter.
func (l fuzzyList) filteredIdx() []int {
	q := strings.ToLower(l.filter.Value())
	if q == "" {
		idx := make([]int, len(l.labels))
		for i := range idx {
			idx[i] = i
		}
		return idx
	}
	var result []int
	for i, lbl := range l.labels {
		if strings.Contains(strings.ToLower(lbl), q) {
			result = append(result, i)
		}
	}
	return result
}

// selectedOrigIdx returns the original index of the currently highlighted item, or -1.
func (l fuzzyList) selectedOrigIdx() int {
	f := l.filteredIdx()
	if len(f) == 0 {
		return -1
	}
	c := l.cursor
	if c >= len(f) {
		c = len(f) - 1
	}
	return f[c]
}

// update handles a KeyMsg. Returns (cmd, consumed).
// enter / esc / space are NOT consumed — the caller handles them.
func (l *fuzzyList) update(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if l.cursor > 0 {
			l.cursor--
			if l.cursor < l.scrollOff {
				l.scrollOff = l.cursor
			}
		}
		return nil, true

	case "down", "j":
		f := l.filteredIdx()
		if l.cursor < len(f)-1 {
			l.cursor++
			if l.cursor >= l.scrollOff+l.maxVisible() {
				l.scrollOff = l.cursor - l.maxVisible() + 1
			}
		}
		return nil, true

	case "enter", "esc", " ", "ctrl+c":
		return nil, false // let caller decide

	default:
		prev := l.filter.Value()
		var cmd tea.Cmd
		l.filter, cmd = l.filter.Update(msg)
		if l.filter.Value() != prev {
			l.cursor = 0
			l.scrollOff = 0
		}
		return cmd, true
	}
}

// view renders the filter input + scrollable list.
// checkedIdxs is the set of selected ORIGINAL indices (only used when multiSelect=true).
func (l fuzzyList) view(title string, checkedIdxs map[int]bool, multiSelect bool) string {
	var sb strings.Builder

	sb.WriteString(DimStyle.Render(title) + "\n")
	sb.WriteString(l.filter.View() + "\n\n")

	f := l.filteredIdx()
	if len(f) == 0 {
		sb.WriteString(DimStyle.Render("  no matches") + "\n")
		return sb.String()
	}

	cursor := l.cursor
	if cursor >= len(f) {
		cursor = len(f) - 1
	}

	end := l.scrollOff + l.maxVisible()
	if end > len(f) {
		end = len(f)
	}

	if l.scrollOff > 0 {
		sb.WriteString(DimStyle.Render(fmt.Sprintf("  ↑ %d more", l.scrollOff)) + "\n")
	}

	checkedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))

	for i := l.scrollOff; i < end; i++ {
		origIdx := f[i]
		label := l.labels[origIdx]

		var row string
		if multiSelect {
			cb := "[ ]"
			if checkedIdxs[origIdx] {
				cb = checkedStyle.Render("[x]")
			}
			row = fmt.Sprintf("  %s %-40s", cb, label)
		} else {
			prefix := "    "
			if i == cursor {
				prefix = "  > "
			}
			row = prefix + label
		}

		if i == cursor {
			sb.WriteString(SelectedItemStyle.Render(row))
		} else {
			sb.WriteString(NormalItemStyle.Render(row))
		}
		sb.WriteString("\n")
	}

	remaining := len(f) - end
	if remaining > 0 {
		sb.WriteString(DimStyle.Render(fmt.Sprintf("  ↓ %d more", remaining)) + "\n")
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// WizardModel
// ---------------------------------------------------------------------------

type WizardStep int

const (
	StepProject   WizardStep = iota
	StepCluster   WizardStep = iota
	StepNamespace WizardStep = iota
	StepServices  WizardStep = iota
	StepConfirm   WizardStep = iota
)

type WizardModel struct {
	step    WizardStep
	cfg     *config.Config
	visible bool
	width   int
	height  int
	err     string
	loading bool

	projectList fuzzyList
	clusterList fuzzyList
	nsList      fuzzyList
	svcList     fuzzyList

	// raw data
	projects   []config.Project
	clusters   []gcp.Cluster
	namespaces []string
	services   []corev1.Service

	selectedSvcs map[int]bool // original service indices

	// confirmed
	selectedProject   string
	selectedCluster   gcp.Cluster
	selectedNamespace string
}

func NewWizardModel(cfg *config.Config) WizardModel {
	m := WizardModel{
		cfg:          cfg,
		projects:     cfg.Projects,
		namespaces:   []string{"default"},
		selectedSvcs: map[int]bool{},
		projectList:  newFuzzyList(),
		clusterList:  newFuzzyList(),
		nsList:       newFuzzyList(),
		svcList:      newFuzzyList(),
	}
	m.projectList.setItems(m.projectLabels())
	m.nsList.setItems([]string{"default"})
	return m
}

func (m *WizardModel) Show() {
	m.visible = true
	m.step = StepProject
	m.err = ""
	m.loading = false
	m.selectedSvcs = map[int]bool{}
	m.projectList.reset()
	m.clusterList.reset()
	m.nsList.reset()
	m.svcList.reset()
	m.currentList().filter.Focus()
}

func (m *WizardModel) Hide() { m.visible = false }

func (m WizardModel) Visible() bool { return m.visible }

func (m *WizardModel) Resize(width, height int) {
	m.width = width
	m.height = height
	// Wizard box overhead: title(2) + list-title(1) + filter(1) + blank(1) +
	// scroll-indicators(2) + hint(2) + border(2) + padding-tb(2) = ~13 lines.
	// Leave a 2-line safety margin.
	n := height - 15
	if n < 3 {
		n = 3
	}
	if n > 12 {
		n = 12
	}
	m.projectList.visibleMax = n
	m.clusterList.visibleMax = n
	m.nsList.visibleMax = n
	m.svcList.visibleMax = n
}

func (m WizardModel) Init() tea.Cmd { return nil }

// currentList returns a pointer to the fuzzyList for the active step.
func (m *WizardModel) currentList() *fuzzyList {
	switch m.step {
	case StepProject:
		return &m.projectList
	case StepCluster:
		return &m.clusterList
	case StepNamespace:
		return &m.nsList
	case StepServices:
		return &m.svcList
	}
	return &m.projectList
}

func (m WizardModel) Update(msg tea.Msg) (WizardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MsgClustersLoaded:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err.Error()
			return m, nil
		}
		m.clusters = msg.Clusters
		if len(m.clusters) == 0 {
			m.err = "no clusters found in project"
			return m, nil
		}
		m.clusterList.setItems(m.clusterLabels())
		m.clusterList.filter.Focus()
		return m, textinput.Blink

	case MsgNamespacesLoaded:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err.Error()
			return m, nil
		}
		m.namespaces = msg.Namespaces
		m.nsList.setItems(m.namespaces)
		m.nsList.filter.Focus()
		return m, textinput.Blink

	case MsgServicesLoaded:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err.Error()
			return m, nil
		}
		m.services = msg.Services
		m.svcList.setItems(m.serviceLabels())
		m.svcList.filter.Focus()
		return m, textinput.Blink

	case tea.KeyMsg:
		if !m.visible {
			return m, nil
		}

		// global Esc handling
		if msg.String() == "esc" {
			if m.step == StepProject {
				m.visible = false
				return m, func() tea.Msg { return MsgWizardCancelled{} }
			}
			m.step--
			m.err = ""
			m.currentList().filter.Focus()
			return m, textinput.Blink
		}

		// Enter: advance step
		if msg.String() == "enter" {
			return m.handleEnter()
		}

		// Space: toggle selection (services only)
		if msg.String() == " " && m.step == StepServices {
			origIdx := m.svcList.selectedOrigIdx()
			if origIdx >= 0 {
				m.selectedSvcs[origIdx] = !m.selectedSvcs[origIdx]
			}
			return m, nil
		}

		// Delegate navigation + typing to the current list
		cmd, _ := m.currentList().update(msg)
		return m, cmd
	}
	return m, nil
}

func (m WizardModel) handleEnter() (WizardModel, tea.Cmd) {
	m.err = ""
	switch m.step {
	case StepProject:
		origIdx := m.projectList.selectedOrigIdx()
		if origIdx < 0 {
			return m, nil
		}
		m.selectedProject = m.projects[origIdx].ID
		m.step = StepCluster
		m.loading = true
		projectID := m.selectedProject
		return m, func() tea.Msg {
			clusters, err := gcp.ListClusters(context.Background(), projectID)
			return MsgClustersLoaded{Clusters: clusters, Err: err}
		}

	case StepCluster:
		origIdx := m.clusterList.selectedOrigIdx()
		if origIdx < 0 {
			return m, nil
		}
		m.selectedCluster = m.clusters[origIdx]
		m.step = StepNamespace
		m.loading = true
		cluster := m.selectedCluster
		return m, func() tea.Msg {
			restCfg, err := k8s.BuildRESTConfig(context.Background(), cluster)
			if err != nil {
				return MsgNamespacesLoaded{Namespaces: []string{"default"}}
			}
			cs, err := kubernetes.NewForConfig(restCfg)
			if err != nil {
				return MsgNamespacesLoaded{Namespaces: []string{"default"}}
			}
			nsList, err := cs.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return MsgNamespacesLoaded{Namespaces: []string{"default"}}
			}
			names := make([]string, 0, len(nsList.Items))
			for _, ns := range nsList.Items {
				names = append(names, ns.Name)
			}
			return MsgNamespacesLoaded{Namespaces: names}
		}

	case StepNamespace:
		origIdx := m.nsList.selectedOrigIdx()
		if origIdx >= 0 && origIdx < len(m.namespaces) {
			m.selectedNamespace = m.namespaces[origIdx]
		} else {
			m.selectedNamespace = "default"
		}
		m.step = StepServices
		m.loading = true
		cluster := m.selectedCluster
		ns := m.selectedNamespace
		return m, func() tea.Msg {
			restCfg, err := k8s.BuildRESTConfig(context.Background(), cluster)
			if err != nil {
				return MsgServicesLoaded{Err: err}
			}
			cs, err := kubernetes.NewForConfig(restCfg)
			if err != nil {
				return MsgServicesLoaded{Err: err}
			}
			svcList, err := cs.CoreV1().Services(ns).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return MsgServicesLoaded{Err: err}
			}
			return MsgServicesLoaded{Services: svcList.Items}
		}

	case StepServices:
		selected := m.getSelectedServices()
		if len(selected) == 0 {
			m.err = "select at least one service with Space"
			return m, nil
		}
		m.step = StepConfirm
		return m, nil

	case StepConfirm:
		selected := m.getSelectedServices()
		m.visible = false
		cluster := m.selectedCluster
		project := m.selectedProject
		ns := m.selectedNamespace
		return m, func() tea.Msg {
			return MsgWizardDone{
				Project:   project,
				Cluster:   cluster,
				Namespace: ns,
				Services:  selected,
			}
		}
	}
	return m, nil
}

func (m WizardModel) getSelectedServices() []corev1.Service {
	var out []corev1.Service
	for idx, sel := range m.selectedSvcs {
		if sel && idx < len(m.services) {
			out = append(out, m.services[idx])
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m WizardModel) View() string {
	if !m.visible {
		return ""
	}

	var content strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f9fafb"))
	stepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))

	stepNames := []string{"Project", "Cluster", "Namespace", "Services", "Confirm"}
	stepLabel := fmt.Sprintf("Step %d/%d: %s", int(m.step)+1, len(stepNames), stepNames[m.step])
	content.WriteString(titleStyle.Render("Add Port Forward") + "  " + stepStyle.Render(stepLabel))
	content.WriteString("\n\n")

	if m.err != "" {
		content.WriteString(ErrorTextStyle.Render("  "+m.err) + "\n\n")
	}

	if m.loading {
		content.WriteString(DimStyle.Render("  Loading..."))
	} else {
		switch m.step {
		case StepProject:
			content.WriteString(m.projectList.view("Select GCP Project", nil, false))
		case StepCluster:
			content.WriteString(m.clusterList.view("Select Cluster", nil, false))
		case StepNamespace:
			content.WriteString(m.nsList.view("Select Namespace", nil, false))
		case StepServices:
			selCount := 0
			for _, v := range m.selectedSvcs {
				if v {
					selCount++
				}
			}
			title := "Select Services"
			if selCount > 0 {
				title += lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Render(fmt.Sprintf("  %d selected", selCount))
			}
			content.WriteString(m.svcList.view(title, m.selectedSvcs, true))
		case StepConfirm:
			content.WriteString(m.renderConfirm())
		}
	}

	content.WriteString("\n")
	hint := "↑↓ navigate  Enter confirm  Esc back"
	if m.step == StepServices {
		hint = "↑↓ navigate  Space toggle  Enter confirm  Esc back  (type :PORT to filter by port)"
	}
	content.WriteString(DimStyle.Render(hint))

	return WizardOverlayStyle.Width(64).Render(content.String())
}

func (m WizardModel) renderConfirm() string {
	var sb strings.Builder
	sb.WriteString(DimStyle.Render("Review and confirm") + "\n\n")
	sb.WriteString(fmt.Sprintf("  Project:   %s\n", m.selectedProject))
	sb.WriteString(fmt.Sprintf("  Cluster:   %s\n", m.selectedCluster.Name))
	sb.WriteString(fmt.Sprintf("  Namespace: %s\n\n", m.selectedNamespace))
	sb.WriteString("  Services to forward:\n")

	for origIdx, sel := range m.selectedSvcs {
		if sel && origIdx < len(m.services) {
			svc := m.services[origIdx]
			port := 0
			if len(svc.Spec.Ports) > 0 {
				port = int(svc.Spec.Ports[0].Port)
			}
			sb.WriteString(fmt.Sprintf("    • %s  :%d\n", svc.Name, port))
		}
	}

	sb.WriteString("\n")
	sb.WriteString(DimStyle.Render("Press Enter to start all forwards"))
	return sb.String()
}

// ---------------------------------------------------------------------------
// Label helpers
// ---------------------------------------------------------------------------

func (m WizardModel) projectLabels() []string {
	labels := make([]string, len(m.projects))
	for i, p := range m.projects {
		labels[i] = fmt.Sprintf("%s  (%s)", p.Label, p.ID)
	}
	return labels
}

func (m WizardModel) clusterLabels() []string {
	labels := make([]string, len(m.clusters))
	for i, c := range m.clusters {
		labels[i] = fmt.Sprintf("%-30s  [%s]", c.EnvName, c.Location)
	}
	return labels
}

func (m WizardModel) serviceLabels() []string {
	labels := make([]string, len(m.services))
	for i, svc := range m.services {
		var ports []string
		for _, p := range svc.Spec.Ports {
			ports = append(ports, fmt.Sprintf(":%d", p.Port))
		}
		portStr := strings.Join(ports, " ")
		labels[i] = fmt.Sprintf("%-38s %s", svc.Name, portStr)
	}
	return labels
}
