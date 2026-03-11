package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geraldofigueiredo/kportmaster/internal/k8s"
)

const maxServiceLogLines = 500

// parsedLogLine holds a structured representation of a single log line.
//
// Python services write structured logs with an "extra" sub-object containing
// the business payload. Go services write flat JSON where all fields are primary.
// Both patterns are handled: if an "extra" key is present, its contents become
// primaryExtra and the remaining fields become metaFields; otherwise all
// remaining fields are primaryExtra with no metaFields.
type parsedLogLine struct {
	raw          string   // original text — used for filter matching
	time         string   // "HH:MM:SS" extracted from timestamp field, or ""
	severity     string   // "INFO"/"WARN"/"ERROR"/etc. or ""
	message      string   // primary message; raw text if not JSON
	primaryExtra []string // python: "extra" sub-object; go: all remaining fields
	metaFields   []string // python: non-extra remaining fields; go: empty
	isJSON       bool
}

var logTimeFields = []string{"timestamp", "time", "ts", "@timestamp"}
var logSeverityFields = []string{"severity", "level", "lvl", "loglevel"}
var logMessageFields = []string{"message", "msg", "text", "textPayload"}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// flattenValue converts any JSON value to a human-readable string.
func flattenValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

func parseLogLine(raw string) parsedLogLine {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return parsedLogLine{raw: raw, message: raw}
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		return parsedLogLine{raw: raw, message: raw}
	}

	p := parsedLogLine{raw: raw, isJSON: true}

	// Extract time.
	for _, key := range logTimeFields {
		if v, ok := fields[key]; ok {
			if s, ok := v.(string); ok {
				p.time = formatLogTime(s)
			}
			delete(fields, key)
			break
		}
	}

	// Extract severity.
	for _, key := range logSeverityFields {
		if v, ok := fields[key]; ok {
			switch val := v.(type) {
			case string:
				p.severity = normalizeSeverity(val)
			case float64:
				p.severity = pinoLevelToSeverity(int(val))
			}
			delete(fields, key)
			break
		}
	}

	// Extract message.
	for _, key := range logMessageFields {
		if v, ok := fields[key]; ok {
			if s, ok := v.(string); ok {
				p.message = s
			} else {
				p.message = fmt.Sprintf("%v", v)
			}
			delete(fields, key)
			break
		}
	}
	if p.message == "" {
		p.message = trimmed
	}

	// Python-style: "extra" sub-object holds business payload → primaryExtra.
	// Remaining fields (file, function, elapsed, etc.) → metaFields.
	// Go-style: no "extra" key → all remaining fields are primaryExtra.
	if extraMap, ok := fields["extra"].(map[string]any); ok {
		delete(fields, "extra")
		for _, k := range sortedKeys(extraMap) {
			p.primaryExtra = append(p.primaryExtra, fmt.Sprintf("%s: %s", k, flattenValue(extraMap[k])))
		}
		for _, k := range sortedKeys(fields) {
			p.metaFields = append(p.metaFields, fmt.Sprintf("%s: %s", k, flattenValue(fields[k])))
		}
	} else {
		for _, k := range sortedKeys(fields) {
			p.primaryExtra = append(p.primaryExtra, fmt.Sprintf("%s: %s", k, flattenValue(fields[k])))
		}
	}

	return p
}

func formatLogTime(s string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("15:04:05")
		}
	}
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func normalizeSeverity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TRACE", "TRC":
		return "TRACE"
	case "DEBUG", "DBG", "DEBU":
		return "DEBUG"
	case "INFO", "INFORMATION":
		return "INFO"
	case "WARN", "WARNING":
		return "WARN"
	case "ERROR", "ERR":
		return "ERROR"
	case "FATAL", "CRIT", "CRITICAL", "PANIC":
		return "FATAL"
	default:
		return strings.ToUpper(s)
	}
}

