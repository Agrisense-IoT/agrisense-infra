package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agrisense/internal/config"
)

// ---------------------------------------------------------------------------
// Colors (inline — avoids import cycle with internal/tui/styles.go)
// ---------------------------------------------------------------------------

var (
	envColorText     = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	envColorMuted    = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	envColorSubtle   = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
	envColorGreen    = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	envColorYellow   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	envColorBlue     = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	envColorCyan     = lipgloss.AdaptiveColor{Light: "#0598bc", Dark: "#39c5cf"}
	envColorOrange   = lipgloss.AdaptiveColor{Light: "#bc4c00", Dark: "#e3b341"}
	envColorSelectBg = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1d3a5e"}
)

// ---------------------------------------------------------------------------
// Internal messages
// ---------------------------------------------------------------------------

type envLoadMsg struct {
	sections []config.EnvSection
	err      error
}

type envWriteMsg struct {
	mode    envEditorSaveMode
	err     error
}

type envStatusClearMsg struct{ gen int }

type envEditorSaveMode int

const (
	envEditorSaveOnly    envEditorSaveMode = iota
	envEditorSaveRestart envEditorSaveMode = iota
	envEditorSaveRebuild envEditorSaveMode = iota
)

// ---------------------------------------------------------------------------
// Exported messages consumed by app.go
// ---------------------------------------------------------------------------

// EnvEditorShowSaveMenuMsg asks app.go to display the env save/discard popup.
type EnvEditorShowSaveMenuMsg struct{}

// EnvEditorSaveRestartMsg asks app.go to restart containers after save.
type EnvEditorSaveRestartMsg struct{}

// EnvEditorSaveRebuildMsg asks app.go to rebuild containers after save.
type EnvEditorSaveRebuildMsg struct{}

// ---------------------------------------------------------------------------
// Local keybindings
// ---------------------------------------------------------------------------

type envKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Enter       key.Binding
	Generate    key.Binding
	Save        key.Binding
	SaveRestart key.Binding
	SaveRebuild key.Binding
	Esc         key.Binding
}

var envKeys = envKeyMap{
	Up:          key.NewBinding(key.WithKeys("up")),
	Down:        key.NewBinding(key.WithKeys("down")),
	PageUp:      key.NewBinding(key.WithKeys("pgup")),
	PageDown:    key.NewBinding(key.WithKeys("pgdown")),
	Enter:       key.NewBinding(key.WithKeys("enter")),
	Generate:    key.NewBinding(key.WithKeys("g", "G")),
	Save:        key.NewBinding(key.WithKeys("ctrl+s")),
	SaveRestart: key.NewBinding(key.WithKeys("ctrl+r")),
	SaveRebuild: key.NewBinding(key.WithKeys("ctrl+b")),
	Esc:         key.NewBinding(key.WithKeys("esc")),
}

// ---------------------------------------------------------------------------
// EnvEditorModel
// ---------------------------------------------------------------------------

// EnvEditorModel is the full-screen .env editor.
type EnvEditorModel struct {
	Width       int
	Height      int
	EnvPath     string // set by app.go
	ExamplePath string // set by app.go

	sections []config.EnvSection // current in-memory state
	saved    []config.EnvSection // snapshot at last save (for dirty tracking)
	cursor   int                 // index across field items only
	scroll   int                 // first visible row in the content area

	editing bool
	input   textinput.Model

	statusMsg string
	statusGen int

	loading bool
	loaded  bool
	loadErr string
}

func (m EnvEditorModel) Init() tea.Cmd { return nil }

