package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Inline color values matching internal/tui/styles.go — avoids import cycle.
var (
	actionColorText   = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	actionColorMuted  = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	actionColorSubtle = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
	actionColorGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	actionColorRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	actionColorBlue   = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	actionColorCyan   = lipgloss.AdaptiveColor{Light: "#0598bc", Dark: "#39c5cf"}
)

// ActionModel is the streaming command output view.
type ActionModel struct {
	Width  int
	Height int

	// Command metadata
	Title  string
	cancel context.CancelFunc

	// State
	running  bool
	exitCode int
	autoTail bool

	// Output buffer
	regularLines    []string // non-pull output
	pulledLines     []string // "Image X Pulled" lines
	pullTotal       int
	pullDone        int
	pullCurrentItem string // image currently being pulled

	viewport viewport.Model
	spinner  spinner.Model
}

// NewActionModel constructs a ready-to-use ActionModel.
func NewActionModel(title string, cancel context.CancelFunc, width, height int) ActionModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(actionColorBlue)

	vp := viewport.New(width, actionVPHeight(height))

	return ActionModel{
		Width:        width,
		Height:       height,
		Title:        title,
		cancel:       cancel,
		running:      true,
		autoTail:     true,
		viewport:     vp,
		spinner:      sp,
	}
}

func actionVPHeight(total int) int {
	// header(1) + separator(1) + footer(1) = 3 fixed rows
	h := total - 3
	if h < 1 {
		h = 1
	}
	return h
}

// AppendLine adds a streaming output line. Called by app.go on ActionLineReceived.
func (m *ActionModel) AppendLine(line string) {
	if actionIsPullLine(line) {
		m.updatePull(line)
	} else {
		m.regularLines = append(m.regularLines, line)
	}
	m.rebuildViewport()
	if m.autoTail {
		m.viewport.GotoBottom()
	}
}

// SetDone marks the command as finished. Called by app.go on ActionCompleted.
func (m *ActionModel) SetDone(exitCode int) {
	m.running = false
	m.exitCode = exitCode
	m.regularLines = append(m.regularLines, "")
	if exitCode == 0 {
		m.regularLines = append(m.regularLines, lipgloss.NewStyle().Foreground(actionColorGreen).Render("[OK] Done."))
	} else {
		m.regularLines = append(m.regularLines, lipgloss.NewStyle().Foreground(actionColorRed).
			Render(fmt.Sprintf("[ERROR] Process exited with code %d.", exitCode)))
	}
	m.rebuildViewport()
	m.viewport.GotoBottom()
}

func actionIsPullLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.Contains(lower, "pulling fs layer") ||
		strings.Contains(lower, "already exists") ||
		strings.Contains(lower, "downloading") ||
		strings.Contains(lower, "download complete") ||
		strings.Contains(lower, "extracting") ||
		strings.Contains(lower, "pull complete") ||
		strings.Contains(lower, "waiting") ||
		strings.Contains(lower, "verifying checksum") ||
		(strings.HasPrefix(lower, "image ") && (strings.Contains(lower, " pulling") || strings.Contains(lower, " pulled")))
}

