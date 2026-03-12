package tui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agrisense/internal/config"
	"agrisense/internal/runner"
	"agrisense/internal/tui/views"
)

// ViewID identifies which view is currently active.
type ViewID int

const (
	ViewStartup    ViewID = iota
	ViewDashboard
	ViewLogs
	ViewAction
	ViewEnvEditor
	ViewPopup
	ViewSimulation
)

// Model is the root bubbletea model for the TUI.
type Model struct {
	width  int
	height int

	activeView ViewID
	prevView   ViewID // view active before popup was shown

	startup    views.StartupModel
	dashboard  views.DashboardModel
	logs       views.LogsModel
	action     views.ActionModel
	envEditor  views.EnvEditorModel
	popup      views.PopupModel
	simulation views.SimulationModel

	runner    runner.Runner
	envPath   string
	scriptDir string

	// action stream pump — channels are set when a CommandStream is started
	actionLines <-chan string
	actionExit  <-chan int
}

// NewModel constructs the root model with runner wiring.
func NewModel(scriptDir, profile string) Model {
	envPath := filepath.Join(scriptDir, ".env")
	envValues, _ := config.ReadEnvValues(envPath)

	r, _ := runner.NewRunner(profile, runner.RunnerConfig{
		ScriptDir: scriptDir,
		EnvPath:   envPath,
		EnvValues: envValues,
	})

	m := Model{
		scriptDir:  scriptDir,
		envPath:    envPath,
		activeView: ViewStartup,
		runner:     r,
		startup:    views.NewStartupModel(scriptDir),
	}
	m.dashboard.Runner = r
	m.envEditor.EnvPath = envPath
	m.envEditor.ExamplePath = filepath.Join(scriptDir, ".env.example")

	return m
}