func pinoLevelToSeverity(n int) string {
	switch {
	case n <= 10:
		return "TRACE"
	case n <= 20:
		return "DEBUG"
	case n <= 30:
		return "INFO"
	case n <= 40:
		return "WARN"
	case n <= 50:
		return "ERROR"
	default:
		return "FATAL"
	}
}

// detailField represents a single navigable row in the detail overlay.
// isSep=true marks a section header (not selectable, not copyable).
type detailField struct {
	key   string
	value string
	isSep bool
}

// splitKV splits "key: value" into (key, value).  If no ": " is found the
// whole string is returned as the key and value is "".
func splitKV(kv string) (string, string) {
	idx := strings.Index(kv, ": ")
	if idx < 0 {
		return kv, ""
	}
	return kv[:idx], kv[idx+2:]
}

// buildDetailFields constructs the full navigable field list for a log entry.
// The first three rows are always timestamp / severity / message (the fixed
// header fields). Extra and meta fields follow with section separators.
func buildDetailFields(p parsedLogLine) []detailField {
	var fields []detailField
	if p.time != "" {
		fields = append(fields, detailField{key: "timestamp", value: p.time})
	}
	if p.severity != "" {
		fields = append(fields, detailField{key: "severity", value: p.severity})
	}
	fields = append(fields, detailField{key: "message", value: p.message})

	if len(p.primaryExtra) > 0 {
		label := "CONTEXT"
		if len(p.metaFields) == 0 {
			label = "FIELDS"
		}
		fields = append(fields, detailField{isSep: true, key: label})
		for _, kv := range p.primaryExtra {
			k, v := splitKV(kv)
			fields = append(fields, detailField{key: k, value: v})
		}
	}
	if len(p.metaFields) > 0 {
		fields = append(fields, detailField{isSep: true, key: "METADATA"})
		for _, kv := range p.metaFields {
			k, v := splitKV(kv)
			fields = append(fields, detailField{key: k, value: v})
		}
	}
	return fields
}

// selectableCount counts the non-separator fields in a detail field list.
func selectableCount(fields []detailField) int {
	n := 0
	for _, f := range fields {
		if !f.isSep {
			n++
		}
	}
	return n
}

// truncateRunes truncates s to maxRunes rune positions, appending "…" if cut.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

// clearCopyFeedbackAfterDelay fires MsgClearCopyFeedback after 2 seconds.
func clearCopyFeedbackAfterDelay() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return MsgClearCopyFeedback{}
	})
}

// ServiceLogsModel streams and displays pod logs for the selected tunnel.
type ServiceLogsModel struct {
	viewport       viewport.Model
	filterInput    textinput.Model
	allLines       []parsedLogLine
	displayedLines []parsedLogLine
	cursor         int
	showDetail     bool
	detailFieldCursor int    // index into selectable fields in the detail overlay
	detailCopied      string // key of last copied field, shown as feedback

	// Pod selection state.
	availablePods   []k8s.PodInfo
	podPickerCursor int
	showPodPicker   bool
	// selectedPod overrides the tunnel's CurrentPod when set.
	selectedPodName      string
	selectedPodContainer string

	streamErr string // last stream error, shown as status line

	tunnelID    string
	tunnelLabel string
	width       int
	height      int
	focused     bool
	follow      bool
}

func NewServiceLogsModel(width, height int) ServiceLogsModel {
	vp := viewport.New(max(width-4, 1), max(height-3, 1))
	fi := textinput.New()
	fi.Placeholder = "filter..."
	fi.CharLimit = 64
	fi.Width = 20
	return ServiceLogsModel{
		viewport:    vp,
		filterInput: fi,
		allLines:    []parsedLogLine{},
		follow:      true,
		width:       width,
		height:      height,
	}
}

// DetailVisible reports whether the full-screen detail overlay should be shown.
func (m ServiceLogsModel) DetailVisible() bool {
	return m.showDetail && len(m.displayedLines) > 0
}