func (m EnvEditorModel) Update(msg tea.Msg) (EnvEditorModel, tea.Cmd) {
	// Lazy load on first Update.
	if !m.loaded && !m.loading {
		m.loading = true
		return m, m.loadCmd()
	}

	if m.editing {
		return m.updateEditing(msg)
	}

	switch msg := msg.(type) {
	case envLoadMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err.Error()
			return m, nil
		}
		m.sections = msg.sections
		m.saved = deepCopySections(msg.sections)
		m.loaded = true
		return m, nil

	case envWriteMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusGen++
			return m, envStatusClearCmd(m.statusGen)
		}
		m.saved = deepCopySections(m.sections)
		switch msg.mode {
		case envEditorSaveOnly:
			m.statusMsg = "✓ Saved"
			m.statusGen++
			return m, envStatusClearCmd(m.statusGen)
		case envEditorSaveRestart:
			return m, func() tea.Msg { return EnvEditorSaveRestartMsg{} }
		case envEditorSaveRebuild:
			return m, func() tea.Msg { return EnvEditorSaveRebuildMsg{} }
		}

	case envStatusClearMsg:
		if msg.gen == m.statusGen {
			m.statusMsg = ""
		}
		return m, nil

	case tea.KeyMsg:
		if m.loaded {
			return m.handleKey(msg)
		}
	}

	return m, nil
}

// updateEditing handles messages while an inline field edit is active.
func (m EnvEditorModel) updateEditing(msg tea.Msg) (EnvEditorModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, envKeys.Enter):
			val := m.input.Value()
			if f := m.currentField(); f != nil {
				f.Value = val
				f.IsPlaceholder = strings.HasPrefix(val, "<") && strings.HasSuffix(val, ">")
			}
			m.editing = false
			return m, nil

		case key.Matches(km, envKeys.Esc):
			m.editing = false
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleKey handles keyboard input in navigation mode.
func (m EnvEditorModel) handleKey(msg tea.KeyMsg) (EnvEditorModel, tea.Cmd) {
	total := m.fieldCount()

	switch {
	case key.Matches(msg, envKeys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible()
		}

	case key.Matches(msg, envKeys.Down):
		if m.cursor < total-1 {
			m.cursor++
			m.ensureVisible()
		}

	case key.Matches(msg, envKeys.PageUp):
		step := m.contentHeight()
		if step < 1 {
			step = 10
		}
		m.cursor -= step
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureVisible()

	case key.Matches(msg, envKeys.PageDown):
		step := m.contentHeight()
		if step < 1 {
			step = 10
		}
		m.cursor += step
		if m.cursor >= total {
			m.cursor = max(0, total-1)
		}
		m.ensureVisible()

	case key.Matches(msg, envKeys.Enter):
		f := m.currentField()
		if f == nil {
			break
		}
		ti := textinput.New()
		if f.IsSensitive {
			ti.EchoMode = textinput.EchoPassword
		}
		ti.SetValue(f.Value)
		ti.Focus()
		m.input = ti
		m.editing = true

	case key.Matches(msg, envKeys.Generate):
		return m.generateCurrent()

	case key.Matches(msg, envKeys.Save):
		return m, m.saveCmd(envEditorSaveOnly)

	case key.Matches(msg, envKeys.SaveRestart):
		return m, m.saveCmd(envEditorSaveRestart)

	case key.Matches(msg, envKeys.SaveRebuild):
		return m, m.saveCmd(envEditorSaveRebuild)

	case key.Matches(msg, envKeys.Esc):
		return m, func() tea.Msg { return EnvEditorShowSaveMenuMsg{} }
	}

	return m, nil
}