func (m Model) Init() tea.Cmd {
	return m.startup.Init()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.startup.Width = msg.Width
		m.startup.Height = msg.Height
		m.dashboard.Width = msg.Width
		m.dashboard.Height = msg.Height
		m.logs.Width = msg.Width
		m.logs.Height = msg.Height
		m.action.Width = msg.Width
		m.action.Height = msg.Height
		m.envEditor.Width = msg.Width
		m.envEditor.Height = msg.Height
		m.popup.Width = msg.Width
		m.popup.Height = msg.Height
		m.simulation.Width = msg.Width
		m.simulation.Height = msg.Height
		// Delegate to active view so it can resize internal state.
		return m.delegateToActive(msg)

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}

	case NavigateTo:
		if msg.View == ViewPopup {
			m.prevView = m.activeView
		}
		m.activeView = msg.View
		if msg.View == ViewDashboard {
			return m, dashboardActivatedCmd()
		}
		return m, nil

	case GoBack:
		m.activeView = ViewDashboard
		return m, dashboardActivatedCmd()

	case views.ActionGoBackMsg:
		m.activeView = ViewDashboard
		return m, dashboardActivatedCmd()

	case views.LogsGoBackMsg:
		m.activeView = ViewDashboard
		return m, dashboardActivatedCmd()

	case ActionLineReceived:
		m.action.AppendLine(msg.Line)
		return m, m.nextActionLineCmd()

	case ActionCompleted:
		m.action.SetDone(msg.ExitCode)
		m.actionLines = nil
		m.actionExit = nil
		return m, nil

	case views.NavigateToDashboardMsg:
		m.activeView = ViewDashboard
		return m, dashboardActivatedCmd()

	case views.OpenEnvEditorMsg:
		m.activeView = ViewEnvEditor
		return m, nil

	// ---- Dashboard popup-trigger messages ----

	case views.DashShowContainerMenuMsg:
		pop := views.ShowContainerMenu(msg.Service, m.runner)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowStackStartMsg:
		pop := views.ShowStackStartMenu(m.runner)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowStackStopMsg:
		pop := views.ShowStackStopMenu(m.runner)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowStackRestartMsg:
		pop := views.ShowStackRestartMenu(m.runner)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowStackBuildMsg:
		pop := views.ShowStackBuildMenu(m.runner)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowDestroyMsg:
		pop := views.ShowDestroyMenu(m.runner)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowTestingMenuMsg:
		envValues, _ := config.ReadEnvValues(m.envPath)
		testURL := runner.BuildTestingURL(envValues)
		pop := views.ShowTestingMenu(testURL)
		pop.SetReturnViewID(int(ViewDashboard))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashShowHelpMsg:
		pop := views.ShowHelpPopup(m.width, m.height)
		pop.SetReturnViewID(int(ViewDashboard))
		m.popup = pop
		m.prevView = m.activeView
		m.activeView = ViewPopup
		return m, nil

	case views.DashOpenEnvEditorMsg:
		m.activeView = ViewEnvEditor
		return m, nil

	// ---- Env editor popup trigger ----

	case views.EnvEditorShowSaveMenuMsg:
		pop := views.ShowEnvSaveMenu()
		pop.SetReturnViewID(int(ViewEnvEditor))
		pop.Width = m.width
		pop.Height = m.height
		m.popup = pop
		m.prevView = ViewEnvEditor
		m.activeView = ViewPopup
		return m, nil

	// ---- Popup dismiss / action ----

	case views.PopupGoBackMsg:
		m.activeView = ViewID(msg.ReturnView)
		if m.activeView == ViewDashboard {
			return m, dashboardActivatedCmd()
		}
		return m, nil

	case views.PopupActionMsg:
		if msg.Cmd != nil {
			return m, msg.Cmd
		}
		return m, nil

	// ---- Stream started (popup runner action) ----

	case views.StreamStartedMsg:
		if msg.Err() != nil {
			// Show error in action view
			title := m.popup.Title
			m.action = views.NewActionModel(title, nil, m.width, m.height)
			m.action.AppendLine("Error: " + msg.Err().Error())
			m.action.SetDone(1)
			m.activeView = ViewAction
			return m, nil
		}
		cs, ok := msg.Stream().(runner.CommandStream)
		if !ok {
			return m, nil
		}
		title := m.popup.Title
		m.action = views.NewActionModel(title, msg.Cancel(), m.width, m.height)
		m.actionLines = cs.Lines
		m.actionExit = cs.ExitCode
		m.activeView = ViewAction
		return m, m.nextActionLineCmd()

	// ---- Open browser ----

	case views.OpenBrowserMsg:
		openBrowser(msg.URL)
		return m, nil

	// ---- Navigate to full-screen logs ----

	case views.NavigateToLogsMsg:
		if m.runner == nil {
			return m, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.logs.SetContainer(msg.Service)
		m.logs.SetCancelFunc(cancel)
		ch, err := m.runner.StreamLogs(ctx, msg.Service, 500)
		if err != nil {
			cancel()
			return m, nil
		}
		m.activeView = ViewLogs
		return m, m.logs.StartStreaming(ctx, ch)

	// ---- Env save-menu actions (from popup) ----

	case views.EnvSaveActionMsg:
		switch msg.Mode() {
		case views.EnvSaveDiscard:
			m.activeView = ViewDashboard
			return m, dashboardActivatedCmd()
		case views.EnvSaveExit:
			m.activeView = ViewDashboard
			return m, tea.Batch(m.envEditor.SaveOnly(), dashboardActivatedCmd())
		case views.EnvSaveRestart:
			return m, m.envEditor.SaveRestartCmd()
		case views.EnvSaveRebuild:
			return m, m.envEditor.SaveRebuildCmd()
		}
		return m, nil

	// ---- Env editor post-save nav ----

	case views.EnvEditorSaveRestartMsg:
		stream, err := m.runner.RestartAll(context.Background())
		if err != nil {
			return m, nil
		}
		m.action = views.NewActionModel("Restart Stack", nil, m.width, m.height)
		m.actionLines = stream.Lines
		m.actionExit = stream.ExitCode
		m.activeView = ViewAction
		return m, m.nextActionLineCmd()

	case views.EnvEditorSaveRebuildMsg:
		stream, err := m.runner.BuildAll(context.Background())
		if err != nil {
			return m, nil
		}
		m.action = views.NewActionModel("Build Stack", nil, m.width, m.height)
		m.actionLines = stream.Lines
		m.actionExit = stream.ExitCode
		m.activeView = ViewAction
		return m, m.nextActionLineCmd()

	case views.SimulationModeMsg:
		envValues, _ := config.ReadEnvValues(m.envPath)
		host := envValues["MQTT_HOST"]
		if host == "" {
			host = "localhost"
		}
		port := envValues["MQTT_PORT"]
		if port == "" {
			port = "1883"
		}
		sim := views.NewSimulationModel(
			msg.Swarm(),
			host, port,
			envValues["MQTT_USERNAME"],
			envValues["MQTT_PASSWORD"],
			m.width, m.height,
		)
		m.simulation = sim
		m.activeView = ViewSimulation
		return m, sim.Init()
	}

	// Delegate to active sub-model.
	return m.delegateToActive(msg)
}