// PodPickerVisible reports whether the pod picker overlay is shown.
func (m ServiceLogsModel) PodPickerVisible() bool {
	return m.showPodPicker
}

// SelectedPodName returns the pod to stream logs from (override or tunnel default).
func (m ServiceLogsModel) SelectedPodName() string {
	if m.selectedPodName != "" {
		return m.selectedPodName
	}
	return ""
}

// SelectedPodContainer returns the container to stream logs from.
func (m ServiceLogsModel) SelectedPodContainer() string {
	if m.selectedPodName != "" {
		return m.selectedPodContainer
	}
	return ""
}

// SetTunnel clears lines and updates the label/state for the new tunnel.
func (m *ServiceLogsModel) SetTunnel(t *k8s.Tunnel) {
	if t == nil {
		m.tunnelID = ""
		m.tunnelLabel = ""
	} else {
		m.tunnelID = t.ID
		m.tunnelLabel = fmt.Sprintf("%s [%s]", t.ID, t.Namespace)
	}
	m.allLines = m.allLines[:0]
	m.displayedLines = nil
	m.cursor = 0
	m.showDetail = false
	m.detailFieldCursor = 0
	m.detailCopied = ""
	m.selectedPodName = ""
	m.selectedPodContainer = ""
	m.availablePods = nil
	m.showPodPicker = false
	m.streamErr = ""
	m.follow = true
	m.refreshViewport()
}

// UpdateTunnelState syncs label and running state without clearing log lines.
func (m *ServiceLogsModel) UpdateTunnelState(t *k8s.Tunnel) {
	if t == nil {
		m.refreshViewport()
		return
	}
	if t.ID != m.tunnelID {
		return
	}
	m.tunnelLabel = fmt.Sprintf("%s [%s]", t.ID, t.Namespace)
	m.refreshViewport()
}

// SetPods updates the available pod list (called when MsgPodsLoaded arrives).
func (m *ServiceLogsModel) SetPods(tunnelID string, pods []k8s.PodInfo) {
	if tunnelID != m.tunnelID {
		return
	}
	m.availablePods = pods
	// Auto-select the first pod when no pod is chosen yet.
	// This lets logs stream immediately for stopped/paused tunnels.
	if m.selectedPodName == "" && len(pods) > 0 {
		m.selectedPodName = pods[0].Name
		m.selectedPodContainer = pods[0].Container
	}
}

// NeedsStreamRestart reports whether a log stream should be started.
// True when no log lines have arrived and there is no active stream error.
func (m ServiceLogsModel) NeedsStreamRestart() bool {
	return len(m.allLines) == 0 && m.streamErr == ""
}

// SetStreamError sets the stream error message for display.
func (m *ServiceLogsModel) SetStreamError(tunnelID, errMsg string) {
	if tunnelID != m.tunnelID {
		return
	}
	m.streamErr = errMsg
	m.refreshViewport()
}

// AddLine appends a log line if the tunnelID matches.
func (m ServiceLogsModel) AddLine(tunnelID, line string) ServiceLogsModel {
	if tunnelID != m.tunnelID {
		return m
	}
	m.streamErr = "" // clear error on successful line
	m.allLines = append(m.allLines, parseLogLine(line))
	if len(m.allLines) > maxServiceLogLines {
		m.allLines = m.allLines[len(m.allLines)-maxServiceLogLines:]
	}
	m.refreshViewport()
	if m.follow {
		m.cursor = max(0, len(m.displayedLines)-1)
		m.viewport.GotoBottom()
	}
	return m
}