// actionExtractImageName parses "Image foo/bar:tag Pulling" → "foo/bar:tag".
func actionExtractImageName(line string) string {
	// Format: "Image <name> Pulling" or "Image <name> Pulled"
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(strings.ToLower(trimmed), "image ") {
		return ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (m *ActionModel) updatePull(line string) {
	lower := strings.ToLower(strings.TrimSpace(line))

	if strings.HasPrefix(lower, "image ") {
		name := actionExtractImageName(line)
		if strings.Contains(lower, " pulling") && name != "" {
			m.pullCurrentItem = name
		} else if strings.Contains(lower, " pulled") {
			if name != "" {
				pulled := lipgloss.NewStyle().Foreground(actionColorGreen).Render("✓ ") +
					lipgloss.NewStyle().Foreground(actionColorMuted).Render(name)
				m.pulledLines = append(m.pulledLines, pulled)
			}
			m.pullCurrentItem = ""
		}
		return
	}

	// Count total layers (each layer announces itself once).
	if strings.Contains(lower, "pulling fs layer") || strings.Contains(lower, "already exists") {
		m.pullTotal++
	}
	// Count completed layers.
	if strings.Contains(lower, "pull complete") || strings.Contains(lower, "already exists") {
		m.pullDone++
	}
}

func actionRenderPullBar(done, total int) string {
	if total == 0 {
		return ""
	}
	width := 32
	filled := 0
	if total > 0 {
		filled = (done * width) / total
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("  %s  %d/%d layers",
		lipgloss.NewStyle().Foreground(actionColorCyan).Render(bar),
		done, total)
}

func (m *ActionModel) rebuildViewport() {
	all := make([]string, 0, len(m.regularLines)+len(m.pulledLines)+4)
	all = append(all, m.regularLines...)

	if m.pullTotal > 0 || len(m.pulledLines) > 0 {
		// Pulled images (above bar).
		all = append(all, m.pulledLines...)
		// Blank separator.
		all = append(all, "")
		// Progress bar.
		all = append(all, actionRenderPullBar(m.pullDone, m.pullTotal))
		// Current image being pulled (below bar, replaced each time).
		if m.pullCurrentItem != "" {
			all = append(all, lipgloss.NewStyle().Foreground(actionColorMuted).Render("  "+m.pullCurrentItem))
		} else {
			all = append(all, "")
		}
	}

	m.viewport.SetContent(strings.Join(all, "\n"))
}

func (m ActionModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m ActionModel) Update(msg tea.Msg) (ActionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = actionVPHeight(msg.Height)
		m.rebuildViewport()
		return m, nil

	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Pass remaining msgs (scroll) to viewport.
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m ActionModel) handleKey(msg tea.KeyMsg) (ActionModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if !m.running {
			return m, actionGoBackCmd()
		}
	case "s", "S":
		if m.running && m.cancel != nil {
			m.cancel()
		}
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

// ActionGoBackMsg is exported so app.go can match it as a GoBack signal.
type ActionGoBackMsg struct{}

func actionGoBackCmd() tea.Cmd {
	return func() tea.Msg { return ActionGoBackMsg{} }
}

func (m ActionModel) View() string {
	if m.Width == 0 {
		return ""
	}
	header := m.renderHeader()
	sep := lipgloss.NewStyle().Foreground(actionColorSubtle).Render(strings.Repeat("─", m.Width))
	footer := m.renderFooter()
	return strings.Join([]string{header, sep, m.viewport.View(), footer}, "\n")
}

func (m ActionModel) renderHeader() string {
	appName := lipgloss.NewStyle().Foreground(actionColorBlue).Bold(true).Render("AgriSense")
	arrow := lipgloss.NewStyle().Foreground(actionColorMuted).Render(" ▸ ")
	title := lipgloss.NewStyle().Foreground(actionColorText).Render(m.Title)
	left := appName + arrow + title

	var right string
	if m.running {
		right = m.spinner.View() + " " + lipgloss.NewStyle().Foreground(actionColorMuted).Render("Running")
	} else if m.exitCode == 0 {
		right = lipgloss.NewStyle().Foreground(actionColorGreen).Render("✓ Done")
	} else {
		right = lipgloss.NewStyle().Foreground(actionColorRed).Render(fmt.Sprintf("✗ Exit %d", m.exitCode))
	}

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.Width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m ActionModel) renderFooter() string {
	var hint string
	if m.running {
		hint = "[S] Stop  [↑↓] Scroll"
	} else {
		hint = "[Esc] Return  [↑↓] Scroll  [End] Tail"
	}
	return lipgloss.NewStyle().Foreground(actionColorMuted).Render(hint)
}