func (m Model) delegateToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activeView {
	case ViewStartup:
		var updated views.StartupModel
		updated, cmd = m.startup.Update(msg)
		m.startup = updated
	case ViewDashboard:
		var updated views.DashboardModel
		updated, cmd = m.dashboard.Update(msg)
		m.dashboard = updated
	case ViewLogs:
		var updated views.LogsModel
		updated, cmd = m.logs.Update(msg)
		m.logs = updated
	case ViewAction:
		var updated views.ActionModel
		updated, cmd = m.action.Update(msg)
		m.action = updated
	case ViewEnvEditor:
		var updated views.EnvEditorModel
		updated, cmd = m.envEditor.Update(msg)
		m.envEditor = updated
	case ViewPopup:
		var updated views.PopupModel
		updated, cmd = m.popup.Update(msg)
		m.popup = updated
	case ViewSimulation:
		var updated views.SimulationModel
		updated, cmd = m.simulation.Update(msg)
		m.simulation = updated
	}
	return m, cmd
}

func (m Model) View() string {
	if m.width > 0 && m.height > 0 && (m.width < 80 || m.height < 24) {
		return resizeWarning(m.width, m.height)
	}

	if m.activeView == ViewPopup {
		// Render the background view, then overlay the popup box on top.
		var bg string
		switch m.prevView {
		case ViewLogs:
			bg = m.logs.View()
		case ViewAction:
			bg = m.action.View()
		case ViewEnvEditor:
			bg = m.envEditor.View()
		default:
			bg = m.dashboard.View()
		}
		pop := m.popup.View()
		if pop == "" {
			return bg
		}
		return overlayPopup(bg, pop, m.width, m.height)
	}

	switch m.activeView {
	case ViewStartup:
		return m.startup.View()
	case ViewDashboard:
		return m.dashboard.View()
	case ViewLogs:
		return m.logs.View()
	case ViewAction:
		return m.action.View()
	case ViewEnvEditor:
		return m.envEditor.View()
	case ViewSimulation:
		return m.simulation.View()
	}
	return ""
}

// overlayPopup centers pop on top of bg using line-level string replacement.
func overlayPopup(bg, pop string, w, h int) string {
	bgLines := strings.Split(bg, "\n")
	popLines := strings.Split(strings.TrimRight(pop, "\n"), "\n")

	// Ensure background has enough rows.
	for len(bgLines) < h {
		bgLines = append(bgLines, strings.Repeat(" ", w))
	}

	popH := len(popLines)
	popW := 0
	for _, l := range popLines {
		if vw := lipgloss.Width(l); vw > popW {
			popW = vw
		}
	}

	startRow := (h - popH) / 2
	startCol := (w - popW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	for i, pLine := range popLines {
		row := startRow + i
		if row >= len(bgLines) {
			break
		}
		bgVW := lipgloss.Width(bgLines[row])
		leftPad := strings.Repeat(" ", startCol)
		rightPadW := bgVW - startCol - popW
		if rightPadW < 0 {
			rightPadW = 0
		}
		rightPad := strings.Repeat(" ", rightPadW)
		bgLines[row] = leftPad + pLine + rightPad
	}

	return strings.Join(bgLines, "\n")
}

func dashboardActivatedCmd() tea.Cmd {
	return func() tea.Msg { return views.DashboardActivatedMsg{} }
}

// nextActionLineCmd reads one line from the active action stream and returns
// either ActionLineReceived or ActionCompleted. Returns nil when no stream is active.
func (m Model) nextActionLineCmd() tea.Cmd {
	if m.actionLines == nil {
		return nil
	}
	lines := m.actionLines
	exit := m.actionExit
	return func() tea.Msg {
		line, ok := <-lines
		if ok {
			return ActionLineReceived{Line: line}
		}
		code := <-exit
		return ActionCompleted{ExitCode: code}
	}
}

// openBrowser opens url in the system default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// resizeWarning renders a centered "terminal too small" overlay.
func resizeWarning(w, h int) string {
	msg := fmt.Sprintf(" Terminal too small (%dx%d). Please resize to at least 80×24. ", w, h)
	style := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}).
		Padding(0, 1)
	box := style.Render(msg)
	bw := lipgloss.Width(box)
	bh := lipgloss.Height(box)
	col := (w - bw) / 2
	row := (h - bh) / 2
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	var lines []string
	for i := 0; i < row; i++ {
		lines = append(lines, "")
	}
	for _, l := range strings.Split(box, "\n") {
		lines = append(lines, strings.Repeat(" ", col)+l)
	}
	return strings.Join(lines, "\n")
}