func (m ServiceLogsModel) Update(msg tea.Msg) (ServiceLogsModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		// Pod picker captures all keys.
		if m.showPodPicker {
			switch msg.String() {
			case "esc", "q":
				m.showPodPicker = false
			case "up", "k":
				if m.podPickerCursor > 0 {
					m.podPickerCursor--
				}
			case "down", "j":
				if m.podPickerCursor < len(m.availablePods)-1 {
					m.podPickerCursor++
				}
			case "enter":
				if m.podPickerCursor < len(m.availablePods) {
					pod := m.availablePods[m.podPickerCursor]
					m.selectedPodName = pod.Name
					m.selectedPodContainer = pod.Container
					m.showPodPicker = false
					m.allLines = m.allLines[:0]
					m.displayedLines = nil
					m.streamErr = ""
					m.refreshViewport()
					// Signal to restart stream with new pod.
					cmds = append(cmds, func() tea.Msg {
						return MsgServiceLogsEnded{TunnelID: m.tunnelID}
					})
				}
			}
			return m, tea.Batch(cmds...)
		}

		// Full-screen detail overlay captures all keys.
		if m.showDetail {
			switch msg.String() {
			case "esc", "q", "enter":
				m.showDetail = false
				m.detailFieldCursor = 0
				m.detailCopied = ""
			case "up", "k":
				if m.detailFieldCursor > 0 {
					m.detailFieldCursor--
				}
			case "down", "j":
				if len(m.displayedLines) > 0 {
					fields := buildDetailFields(m.displayedLines[m.cursor])
					if m.detailFieldCursor < selectableCount(fields)-1 {
						m.detailFieldCursor++
					}
				}
			case "c", "y":
				if len(m.displayedLines) > 0 {
					fields := buildDetailFields(m.displayedLines[m.cursor])
					si := 0
					for _, f := range fields {
						if !f.isSep {
							if si == m.detailFieldCursor {
								if err := copyToClipboard(f.value); err == nil {
									m.detailCopied = f.key
									cmds = append(cmds, clearCopyFeedbackAfterDelay())
								}
								break
							}
							si++
						}
					}
				}
			case "C":
				if len(m.displayedLines) > 0 {
					if err := copyToClipboard(m.displayedLines[m.cursor].raw); err == nil {
						m.detailCopied = "raw JSON"
						cmds = append(cmds, clearCopyFeedbackAfterDelay())
					}
				}
			case "left", "h":
				if m.cursor > 0 {
					m.follow = false
					m.cursor--
					m.detailFieldCursor = 0
					m.detailCopied = ""
					m.refreshViewport()
				}
			case "right", "l":
				if m.cursor < len(m.displayedLines)-1 {
					m.follow = false
					m.cursor++
					m.detailFieldCursor = 0
					m.detailCopied = ""
					m.refreshViewport()
				}
			}
			return m, tea.Batch(cmds...)
		}

		if m.filterInput.Focused() {
			switch msg.String() {
			case "esc":
				m.filterInput.Blur()
				m.filterInput.SetValue("")
				m.refreshViewport()
				if m.follow {
					m.viewport.GotoBottom()
				}
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				cmds = append(cmds, cmd)
				m.refreshViewport()
				if m.follow {
					m.viewport.GotoBottom()
				}
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "/":
			cmd := m.filterInput.Focus()
			cmds = append(cmds, cmd)
		case "enter":
			if len(m.displayedLines) > 0 {
				m.showDetail = true
				m.detailFieldCursor = 0
				m.detailCopied = ""
			}
		case "p":
			// Open pod picker if pods are available.
			if len(m.availablePods) > 0 {
				m.showPodPicker = true
				m.podPickerCursor = 0
				// Pre-select current pod in picker.
				for i, pod := range m.availablePods {
					if pod.Name == m.selectedPodName {
						m.podPickerCursor = i
						break
					}
				}
			}
		case "G":
			m.follow = true
			m.cursor = max(0, len(m.displayedLines)-1)
			m.viewport.GotoBottom()
		case "up", "k":
			m.follow = false
			if m.cursor > 0 {
				m.cursor--
			}
			m.refreshViewport()
		case "down", "j":
			m.follow = false
			if m.cursor < len(m.displayedLines)-1 {
				m.cursor++
			}
			m.refreshViewport()
		case "esc":
			// no-op in normal mode
		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}

	case MsgClearCopyFeedback:
		m.detailCopied = ""

	default:
		if m.focused {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *ServiceLogsModel) Resize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = max(width-4, 1)
	m.viewport.Height = max(height-3, 1)
	m.refreshViewport()
}

func (m *ServiceLogsModel) SetFocused(b bool) {
	m.focused = b
}

// View renders the log panel (without the detail overlay — that is handled by
// the root Model.View() as a full-screen layer).
func (m ServiceLogsModel) View() string {
	borderColor := lipgloss.Color("#6b7280")
	if m.focused {
		borderColor = lipgloss.Color("#3b82f6")
	}

	title := "SERVICE LOGS"
	if m.tunnelLabel != "" {
		title += "  " + DimStyle.Render(m.tunnelLabel)
	}

	filterPart := ""
	if m.filterInput.Focused() {
		filterPart = "  filter: " + m.filterInput.View()
	} else if m.filterInput.Value() != "" {
		filterPart = "  " + DimStyle.Render("filter: "+m.filterInput.Value())
	}

	// Pod hint: show which pod is selected (always, when pods are known).
	podHint := ""
	if len(m.availablePods) > 0 {
		podName := m.selectedPodName
		if podName == "" && len(m.availablePods) > 0 {
			podName = m.availablePods[0].Name
		}
		// Show just the last segment of the pod name for brevity.
		short := podName
		if idx := strings.LastIndex(podName, "-"); idx > 0 {
			short = podName[idx+1:]
		}
		action := "pod"
		if len(m.availablePods) > 1 {
			action = "[p] pod"
		}
		podHint = "  " + DimStyle.Render(fmt.Sprintf("%s: ...%s (%d)", action, short, len(m.availablePods)))
	}

	titleLine := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9fafb")).Bold(true).Render(title) +
		filterPart + podHint

	// Show panel-specific shortcuts when focused and not in filter mode.
	if m.focused && !m.filterInput.Focused() {
		titleLine += "  " + DimStyle.Render("[/]filter  [↵]detail  [j/k]nav  [G]follow")
	}

	inner := titleLine + "\n" + m.viewport.View()

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width - 2).
		Height(m.height - 2).
		Render(inner)
}