// generateCurrent generates a new value for the selected field.
// Implements the JWT generation order trap from AGENTS.md.
func (m EnvEditorModel) generateCurrent() (EnvEditorModel, tea.Cmd) {
	f := m.currentField()
	if f == nil || !f.Generatable {
		return m, nil
	}

	allValues := m.buildAllValues()
	oldJWTSecret := allValues["JWT_SECRET"]

	val, err := config.GenerateValue(f.Key, allValues)
	if err != nil {
		m.statusMsg = "Error: " + err.Error()
		m.statusGen++
		return m, envStatusClearCmd(m.statusGen)
	}

	f.Value = val
	f.IsPlaceholder = false

	// JWT trap: if JWT_SECRET was auto-generated while producing ANON_KEY or
	// SERVICE_ROLE_KEY, propagate the new JWT_SECRET into the editor display.
	if (f.Key == "ANON_KEY" || f.Key == "SERVICE_ROLE_KEY") && allValues["JWT_SECRET"] != oldJWTSecret {
		m.setFieldValue("JWT_SECRET", allValues["JWT_SECRET"])
	}

	m.statusMsg = fmt.Sprintf("⚡ Generated %s", f.Key)
	m.statusGen++
	return m, envStatusClearCmd(m.statusGen)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m EnvEditorModel) View() string {
	if m.Width == 0 || m.Height == 0 {
		return ""
	}

	if !m.loaded {
		msg := "Loading…"
		if m.loadErr != "" {
			msg = "Error loading .env: " + m.loadErr
		}
		return lipgloss.NewStyle().Foreground(envColorMuted).Render(msg)
	}

	var sb strings.Builder

	// ── Header ──────────────────────────────────────────────────────────────
	headerLeft := lipgloss.NewStyle().Foreground(envColorBlue).Render("AgriSense") +
		lipgloss.NewStyle().Foreground(envColorMuted).Render(" ▸ ") +
		lipgloss.NewStyle().Foreground(envColorText).Render("Environment Editor")
	headerRight := ""
	if m.isDirty() {
		headerRight = lipgloss.NewStyle().Foreground(envColorOrange).Render("[MODIFIED]")
	}
	pad := m.Width - lipgloss.Width(headerLeft) - lipgloss.Width(headerRight)
	if pad < 1 {
		pad = 1
	}
	sb.WriteString(headerLeft + strings.Repeat(" ", pad) + headerRight + "\n")

	// ── Top divider ─────────────────────────────────────────────────────────
	sb.WriteString(lipgloss.NewStyle().Foreground(envColorSubtle).Render(strings.Repeat("─", m.Width)) + "\n")

	// ── Scrollable content ───────────────────────────────────────────────────
	rows := m.buildRows()
	contentH := m.contentHeight()

	// Clamp scroll for display (does not mutate stored scroll).
	scroll := m.scroll
	if scroll > len(rows)-contentH {
		scroll = len(rows) - contentH
	}
	if scroll < 0 {
		scroll = 0
	}

	end := scroll + contentH
	if end > len(rows) {
		end = len(rows)
	}
	visible := rows[scroll:end]
	for _, row := range visible {
		sb.WriteString(row + "\n")
	}
	// Pad remaining lines
	for i := len(visible); i < contentH; i++ {
		sb.WriteString("\n")
	}

	// ── Description bar ─────────────────────────────────────────────────────
	sb.WriteString(lipgloss.NewStyle().Foreground(envColorSubtle).Render(strings.Repeat("─", m.Width)) + "\n")
	descLine := m.descriptionLine()
	if m.statusMsg != "" {
		descLine = lipgloss.NewStyle().Foreground(envColorGreen).Render(m.statusMsg)
	}
	sb.WriteString(descLine + "\n")

	// ── Footer ───────────────────────────────────────────────────────────────
	sb.WriteString(lipgloss.NewStyle().Foreground(envColorSubtle).Render(strings.Repeat("─", m.Width)) + "\n")
	footer := lipgloss.NewStyle().Foreground(envColorMuted).Render(
		"[Enter] Edit  [G] Generate  [Ctrl+S] Save  [Ctrl+R] Save+Restart  [Ctrl+B] Save+Rebuild  [Esc] Menu  [↑↓] Navigate",
	)
	sb.WriteString(footer)

	return sb.String()
}

