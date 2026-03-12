package views

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Inline color values matching internal/tui/styles.go — avoids import cycle.
var (
	logsColorText    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	logsColorMuted   = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	logsColorSubtle  = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
	logsColorGreen   = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	logsColorYellow  = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	logsColorRed     = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	logsColorBlue    = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	logsColorMagenta = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#bc8cff"}
)

// LogsGoBackMsg signals the root model to return to dashboard.
type LogsGoBackMsg struct{}

// LogsModel is the full-screen log viewer.
type LogsModel struct {
	Width  int
	Height int

	containerName string
	cancelLog     context.CancelFunc

	autoTail bool
	lines    []string

	viewport viewport.Model
}

func logsVPHeight(total int) int {
	// header(1) + separator(1) + footer(1) = 3 rows
	h := total - 3
	if h < 1 {
		h = 1
	}
	return h
}

// SetContainer is called (by app.go or dashboard) before navigating to this
// view. It stores the container name and cancels any previous log stream.
func (m *LogsModel) SetContainer(name string) {
	if m.cancelLog != nil {
		m.cancelLog()
		m.cancelLog = nil
	}
	m.containerName = name
	m.lines = nil
	m.autoTail = true
	m.viewport = viewport.New(m.Width, logsVPHeight(m.Height))
}

// SetCancelFunc stores the cancel function for the active stream so that
// SetContainer can cancel it when the selection changes.
func (m *LogsModel) SetCancelFunc(cancel context.CancelFunc) {
	m.cancelLog = cancel
}

// StartStreaming begins a docker logs stream for the current container.
// The caller provides a stream function to avoid an import cycle with runner.
// streamFn must return a channel of log lines and a cancel func.
func (m *LogsModel) StartStreaming(ctx context.Context, linesCh <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-linesCh
		if !ok {
			return nil
		}
		return logsLineMsg{line: line, ch: linesCh}
	}
}

// logsLineMsg is an internal message carrying one streamed line plus the
// channel so the model can schedule the next read.
type logsLineMsg struct {
	line string
	ch   <-chan string
}

func (m LogsModel) Init() tea.Cmd { return nil }

func (m LogsModel) Update(msg tea.Msg) (LogsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = logsVPHeight(msg.Height)
		return m, nil

	case logsLineMsg:
		// Append colored line then schedule next read from the same channel.
		m.lines = append(m.lines, logsColorLine(msg.line))
		m.rebuildViewport()
		if m.autoTail {
			m.viewport.GotoBottom()
		}
		next := func() tea.Msg {
			line, ok := <-msg.ch
			if !ok {
				return nil
			}
			return logsLineMsg{line: line, ch: msg.ch}
		}
		return m, next

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m LogsModel) handleKey(msg tea.KeyMsg) (LogsModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "f", "F":
		if m.cancelLog != nil {
			m.cancelLog()
			m.cancelLog = nil
		}
		return m, func() tea.Msg { return LogsGoBackMsg{} }
	case "end":
		m.autoTail = true
		m.viewport.GotoBottom()
	case "up", "down", "pgup", "pgdown", "home":
		m.autoTail = false
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *LogsModel) rebuildViewport() {
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
}

func (m LogsModel) View() string {
	if m.Width == 0 {
		return ""
	}
	header := m.renderHeader()
	sep := lipgloss.NewStyle().Foreground(logsColorSubtle).Render(strings.Repeat("─", m.Width))
	footer := lipgloss.NewStyle().Foreground(logsColorMuted).Render("[Esc/F] Back  [↑↓] Scroll  [End] Tail")
	return strings.Join([]string{header, sep, m.viewport.View(), footer}, "\n")
}

func (m LogsModel) renderHeader() string {
	appName := lipgloss.NewStyle().Foreground(logsColorBlue).Bold(true).Render("AgriSense")
	arrow := lipgloss.NewStyle().Foreground(logsColorMuted).Render(" ▸ ")
	label := lipgloss.NewStyle().Foreground(logsColorMuted).Render("Logs: ")
	name := lipgloss.NewStyle().Foreground(logsColorMagenta).Render(m.containerName)
	left := appName + arrow + label + name

	var badge string
	if m.autoTail {
		badge = lipgloss.NewStyle().Foreground(logsColorGreen).Render("[AUTO-TAIL]")
	} else {
		badge = lipgloss.NewStyle().Foreground(logsColorMuted).Render("[scrolled]")
	}

	leftW := lipgloss.Width(left)
	badgeW := lipgloss.Width(badge)
	gap := m.Width - leftW - badgeW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + badge
}

// logsColorLine applies color based on log line content.
func logsColorLine(line string) string {
	lower := strings.ToLower(line)
	var color lipgloss.TerminalColor
	switch {
	case strings.Contains(lower, "error") ||
		strings.Contains(lower, "err") ||
		strings.Contains(lower, "erro") ||
		strings.Contains(lower, "fatal"):
		color = logsColorRed
	case strings.Contains(lower, "warn"):
		color = logsColorYellow
	case strings.Contains(lower, "info"):
		color = logsColorMuted
	default:
		color = logsColorText
	}
	return lipgloss.NewStyle().Foreground(color).Render(line)
}