// DetailView renders the full-screen log-entry detail overlay.
// Called by the root Model.View() when DetailVisible() is true.
func (m ServiceLogsModel) DetailView(width, height int) string {
	if len(m.displayedLines) == 0 {
		return ""
	}
	p := m.displayedLines[m.cursor]
	fields := buildDetailFields(p)

	boxW := max(width*85/100, 60)
	// contentW = boxW minus border(2) and padding left+right(4)
	contentW := boxW - 6
	if contentW < 30 {
		contentW = 30
	}

	// Compute key-column width from actual keys (min 9 = "timestamp").
	maxKeyLen := 9
	for _, f := range fields {
		if !f.isSep && len(f.key) > maxKeyLen {
			maxKeyLen = len(f.key)
		}
	}
	if maxKeyLen > 30 {
		maxKeyLen = 30
	}
	// cursor(2) + key + gap(2) + value
	valW := contentW - 2 - maxKeyLen - 2
	if valW < 15 {
		valW = 15
	}

	// Styles.
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f1f5f9"))
	sepHdrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	selKeyStyle := lipgloss.NewStyle().Background(lipgloss.Color("#1e3a5f")).Foreground(lipgloss.Color("#93c5fd"))
	selValStyle := lipgloss.NewStyle().Background(lipgloss.Color("#1e3a5f")).Foreground(lipgloss.Color("#e2e8f0"))
	copiedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))

	// Summary header line: time  ● SEVERITY  message…
	timeStr := p.time
	if timeStr == "" {
		timeStr = "──────"
	}
	sevBadge := renderSeverityBadge(p.severity)
	msgSummary := truncateRunes(p.message, max(10, contentW-20))
	headerLine := DimStyle.Render(timeStr) + "  " + sevBadge + "  " + valStyle.Render(msgSummary)
	separatorLine := sepHdrStyle.Render(strings.Repeat("─", contentW))

	// Build body rows tracking which line the cursor falls on.
	type bodyRow struct {
		text        string
		selectable  bool
	}
	var body []bodyRow
	selectIdx := 0
	cursorBodyIdx := 0
	for _, f := range fields {
		if f.isSep {
			label := "  " + f.key + " "
			fill := strings.Repeat("─", max(0, contentW-len(label)-1))
			body = append(body, bodyRow{text: sepHdrStyle.Render(label + fill)})
			continue
		}
		selected := selectIdx == m.detailFieldCursor
		kTrunc := truncateRunes(f.key, maxKeyLen)
		vTrunc := truncateRunes(f.value, valW)
		kPad := fmt.Sprintf("%-*s", maxKeyLen, kTrunc)
		var row string
		if selected {
			cursorBodyIdx = len(body)
			row = "▶ " + selKeyStyle.Render(kPad) + "  " + selValStyle.Render(fmt.Sprintf("%-*s", valW, vTrunc))
		} else {
			row = "  " + keyStyle.Render(kPad) + "  " + valStyle.Render(vTrunc)
		}
		body = append(body, bodyRow{text: row, selectable: true})
		selectIdx++
	}

	// Fixed lines: header(1) + sep(1) + blank(1) above body; blank(1) + footer(1) + feedback(1) below.
	const fixedAbove = 3
	const fixedBelow = 3
	// box overhead = border(2) + padding top+bottom(2)
	const boxOverhead = 4
	boxH := max(height*80/100, 16)
	visibleBodyH := boxH - boxOverhead - fixedAbove - fixedBelow
	if visibleBodyH < 4 {
		visibleBodyH = 4
	}

	// Scroll body so cursor is centered.
	scrollOffset := cursorBodyIdx - visibleBodyH/2
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	if scrollOffset > len(body)-visibleBodyH {
		scrollOffset = max(0, len(body)-visibleBodyH)
	}
	end := scrollOffset + visibleBodyH
	if end > len(body) {
		end = len(body)
	}
	visibleBody := body[scrollOffset:end]
	// Pad to fill.
	for len(visibleBody) < visibleBodyH {
		visibleBody = append(visibleBody, bodyRow{text: ""})
	}

	bodyLines := make([]string, len(visibleBody))
	for i, br := range visibleBody {
		bodyLines[i] = br.text
	}

	// Footer: keybindings + scroll position + entry position.
	scrollInfo := ""
	if len(body) > visibleBodyH {
		scrollInfo = "  " + DimStyle.Render(fmt.Sprintf("%d/%d", scrollOffset+visibleBodyH, len(body)))
	}
	posInfo := ""
	if len(m.displayedLines) > 1 {
		posInfo = "  " + DimStyle.Render(fmt.Sprintf("entry %d/%d", m.cursor+1, len(m.displayedLines)))
	}
	footer := DimStyle.Render("[j/k] field  [c] copy  [C] copy raw  [←/→] entry  [esc] close") +
		scrollInfo + posInfo

	// Copy feedback.
	feedback := ""
	if m.detailCopied != "" {
		feedback = "\n" + copiedStyle.Render("✓ copied: "+m.detailCopied)
	}

	content := headerLine + "\n" +
		separatorLine + "\n\n" +
		strings.Join(bodyLines, "\n") + "\n\n" +
		footer + feedback

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Width(boxW - 2).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0f172a")),
		lipgloss.WithWhitespaceChars(" "),
	)
}