// buildRows renders all sections and their items as a flat slice of strings.
func (m EnvEditorModel) buildRows() []string {
	var rows []string
	flatIdx := 0 // counts only field items, aligns with m.cursor

	for _, sec := range m.sections {
		// Section header: ─ Name ──────────
		headerText := " " + sec.Name + " "
		ruleLen := max(0, m.Width-1-lipgloss.Width(headerText))
		header := "─" + headerText + strings.Repeat("─", ruleLen)
		rows = append(rows, lipgloss.NewStyle().Foreground(envColorYellow).Render(header))

		for _, item := range sec.Items {
			if item.IsSeparator {
				rows = append(rows, lipgloss.NewStyle().Foreground(envColorCyan).Render("    » "+item.Label))
				continue
			}
			if item.Field == nil {
				rows = append(rows, "")
				continue
			}

			f := item.Field
			isSelected := flatIdx == m.cursor

			marker := " "
			if isSelected {
				marker = "▶"
			}
			genIcon := " "
			if f.Generatable {
				genIcon = "⚡"
			}

			const labelW = 22
			label := f.Label
			if lipgloss.Width(label) < labelW {
				label = label + strings.Repeat(" ", labelW-lipgloss.Width(label))
			} else if lipgloss.Width(label) > labelW {
				label = string([]rune(label)[:labelW])
			}

			var valueStr string
			if m.editing && isSelected {
				valueStr = m.input.View()
			} else {
				valueStr = m.maskValue(f)
			}

			rowStr := fmt.Sprintf("  %s %s %s = %s", marker, genIcon, label, valueStr)

			if isSelected {
				rowStr = lipgloss.NewStyle().
					Foreground(envColorBlue).
					Background(envColorSelectBg).
					Width(m.Width).
					Render(rowStr)
			} else {
				rowStr = lipgloss.NewStyle().Foreground(envColorText).Render(rowStr)
			}

			rows = append(rows, rowStr)
			flatIdx++
		}
	}

	return rows
}

// descriptionLine returns the description bar content for the current field.
func (m EnvEditorModel) descriptionLine() string {
	f := m.currentField()
	if f == nil {
		return ""
	}
	desc := f.Key
	if f.Description != "" {
		desc = f.Key + " : " + f.Description
	}
	return lipgloss.NewStyle().Foreground(envColorMuted).Render("  ℹ " + desc)
}

// maskValue returns the display string for a field value.
// Sensitive fields show first 4 chars + **** (unless placeholder).
func (m EnvEditorModel) maskValue(f *config.EnvField) string {
	val := f.Value
	if f.IsPlaceholder {
		return lipgloss.NewStyle().Foreground(envColorYellow).Render(val)
	}
	if f.IsSensitive && val != "" {
		shown := val
		if len(shown) > 4 {
			shown = shown[:4]
		}
		return lipgloss.NewStyle().Foreground(envColorMuted).Render(shown + "****")
	}
	return lipgloss.NewStyle().Foreground(envColorText).Render(val)
}

// ---------------------------------------------------------------------------
// Model helpers
// ---------------------------------------------------------------------------

// currentField returns a pointer to the currently selected EnvField, or nil.
func (m EnvEditorModel) currentField() *config.EnvField {
	idx := 0
	for si := range m.sections {
		for ii, item := range m.sections[si].Items {
			if item.IsSeparator || item.Field == nil {
				continue
			}
			if idx == m.cursor {
				return m.sections[si].Items[ii].Field
			}
			idx++
		}
	}
	return nil
}

// setFieldValue finds a field by key and sets its value.
func (m *EnvEditorModel) setFieldValue(k, val string) {
	for si := range m.sections {
		for ii := range m.sections[si].Items {
			item := &m.sections[si].Items[ii]
			if !item.IsSeparator && item.Field != nil && item.Field.Key == k {
				item.Field.Value = val
				item.Field.IsPlaceholder = false
			}
		}
	}
}

// buildAllValues returns a flat map of all current field values.
func (m EnvEditorModel) buildAllValues() map[string]string {
	out := map[string]string{}
	for _, sec := range m.sections {
		for _, item := range sec.Items {
			if !item.IsSeparator && item.Field != nil {
				out[item.Field.Key] = item.Field.Value
			}
		}
	}
	return out
}

// fieldCount returns the total number of navigable field items.
func (m EnvEditorModel) fieldCount() int {
	count := 0
	for _, sec := range m.sections {
		for _, item := range sec.Items {
			if !item.IsSeparator && item.Field != nil {
				count++
			}
		}
	}
	return count
}

