package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agrisense/internal/runner"
)

// Inline color values matching internal/tui/styles.go — avoids import cycle.
var (
	dashColorText     = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	dashColorMuted    = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	dashColorSubtle   = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
	dashColorGreen    = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	dashColorYellow   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	dashColorRed      = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	dashColorBlue     = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	dashColorCyan     = lipgloss.AdaptiveColor{Light: "#0598bc", Dark: "#39c5cf"}
	dashColorSelectBg = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1d3a5e"}
)

// ---------------------------------------------------------------------------
// Focus
// ---------------------------------------------------------------------------

type dashFocus int

const (
	dashFocusLeft  dashFocus = iota
	dashFocusRight
)

// ---------------------------------------------------------------------------
// Internal messages
// ---------------------------------------------------------------------------

type dashLogLineMsg struct {
	line string
	ch   <-chan string
	gen  int
}

type dashTickMsg struct{ gen int }

type dashServicesMsg struct {
	services []runner.ServiceStatus
	err      error
}

// DashboardActivatedMsg is sent by app.go when the dashboard becomes the
// active view (NavigateTo or GoBack). It (re)starts the tick chain and log
// stream so that the dashboard does not go stale after a view switch.
type DashboardActivatedMsg struct{}

// ---------------------------------------------------------------------------
// Popup-trigger messages emitted by the dashboard, handled by app.go
// ---------------------------------------------------------------------------

// DashShowContainerMenuMsg asks app.go to show the per-container action popup.
type DashShowContainerMenuMsg struct{ Service runner.ServiceStatus }

// DashShowStackStartMsg asks app.go to show the start-all confirmation popup.
type DashShowStackStartMsg struct{}

// DashShowStackStopMsg asks app.go to show the stop-all confirmation popup.
type DashShowStackStopMsg struct{}

// DashShowStackRestartMsg asks app.go to show the restart-all confirmation popup.
type DashShowStackRestartMsg struct{}

// DashShowStackBuildMsg asks app.go to show the build-and-start confirmation popup.
type DashShowStackBuildMsg struct{}

// DashShowDestroyMsg asks app.go to show the destroy-mode picker popup.
type DashShowDestroyMsg struct{}

// DashShowTestingMenuMsg asks app.go to show the testing/simulation popup.
type DashShowTestingMenuMsg struct{}

// DashShowHelpMsg asks app.go to show the keyboard-shortcut help popup.
type DashShowHelpMsg struct{}

// DashOpenEnvEditorMsg asks app.go to navigate to the env editor.
type DashOpenEnvEditorMsg struct{}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// DashboardModel is the main split-pane bento dashboard.
type DashboardModel struct {
	Width  int
	Height int

	focus         dashFocus
	services      []runner.ServiceStatus
	selectedIndex int

	// Inline log stream
	logViewport  viewport.Model
	logLines     []string
	logCancel    context.CancelFunc
	autoTail     bool
	logContainer string // name of container currently being logged
	logGen       int    // generation counter to discard stale log lines

	// Runner (set by app.go before first use)
	Runner runner.Runner

	// Tick generation — prevents stale tick chains after view switches.
	tickGen int

	ready bool
}