// renderSeverityBadge returns a colored "● LEVEL" badge for the detail header.
func renderSeverityBadge(level string) string {
	var color lipgloss.Color
	switch level {
	case "ERROR", "FATAL":
		color = lipgloss.Color("#ef4444")
	case "WARN":
		color = lipgloss.Color("#f59e0b")
	case "INFO":
		color = lipgloss.Color("#22c55e")
	case "DEBUG", "TRACE":
		color = lipgloss.Color("#94a3b8")
	default:
		if level == "" {
			return DimStyle.Render("●")
		}
		color = lipgloss.Color("#94a3b8")
	}
	return lipgloss.NewStyle().Foreground(color).Render("● " + level)
}

// PodPickerView renders the full-screen pod picker overlay.
func (m ServiceLogsModel) PodPickerView(width, height int) string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#94a3b8"))

	var lines []string
	lines = append(lines, keyStyle.Render("Select pod to stream logs from:"))
	lines = append(lines, "")
	for i, pod := range m.availablePods {
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == m.podPickerCursor {
			prefix = "▶ "
			style = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e293b")).
				Foreground(lipgloss.Color("#f1f5f9"))
		}
		label := fmt.Sprintf("%s  [%s]", pod.Name, pod.Container)
		// Mark currently selected pod.
		if pod.Name == m.selectedPodName {
			label += "  " + DimStyle.Render("(current)")
		}
		lines = append(lines, prefix+style.Render(label))
	}
	lines = append(lines, "")
	lines = append(lines, DimStyle.Render("[↑/↓] navigate  [enter] select  [esc] cancel"))

	content := strings.Join(lines, "\n")

	boxW := max(width*70/100, 50)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Width(boxW - 2).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0f172a")),
		lipgloss.WithWhitespaceChars(" "),
	)
}