// isDirty returns true if any field value differs from the saved snapshot.
func (m EnvEditorModel) isDirty() bool {
	if len(m.sections) != len(m.saved) {
		return true
	}
	for si, sec := range m.sections {
		savedSec := m.saved[si]
		if len(sec.Items) != len(savedSec.Items) {
			return true
		}
		for ii, item := range sec.Items {
			if item.IsSeparator || item.Field == nil {
				continue
			}
			if savedSec.Items[ii].Field == nil {
				return true
			}
			if item.Field.Value != savedSec.Items[ii].Field.Value {
				return true
			}
		}
	}
	return false
}

// contentHeight returns the number of rows available for scrollable content.
// Layout: header(1) + top-divider(1) + content(??) + bottom-divider(1) + desc(1) + footer-divider(1) + footer(1) = 6 fixed rows.
func (m EnvEditorModel) contentHeight() int {
	h := m.Height - 6
	if h < 1 {
		return 1
	}
	return h
}

// ensureVisible adjusts m.scroll so the cursor row is within the visible window.
func (m *EnvEditorModel) ensureVisible() {
	rowIdx := m.cursorRowIndex()
	if rowIdx < 0 {
		return
	}
	contentH := m.contentHeight()
	if rowIdx < m.scroll {
		m.scroll = rowIdx
	}
	if rowIdx >= m.scroll+contentH {
		m.scroll = rowIdx - contentH + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// cursorRowIndex returns the row index (in buildRows output) for the current cursor.
func (m EnvEditorModel) cursorRowIndex() int {
	rowIdx := 0
	flatIdx := 0
	for _, sec := range m.sections {
		rowIdx++ // section header
		for _, item := range sec.Items {
			if item.IsSeparator || item.Field == nil {
				rowIdx++
				continue
			}
			if flatIdx == m.cursor {
				return rowIdx
			}
			rowIdx++
			flatIdx++
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m EnvEditorModel) loadCmd() tea.Cmd {
	envPath := m.EnvPath
	exPath := m.ExamplePath
	return func() tea.Msg {
		sections, err := config.ParseEnvWithExample(envPath, exPath)
		return envLoadMsg{sections: sections, err: err}
	}
}

// SaveOnly returns a tea.Cmd that writes the .env file (save-only mode).
// Called by app.go for the "Save & Exit" popup choice.
func (m EnvEditorModel) SaveOnly() tea.Cmd { return m.saveCmd(envEditorSaveOnly) }

// SaveRestartCmd returns a tea.Cmd that writes .env and emits EnvEditorSaveRestartMsg.
func (m EnvEditorModel) SaveRestartCmd() tea.Cmd { return m.saveCmd(envEditorSaveRestart) }

// SaveRebuildCmd returns a tea.Cmd that writes .env and emits EnvEditorSaveRebuildMsg.
func (m EnvEditorModel) SaveRebuildCmd() tea.Cmd { return m.saveCmd(envEditorSaveRebuild) }

func (m EnvEditorModel) saveCmd(mode envEditorSaveMode) tea.Cmd {
	envPath := m.EnvPath
	exPath := m.ExamplePath
	sections := m.sections
	return func() tea.Msg {
		err := config.WriteEnv(envPath, exPath, sections)
		return envWriteMsg{mode: mode, err: err}
	}
}

func envStatusClearCmd(gen int) tea.Cmd {
	return tea.Tick(600*time.Millisecond, func(_ time.Time) tea.Msg {
		return envStatusClearMsg{gen: gen}
	})
}

// ---------------------------------------------------------------------------
// Deep copy helpers
// ---------------------------------------------------------------------------

func deepCopySections(sections []config.EnvSection) []config.EnvSection {
	out := make([]config.EnvSection, len(sections))
	for i, sec := range sections {
		out[i] = config.EnvSection{
			Name:  sec.Name,
			Items: make([]config.EnvItem, len(sec.Items)),
		}
		for j, item := range sec.Items {
			if item.Field != nil {
				f := *item.Field
				out[i].Items[j] = config.EnvItem{
					IsSeparator: item.IsSeparator,
					Label:       item.Label,
					Field:       &f,
				}
			} else {
				out[i].Items[j] = item
			}
		}
	}
	return out
}