func (m DashboardModel) Init() tea.Cmd { return nil }

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	var cmds []tea.Cmd

	// First-time initialisation.
	if !m.ready {
		m.ready = true
		m.autoTail = true
		m.logViewport = viewport.New(0, 0)
		m.resizeViewport()
		m.tickGen++
		cmds = append(cmds, m.tickCmd(), m.refreshServicesCmd())
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.resizeViewport()
		return m, tea.Batch(cmds...)

	case DashboardActivatedMsg:
		// (Re)start tick chain and log stream after a view switch.
		m.tickGen++
		cmds = append(cmds, m.tickCmd(), m.refreshServicesCmd())
		if len(m.services) > 0 {
			cmds = append(cmds, m.startLogStream())
		}
		return m, tea.Batch(cmds...)

	case dashTickMsg:
		if msg.gen != m.tickGen {
			return m, tea.Batch(cmds...) // stale tick, ignore
		}
		cmds = append(cmds, m.tickCmd(), m.refreshServicesCmd())
		return m, tea.Batch(cmds...)

	case dashServicesMsg:
		if msg.err == nil {
			oldName := m.selectedServiceName()
			m.services = msg.services
			// Keep selection on the same service if possible.
			if oldName != "" {
				for i, s := range m.services {
					if s.Name == oldName {
						m.selectedIndex = i
						break
					}
				}
			}
			if m.selectedIndex >= len(m.services) {
				m.selectedIndex = max(0, len(m.services)-1)
			}
			// Start log stream if we don't have one yet.
			if m.logContainer == "" && len(m.services) > 0 {
				cmds = append(cmds, m.startLogStream())
			}
		}
		return m, tea.Batch(cmds...)

	case dashLogLineMsg:
		if msg.gen != m.logGen {
			return m, tea.Batch(cmds...) // stale log line, ignore
		}
		m.logLines = append(m.logLines, dashColorLine(msg.line))
		m.rebuildLogViewport()
		if m.autoTail {
			m.logViewport.GotoBottom()
		}
		gen := m.logGen
		next := func() tea.Msg {
			line, ok := <-msg.ch
			if !ok {
				return nil
			}
			return dashLogLineMsg{line: line, ch: msg.ch, gen: gen}
		}
		cmds = append(cmds, next)
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		updated, cmd := m.handleKey(msg)
		cmds = append(cmds, cmd)
		return updated, tea.Batch(cmds...)
	}

	// Pass remaining messages (e.g. viewport scroll) when right panel focused.
	if m.focus == dashFocusRight {
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (m DashboardModel) handleKey(msg tea.KeyMsg) (DashboardModel, tea.Cmd) {
	if m.focus == dashFocusRight {
		return m.handleLogKey(msg)
	}
	return m.handleSidebarKey(msg)
}

func (m DashboardModel) handleSidebarKey(msg tea.KeyMsg) (DashboardModel, tea.Cmd) {
	switch msg.String() {
	case "up":
		if len(m.services) > 0 {
			m.selectedIndex--
			if m.selectedIndex < 0 {
				m.selectedIndex = len(m.services) - 1
			}
			return m, m.startLogStream()
		}
	case "down":
		if len(m.services) > 0 {
			m.selectedIndex++
			if m.selectedIndex >= len(m.services) {
				m.selectedIndex = 0
			}
			return m, m.startLogStream()
		}
	case "tab":
		m.focus = dashFocusRight
	case "enter":
		if svc := m.selectedService(); svc != nil {
			return m, func() tea.Msg { return DashShowContainerMenuMsg{Service: *svc} }
		}
	case "s", "S":
		return m, func() tea.Msg { return DashShowStackStartMsg{} }
	case "r", "R":
		return m, func() tea.Msg { return DashShowStackRestartMsg{} }
	case "b", "B":
		return m, func() tea.Msg { return DashShowStackBuildMsg{} }
	case "x", "X":
		return m, func() tea.Msg { return DashShowStackStopMsg{} }
	case "d", "D":
		return m, func() tea.Msg { return DashShowDestroyMsg{} }
	case "t", "T":
		return m, func() tea.Msg { return DashShowTestingMenuMsg{} }
	case "e", "E":
		return m, func() tea.Msg { return DashOpenEnvEditorMsg{} }
	case "?":
		return m, func() tea.Msg { return DashShowHelpMsg{} }
	case "q", "Q":
		return m, tea.Quit
	}
	return m, nil
}

func (m DashboardModel) handleLogKey(msg tea.KeyMsg) (DashboardModel, tea.Cmd) {
	switch msg.String() {
	case "tab", "esc":
		m.focus = dashFocusLeft
	case "end":
		m.autoTail = true
		m.logViewport.GotoBottom()
	case "up", "down", "pgup", "pgdown", "home":
		m.autoTail = false
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		return m, cmd
	case "f", "F":
		if m.logContainer != "" {
			name := m.logContainer
			var svc runner.ServiceStatus
			for _, s := range m.services {
				if s.Name == name {
					svc = s
					break
				}
			}
			return m, func() tea.Msg { return DashShowContainerMenuMsg{Service: svc} }
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m DashboardModel) tickCmd() tea.Cmd {
	gen := m.tickGen
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return dashTickMsg{gen: gen}
	})
}

func (m DashboardModel) refreshServicesCmd() tea.Cmd {
	if m.Runner == nil {
		return nil
	}
	r := m.Runner
	return func() tea.Msg {
		svcs, err := r.ListServices(context.Background())
		return dashServicesMsg{services: svcs, err: err}
	}
}

// startLogStream cancels any previous log process (AGENTS.md: log-process
// leak trap) and starts a new docker-logs stream for the selected service.
func (m *DashboardModel) startLogStream() tea.Cmd {
	if m.Runner == nil || len(m.services) == 0 {
		return nil
	}

	// Cancel previous stream.
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}

	svc := m.services[m.selectedIndex]
	m.logContainer = svc.Name
	m.logLines = nil
	m.autoTail = true
	m.logViewport.SetContent("")
	m.logGen++

	// Show notice for stopped/errored containers.
	if svc.State == "stopped" || svc.State == "error" {
		notice := lipgloss.NewStyle().Foreground(dashColorYellow).
			Render("Container is not running — showing last logs")
		m.logLines = append(m.logLines, notice, "")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.logCancel = cancel
	r := m.Runner
	name := svc.Name
	gen := m.logGen

	return func() tea.Msg {
		ch, err := r.StreamLogs(ctx, name, 100)
		if err != nil {
			return nil
		}
		line, ok := <-ch
		if !ok {
			return nil
		}
		return dashLogLineMsg{line: line, ch: ch, gen: gen}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (m DashboardModel) selectedServiceName() string {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.services) {
		return m.services[m.selectedIndex].Name
	}
	return ""
}

func (m DashboardModel) selectedService() *runner.ServiceStatus {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.services) {
		return &m.services[m.selectedIndex]
	}
	return nil
}

func (m DashboardModel) sidebarWidth() int {
	w := int(float64(m.Width) * 0.30)
	if w > 34 {
		w = 34
	}
	if w < 26 {
		w = 26
	}
	return w
}

func (m *DashboardModel) resizeViewport() {
	vpW, vpH := m.logViewportDims()
	m.logViewport.Width = vpW
	m.logViewport.Height = vpH
}

func (m DashboardModel) logViewportDims() (int, int) {
	panelH := m.Height - 3 // header + 2-row footer

	if m.Width < 80 {
		// Narrow mode: full-width log panel.
		vpW := m.Width - 2 // borders
		vpH := panelH - 2  // borders
		return max(1, vpW), max(1, vpH)
	}

	sw := m.sidebarWidth()
	rightW := m.Width - sw - 1 // 1-char gap
	innerW := rightW - 2       // borders

	var logPanelH int
	if m.Width < 100 {
		logPanelH = panelH
	} else {
		logPanelH = panelH - 6 // minus details panel (4 content + 2 border)
	}
	vpH := logPanelH - 2 // borders

	return max(1, innerW), max(1, vpH)
}

func (m *DashboardModel) rebuildLogViewport() {
	m.logViewport.SetContent(strings.Join(m.logLines, "\n"))
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m DashboardModel) View() string {
	if m.Width == 0 || m.Height == 0 {
		return ""
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	panelH := m.Height - 3 // header + 2-row footer

	if m.Width < 80 {
		logPanel := m.renderLogPanel(m.Width, panelH)
		return header + "\n" + logPanel + "\n" + footer
	}

	sidebar := m.renderSidebar(panelH)
	sidebarW := m.sidebarWidth()
	rightW := m.Width - sidebarW - 1 // 1 space gap

	var rightPanel string
	if m.Width < 100 {
		rightPanel = m.renderLogPanel(rightW, panelH)
	} else {
		detailsH := 6
		logH := panelH - detailsH
		rightPanel = m.renderLogPanel(rightW, logH) + "\n" + m.renderDetailsPanel(rightW, detailsH)
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", rightPanel)
	return header + "\n" + body + "\n" + footer
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (m DashboardModel) renderHeader() string {
	appName := lipgloss.NewStyle().Foreground(dashColorBlue).Bold(true).Render("AgriSense")

	running := 0
	for _, s := range m.services {
		if s.State == "running" {
			running++
		}
	}
	total := len(m.services)
	now := time.Now().Format("15:04:05")

	var dot string
	switch {
	case total == 0:
		dot = lipgloss.NewStyle().Foreground(dashColorSubtle).Render("●")
	case running == total:
		dot = lipgloss.NewStyle().Foreground(dashColorGreen).Render("●")
	case running > 0:
		dot = lipgloss.NewStyle().Foreground(dashColorYellow).Render("●")
	default:
		dot = lipgloss.NewStyle().Foreground(dashColorRed).Render("●")
	}

	right := fmt.Sprintf("%s %d/%d running  %s", dot, running, total, now)
	rightW := lipgloss.Width(right)

	left := " " + appName + " "
	leftW := lipgloss.Width(left)

	fillW := m.Width - leftW - rightW
	if fillW < 1 {
		fillW = 1
	}
	fill := lipgloss.NewStyle().Foreground(dashColorMuted).Render(strings.Repeat("─", fillW))

	return left + fill + right
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m DashboardModel) renderFooter() string {
	muted := func(s string) string {
		return lipgloss.NewStyle().Foreground(dashColorMuted).Render(s)
	}
	keyHint := func(k string, kColor lipgloss.TerminalColor, label string) string {
		bracket := lipgloss.NewStyle().Foreground(dashColorSubtle).Render
		keyStr := lipgloss.NewStyle().Foreground(kColor).Bold(true).Render(strings.ToUpper(k))
		labelStr := lipgloss.NewStyle().Foreground(dashColorMuted).Render(label)
		return bracket("[") + keyStr + bracket("]") + " " + labelStr
	}

	if m.focus == dashFocusLeft {
		row1 := strings.Join([]string{
			keyHint("↑↓", dashColorMuted, "Navigate"),
			muted("·"),
			keyHint("Enter", dashColorMuted, "Actions"),
			muted("·"),
			keyHint("Tab", dashColorMuted, "Logs"),
		}, "  ")

		row2 := strings.Join([]string{
			keyHint("S", dashColorGreen, "Start"),
			keyHint("R", dashColorYellow, "Restart"),
			keyHint("B", dashColorCyan, "Build"),
			keyHint("X", dashColorRed, "Stop"),
			keyHint("D", dashColorRed, "Destroy"),
			muted("·"),
			keyHint("E", dashColorBlue, ".env"),
			keyHint("T", dashColorCyan, "Test"),
			keyHint("Q", dashColorMuted, "Quit"),
		}, "  ")

		return row1 + "\n" + row2
	}

	row1 := strings.Join([]string{
		keyHint("↑↓", dashColorMuted, "Scroll logs"),
		muted("·"),
		keyHint("Tab", dashColorMuted, "Sidebar"),
		muted("·"),
		keyHint("End", dashColorMuted, "Auto-tail"),
	}, "  ")
	return row1 + "\n"
}

// ---------------------------------------------------------------------------
// Sidebar
// ---------------------------------------------------------------------------

func (m DashboardModel) renderSidebar(height int) string {
	w := m.sidebarWidth()
	innerW := w - 2
	contentH := height - 2

	var rows []string
	sawSupabase := false

	for i, svc := range m.services {
		if !sawSupabase && strings.HasPrefix(svc.Name, "supabase") {
			sawSupabase = true
			sepText := "── supabase ──"
			padTotal := innerW - lipgloss.Width(sepText)
			if padTotal < 0 {
				padTotal = 0
			}
			lp := padTotal / 2
			rp := padTotal - lp
			sep := lipgloss.NewStyle().Foreground(dashColorSubtle).
				Render(strings.Repeat(" ", lp) + sepText + strings.Repeat(" ", rp))
			rows = append(rows, sep)
		}
		rows = append(rows, m.renderServiceRow(i, svc, innerW))
	}

	for len(rows) < contentH {
		rows = append(rows, strings.Repeat(" ", innerW))
	}
	if len(rows) > contentH {
		rows = rows[:contentH]
	}

	content := strings.Join(rows, "\n")
	return dashBorderedPanel("Containers", "", content, w, height, m.focus == dashFocusLeft)
}

func (m DashboardModel) renderServiceRow(index int, svc runner.ServiceStatus, width int) string {
	selected := index == m.selectedIndex
	glyph, glyphColor := dashStateGlyph(svc.State)

	name := dashShortName(svc.Name)
	marker := ""
	if selected {
		marker = "[▶]"
	}

	// Layout: glyph(1) + space(1) + name + gap + marker
	fixedW := 2 + len(marker) // glyph + space + marker
	maxNameW := width - fixedW
	if maxNameW < 0 {
		maxNameW = 0
	}
	if len(name) > maxNameW {
		name = name[:maxNameW]
	}
	gap := width - 2 - len(name) - len(marker)
	if gap < 0 {
		gap = 0
	}

	if selected {
		bg := lipgloss.NewStyle().Background(dashColorSelectBg)
		glyphStr := bg.Foreground(glyphColor).Render(glyph)
		sp := bg.Render(" ")
		nameStr := bg.Foreground(dashColorBlue).Render(name)
		gapStr := bg.Render(strings.Repeat(" ", gap))
		markerStr := bg.Foreground(dashColorBlue).Render(marker)
		return glyphStr + sp + nameStr + gapStr + markerStr
	}

	glyphStr := lipgloss.NewStyle().Foreground(glyphColor).Render(glyph)
	nameStr := lipgloss.NewStyle().Foreground(dashColorText).Render(name)
	return glyphStr + " " + nameStr + strings.Repeat(" ", gap+len(marker))
}

// ---------------------------------------------------------------------------
// Log panel (right, top)
// ---------------------------------------------------------------------------

func (m DashboardModel) renderLogPanel(width, height int) string {
	name := m.logContainer
	if name == "" {
		name = "logs"
	}

	var rightTitle string
	if m.autoTail {
		rightTitle = "AUTO"
	} else {
		total := len(m.logLines)
		pos := m.logViewport.YOffset + m.logViewport.Height
		if pos > total {
			pos = total
		}
		rightTitle = fmt.Sprintf("line %d/%d", pos, total)
	}

	content := m.logViewport.View()
	return dashBorderedPanel(name, rightTitle, content, width, height, m.focus == dashFocusRight)
}

// ---------------------------------------------------------------------------
// Details panel (right, bottom strip — 6 rows: 4 content + 2 border)
// ---------------------------------------------------------------------------

func (m DashboardModel) renderDetailsPanel(width, height int) string {
	innerW := width - 2
	svc := m.selectedService()

	var lines []string
	if svc != nil {
		// Row 1: State
		glyph, gc := dashStateGlyph(svc.State)
		glyphStr := lipgloss.NewStyle().Foreground(gc).Render(glyph)
		stateLabel := lipgloss.NewStyle().Foreground(gc).Render(svc.State)
		statusStr := lipgloss.NewStyle().Foreground(dashColorMuted).Render("  " + svc.Status)
		lines = append(lines, " State  "+glyphStr+" "+stateLabel+statusStr)

		// Row 2: Ports (derived from service URL)
		url := ""
		if m.Runner != nil {
			url = m.Runner.ServiceURL(svc.Name)
		}
		if url != "" {
			port := dashExtractPort(url)
			if port != "" {
				lines = append(lines, " Ports  "+lipgloss.NewStyle().Foreground(dashColorText).Render(port+" → "+port))
			}
		}

		// Row 3: Image
		if svc.Service != "" {
			lines = append(lines, " Image  "+lipgloss.NewStyle().Foreground(dashColorText).Render(svc.Service+":latest"))
		}

		// Row 4: URL link (only for backend, frontend, studio)
		if url != "" {
			lines = append(lines, " "+lipgloss.NewStyle().Foreground(dashColorCyan).Render("🌐  "+url))
		}
	}

	contentH := height - 2
	for len(lines) < contentH {
		lines = append(lines, "")
	}
	if len(lines) > contentH {
		lines = lines[:contentH]
	}

	// Pad each line to inner width.
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w < innerW {
			lines[i] = line + strings.Repeat(" ", innerW-w)
		}
	}

	content := strings.Join(lines, "\n")
	return dashBorderedPanel("Details", "", content, width, height, false)
}

// ---------------------------------------------------------------------------
// Bordered panel renderer
// ---------------------------------------------------------------------------

func dashBorderedPanel(title, rightTitle, content string, width, height int, focused bool) string {
	bc := dashColorSubtle
	if focused {
		bc = dashColorBlue
	}
	bStyle := lipgloss.NewStyle().Foreground(bc)

	innerW := width - 2
	if innerW < 0 {
		innerW = 0
	}
	contentH := height - 2
	if contentH < 0 {
		contentH = 0
	}

	// Top border with title(s).
	titlePart := "─ " + title + " "
	rightPart := ""
	if rightTitle != "" {
		rightPart = " " + rightTitle + " ─"
	}
	fillLen := innerW - lipgloss.Width(titlePart) - lipgloss.Width(rightPart)
	if fillLen < 0 {
		fillLen = 0
	}
	top := bStyle.Render("╭" + titlePart + strings.Repeat("─", fillLen) + rightPart + "╮")

	// Bottom border.
	bottom := bStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	// Content lines — pad or truncate to fill the panel.
	lines := strings.Split(content, "\n")
	var body []string
	for i := 0; i < contentH; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		lineW := lipgloss.Width(line)
		if lineW < innerW {
			line += strings.Repeat(" ", innerW-lineW)
		}
		body = append(body, bStyle.Render("│")+line+bStyle.Render("│"))
	}

	result := []string{top}
	result = append(result, body...)
	result = append(result, bottom)
	return strings.Join(result, "\n")
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

func dashStateGlyph(state string) (string, lipgloss.AdaptiveColor) {
	switch state {
	case "running":
		return "●", dashColorGreen
	case "restarting":
		return "◌", dashColorYellow
	case "stopped", "exited", "error":
		return "✗", dashColorRed
	default:
		return "·", dashColorSubtle
	}
}

func dashShortName(name string) string {
	return strings.TrimPrefix(name, "agrisense-")
}

func dashColorLine(line string) string {
	lower := strings.ToLower(line)
	var color lipgloss.TerminalColor
	switch {
	case strings.Contains(lower, "error") ||
		strings.Contains(lower, "err") ||
		strings.Contains(lower, "erro") ||
		strings.Contains(lower, "fatal"):
		color = dashColorRed
	case strings.Contains(lower, "warn"):
		color = dashColorYellow
	case strings.Contains(lower, "info"):
		color = dashColorMuted
	default:
		color = dashColorText
	}
	return lipgloss.NewStyle().Foreground(color).Render(line)
}

func dashExtractPort(url string) string {
	idx := strings.LastIndex(url, ":")
	if idx < 0 {
		return ""
	}
	port := url[idx+1:]
	if slashIdx := strings.Index(port, "/"); slashIdx >= 0 {
		port = port[:slashIdx]
	}
	return port
}