// renderLine formats a single log line for display in the viewport.
func (m *ServiceLogsModel) renderLine(i int, p parsedLogLine) string {
	cursor := "  "
	lineStyle := lipgloss.NewStyle()
	if i == m.cursor {
		cursor = "▶ "
		lineStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e293b")).
			Foreground(lipgloss.Color("#f1f5f9"))
	}

	timeStr := fmt.Sprintf("%-8s", p.time)
	sevStr := fmt.Sprintf("%-5s", p.severity)

	timeRendered := DimStyle.Render(timeStr)
	sevRendered := renderSeverity(sevStr, p.severity)

	// Available width for message: total - cursor(2) - time(8) - sep(2) - sev(5) - sep(2) - badge(8)
	msgWidth := m.viewport.Width - 2 - 8 - 2 - 5 - 2 - 8
	if msgWidth < 10 {
		msgWidth = 10
	}

	msgText := p.message
	if len(msgText) > msgWidth {
		msgText = msgText[:msgWidth-1] + "…"
	}
	msgRendered := lineStyle.Render(fmt.Sprintf("%-*s", msgWidth, msgText))

	badge := ""
	if n := len(p.primaryExtra); n > 0 {
		badge = DimStyle.Render(fmt.Sprintf("[+%d]", n))
	}

	return cursor + timeRendered + "  " + sevRendered + "  " + msgRendered + "  " + badge
}

func renderSeverity(s, level string) string {
	var color lipgloss.Color
	switch level {
	case "ERROR", "FATAL":
		color = lipgloss.Color("#ef4444")
	case "WARN":
		color = lipgloss.Color("#f59e0b")
	default:
		return DimStyle.Render(s)
	}
	return lipgloss.NewStyle().Foreground(color).Render(s)
}

// refreshViewport rebuilds the viewport content from filtered lines.
func (m *ServiceLogsModel) refreshViewport() {
	if m.tunnelID == "" {
		m.viewport.SetContent(DimStyle.Render("  select a running tunnel to stream logs"))
		return
	}
	if m.streamErr != "" {
		m.viewport.SetContent(lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Render("  stream error: " + m.streamErr))
		return
	}

	filter := strings.ToLower(m.filterInput.Value())
	displayed := m.displayedLines[:0]
	for _, l := range m.allLines {
		if filter == "" || strings.Contains(strings.ToLower(l.raw), filter) {
			displayed = append(displayed, l)
		}
	}
	m.displayedLines = displayed

	if len(m.displayedLines) == 0 {
		if filter != "" {
			m.viewport.SetContent(DimStyle.Render("  no lines match filter"))
		} else {
			m.viewport.SetContent(DimStyle.Render("  waiting for logs…"))
		}
		return
	}

	// When following, always jump to the most recent line; otherwise clamp.
	if m.follow {
		m.cursor = len(m.displayedLines) - 1
	} else if m.cursor >= len(m.displayedLines) {
		m.cursor = len(m.displayedLines) - 1
	}

	rendered := make([]string, len(m.displayedLines))
	for i, p := range m.displayedLines {
		rendered[i] = m.renderLine(i, p)
	}
	m.viewport.SetContent(strings.Join(rendered, "\n"))
	// Center the cursor in the viewport (SetContent resets offset to 0).
	offset := m.cursor - m.viewport.Height/2
	if offset < 0 {
		offset = 0
	}
	m.viewport.SetYOffset(offset)
}

// streamServiceLogs opens a log stream for the given tunnel and returns a Cmd.
// podName/containerName override t.CurrentPod/t.CurrentContainer when non-empty.
func streamServiceLogs(ctx context.Context, t *k8s.Tunnel, podName, containerName string) tea.Cmd {
	tunnelID := t.ID
	namespace := t.Namespace
	if podName == "" {
		podName = t.CurrentPod
	}
	if containerName == "" {
		containerName = t.CurrentContainer
	}
	return func() tea.Msg {
		restCfg, err := t.RESTConfigOrBuild(ctx)
		if err != nil {
			return MsgServiceLogStreamErr{TunnelID: tunnelID, Err: fmt.Errorf("cluster credentials: %w", err)}
		}
		if restCfg == nil {
			return MsgServiceLogStreamErr{TunnelID: tunnelID, Err: fmt.Errorf("no cluster credentials — resume tunnel to connect")}
		}
		if podName == "" {
			return MsgServiceLogStreamErr{TunnelID: tunnelID, Err: fmt.Errorf("no pod selected")}
		}
		stream, err := k8s.StreamPodLogs(ctx, restCfg, namespace, podName, containerName, 100)
		if err != nil {
			return MsgServiceLogStreamErr{TunnelID: tunnelID, Err: err}
		}
		return MsgServiceLogStreamReady{TunnelID: tunnelID, Stream: stream}
	}
}

// listPodsForService returns a Cmd that fetches available pods for the tunnel's service.
func listPodsForService(t *k8s.Tunnel) tea.Cmd {
	tunnelID := t.ID
	namespace := t.Namespace
	serviceName := t.ID
	return func() tea.Msg {
		restCfg, err := t.RESTConfigOrBuild(context.Background())
		if err != nil || restCfg == nil {
			return nil
		}
		pods, err := k8s.ListPodsForService(context.Background(), restCfg, namespace, serviceName)
		if err != nil || len(pods) == 0 {
			return nil
		}
		return MsgPodsLoaded{TunnelID: tunnelID, Pods: pods}
	}
}

// readNextLogLine returns a Cmd that reads one line from scanner.
// The scanner is captured by closure and persists across invocations via msg.next.
func readNextLogLine(tunnelID string, scanner *bufio.Scanner) tea.Cmd {
	return func() tea.Msg {
		if scanner.Scan() {
			return MsgServiceLogLine{
				TunnelID: tunnelID,
				Line:     scanner.Text(),
				next:     readNextLogLine(tunnelID, scanner),
			}
		}
		return MsgServiceLogsEnded{TunnelID: tunnelID}
	}
}
