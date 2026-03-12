package views

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agrisense/internal/runner"
)

// ---------------------------------------------------------------------------
// Color palette (inline to avoid import cycle with internal/tui/styles.go)
// ---------------------------------------------------------------------------

var (
	popColorText     = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	popColorMuted    = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	popColorGreen    = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	popColorYellow   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	popColorRed      = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	popColorBlue     = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	popColorCyan     = lipgloss.AdaptiveColor{Light: "#0598bc", Dark: "#39c5cf"}
	popColorOrange   = lipgloss.AdaptiveColor{Light: "#bc4c00", Dark: "#e3b341"}
	popColorSubtle   = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
)

// ---------------------------------------------------------------------------
// ViewID constants (must match internal/tui/app.go)
// ---------------------------------------------------------------------------

// popViewID mirrors the ViewID enum from app.go — kept private to avoid
// an import cycle (views → tui would be circular).
type popViewID int

const (
	popViewDashboard  popViewID = 1
	popViewLogs       popViewID = 2
	popViewAction     popViewID = 3
	popViewEnvEditor  popViewID = 4
	popViewSimulation popViewID = 6
)

// ---------------------------------------------------------------------------
// Keybindings
// ---------------------------------------------------------------------------

type popupKeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
	Enter key.Binding
	Esc   key.Binding
}

var popupKeys = popupKeyMap{
	Up:    key.NewBinding(key.WithKeys("up")),
	Down:  key.NewBinding(key.WithKeys("down")),
	Left:  key.NewBinding(key.WithKeys("left")),
	Right: key.NewBinding(key.WithKeys("right")),
	Enter: key.NewBinding(key.WithKeys("enter")),
	Esc:   key.NewBinding(key.WithKeys("esc")),
}

// ---------------------------------------------------------------------------
// Option
// ---------------------------------------------------------------------------

// Option is a single selectable entry in the popup.
type Option struct {
	Label       string
	Description string
	Action      func() tea.Cmd
	Color       lipgloss.Color // auto-assigned when zero
}

// resolvedColor returns the option's color, auto-assigning if not set.
func (o Option) resolvedColor() lipgloss.TerminalColor {
	if o.Color != "" {
		return o.Color
	}
	lower := strings.ToLower(o.Label)
	switch {
	case containsAny(lower, "start", "logs", "save", "open"):
		return popColorGreen
	case containsAny(lower, "stop", "destroy", "discard", "delete", "remove"):
		return popColorRed
	case containsAny(lower, "build", "restart", "rebuild", "generate"):
		return popColorOrange
	case containsAny(lower, "cancel", "back"):
		return popColorMuted
	default:
		return popColorCyan
	}
}

func containsAny(s string, keywords ...string) bool {
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// PopupModel
// ---------------------------------------------------------------------------

// PopupModel is the generic popup overlay.
type PopupModel struct {
	Title      string
	Options    []Option
	selected   int
	Width      int
	Height     int
	returnView popViewID

	// Help overlay variant — set by ShowHelpPopup.
	isHelp      bool
	helpVP      viewport.Model
	helpContent string
}

// SetReturnViewID sets the view to return to on Esc. Accepts an int so callers
// in app.go can pass their ViewID without an import cycle.
func (m *PopupModel) SetReturnViewID(id int) {
	m.returnView = popViewID(id)
}

// ReturnViewID returns the return view as an int.
func (m PopupModel) ReturnViewID() int {
	return int(m.returnView)
}

// PopupGoBackMsg is emitted when the popup is dismissed (Esc or Cancel).
type PopupGoBackMsg struct{ ReturnView int }

// PopupActionMsg is emitted when the user confirms an option.
// The Action func has already been called; this carries any resulting Cmd.
type PopupActionMsg struct{ Cmd tea.Cmd }

func (m PopupModel) Init() tea.Cmd { return nil }

func (m PopupModel) Update(msg tea.Msg) (PopupModel, tea.Cmd) {
	if m.isHelp {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if key.Matches(msg, popupKeys.Esc) {
				return m, func() tea.Msg { return PopupGoBackMsg{ReturnView: int(m.returnView)} }
			}
			var cmd tea.Cmd
			m.helpVP, cmd = m.helpVP.Update(msg)
			return m, cmd
		default:
			var cmd tea.Cmd
			m.helpVP, cmd = m.helpVP.Update(msg)
			return m, cmd
		}
	}

	if len(m.Options) == 0 {
		if km, ok := msg.(tea.KeyMsg); ok && key.Matches(km, popupKeys.Esc) {
			return m, func() tea.Msg { return PopupGoBackMsg{ReturnView: int(m.returnView)} }
		}
		return m, nil
	}

	cols := m.columns()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, popupKeys.Esc):
			// Cancel: return to previous view
			return m, func() tea.Msg { return PopupGoBackMsg{ReturnView: int(m.returnView)} }

		case key.Matches(msg, popupKeys.Enter):
			opt := m.Options[m.selected]
			if opt.Action == nil {
				return m, func() tea.Msg { return PopupGoBackMsg{ReturnView: int(m.returnView)} }
			}
			cmd := opt.Action()
			return m, func() tea.Msg { return PopupActionMsg{Cmd: cmd} }

		case key.Matches(msg, popupKeys.Left):
			m.selected = max0(m.selected-1, 0)

		case key.Matches(msg, popupKeys.Right):
			m.selected = min0(m.selected+1, len(m.Options)-1)

		case key.Matches(msg, popupKeys.Up):
			m.selected = max0(m.selected-cols, 0)

		case key.Matches(msg, popupKeys.Down):
			m.selected = min0(m.selected+cols, len(m.Options)-1)
		}
	}
	return m, nil
}

func (m PopupModel) View() string {
	if m.isHelp {
		return m.renderHelp()
	}

	if len(m.Options) == 0 {
		return ""
	}

	cols := m.columns()

	// Build option rows (raw, without centering padding)
	var rawRows []string
	for i := 0; i < len(m.Options); i += cols {
		var parts []string
		for j := i; j < i+cols && j < len(m.Options); j++ {
			opt := m.Options[j]
			label := "[ " + opt.Label + " ]"
			style := lipgloss.NewStyle().Foreground(opt.resolvedColor())
			if j == m.selected {
				style = style.Bold(true).Underline(true)
			}
			parts = append(parts, style.Render(label))
		}
		rawRows = append(rawRows, strings.Join(parts, "   "))
	}

	// Measure popup width first, then center each row
	innerWidth := m.popupInnerWidth(rawRows)
	var rows []string
	for _, r := range rawRows {
		rows = append(rows, centeredPad(r, innerWidth))
	}

	// Description bar
	desc := ""
	if d := m.Options[m.selected].Description; d != "" {
		desc = lipgloss.NewStyle().Foreground(popColorMuted).Render("  ℹ " + d)
	}

	// Divider
	divider := strings.Repeat("─", innerWidth)

	// Header
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(popColorText)
	header := centeredPad(titleStyle.Render(m.Title), innerWidth)

	// Assemble inner content
	var inner []string
	inner = append(inner, header)
	inner = append(inner, divider)
	inner = append(inner, "")
	inner = append(inner, rows...)
	inner = append(inner, "")
	if desc != "" {
		inner = append(inner, divider)
		inner = append(inner, desc)
	}

	content := strings.Join(inner, "\n")

	// Apply rounded border box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(popColorSubtle).
		Padding(0, 1)

	return boxStyle.Render(content)
}

// columns returns how many options to show per row.
func (m PopupModel) columns() int {
	n := len(m.Options)
	if n <= 2 {
		return n
	}
	if n <= 4 {
		return 2
	}
	return 3
}

func (m PopupModel) popupInnerWidth(rows []string) int {
	w := lipgloss.Width(m.Title) + 4
	for _, r := range rows {
		if vw := lipgloss.Width(r); vw > w {
			w = vw
		}
	}
	// Include all descriptions so the box width stays stable while navigating.
	for _, opt := range m.Options {
		if opt.Description != "" {
			dw := lipgloss.Width("  ℹ " + opt.Description)
			if dw > w {
				w = dw
			}
		}
	}
	// minimum 40
	if w < 40 {
		w = 40
	}
	return w
}

func centeredPad(s string, width int) string {
	sw := lipgloss.Width(s)
	if sw >= width {
		return s
	}
	pad := (width - sw) / 2
	return strings.Repeat(" ", pad) + s
}

func max0(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min0(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Pre-built popup constructors
// ---------------------------------------------------------------------------

// ShowContainerMenu builds a per-container action popup.
func ShowContainerMenu(svc runner.ServiceStatus, runner runner.Runner) PopupModel {
	var opts []Option

	opts = append(opts, Option{
		Label:       "Full Logs",
		Description: "View full-screen log output for this container",
		Action: func() tea.Cmd {
			return func() tea.Msg {
				return navigateToLogsMsg{Service: svc.Name}
			}
		},
	})

	if svc.State == "running" || svc.State == "restarting" {
		opts = append(opts,
			Option{
				Label:       "Stop",
				Description: "Stop this container",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return runner.StopService(ctx, svc.Service)
					})
				},
			},
			Option{
				Label:       "Restart",
				Description: "Restart this container",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return runner.RestartService(ctx, svc.Service)
					})
				},
			},
		)
	} else {
		opts = append(opts, Option{
			Label:       "Start",
			Description: "Start this container",
			Action: func() tea.Cmd {
				return streamAction(func(ctx context.Context) (interface{}, error) {
					return runner.StartService(ctx, svc.Service)
				})
			},
		})
	}

	opts = append(opts, Option{
		Label:       "Build",
		Description: "Rebuild image for this container",
		Action: func() tea.Cmd {
			return streamAction(func(ctx context.Context) (interface{}, error) {
				return runner.BuildService(ctx, svc.Service)
			})
		},
	})

	lower := strings.ToLower(svc.Service)
	if strings.Contains(lower, "backend") || strings.Contains(lower, "frontend") {
		url := runner.ServiceURL(svc.Service)
		opts = append(opts, Option{
			Label:       "Open in Browser",
			Description: "Open " + url + " in your browser",
			Action: func() tea.Cmd {
				return openURLCmd(url)
			},
		})
	}

	opts = append(opts, Option{
		Label:       "Cancel",
		Description: "Close this menu",
		Action:      nil,
	})

	return PopupModel{
		Title:   svc.Name,
		Options: opts,
	}
}

// ShowStackStartMenu builds the start-all confirmation popup.
func ShowStackStartMenu(r runner.Runner) PopupModel {
	return PopupModel{
		Title: "Start Stack",
		Options: []Option{
			{
				Label:       "Start All",
				Description: "Start all containers",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.StartAll(ctx)
					})
				},
			},
			{Label: "Cancel", Description: "Close this menu"},
		},
	}
}

// ShowStackStopMenu builds the stop-all confirmation popup.
func ShowStackStopMenu(r runner.Runner) PopupModel {
	return PopupModel{
		Title: "Stop Stack",
		Options: []Option{
			{
				Label:       "Stop All",
				Description: "Stop all running containers",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.StopAll(ctx)
					})
				},
			},
			{Label: "Cancel", Description: "Close this menu"},
		},
	}
}

// ShowStackRestartMenu builds the restart-all confirmation popup.
func ShowStackRestartMenu(r runner.Runner) PopupModel {
	return PopupModel{
		Title: "Restart Stack",
		Options: []Option{
			{
				Label:       "Restart All",
				Description: "Restart all containers",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.RestartAll(ctx)
					})
				},
			},
			{Label: "Cancel", Description: "Close this menu"},
		},
	}
}

// ShowStackBuildMenu builds the build-and-start confirmation popup.
func ShowStackBuildMenu(r runner.Runner) PopupModel {
	return PopupModel{
		Title: "Build & Start Stack",
		Options: []Option{
			{
				Label:       "Build & Start",
				Description: "Rebuild images and start all containers",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.BuildAll(ctx)
					})
				},
			},
			{Label: "Cancel", Description: "Close this menu"},
		},
	}
}

// ShowDestroyMenu builds the destroy mode picker popup.
func ShowDestroyMenu(r runner.Runner) PopupModel {
	return PopupModel{
		Title: "Stop & Destroy Containers",
		Options: []Option{
			{
				Label:       "Containers Only",
				Description: "Remove containers and networks (down --remove-orphans)",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.Destroy(ctx, runner.DestroyContainersOnly)
					})
				},
			},
			{
				Label:       "+Volumes",
				Description: "Remove containers, networks, volumes and Supabase bind-mount data",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.Destroy(ctx, runner.DestroyContainersAndVolumes)
					})
				},
			},
			{
				Label:       "+Volumes+Builds",
				Description: "Remove containers, volumes, all images and Supabase bind-mount data",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.Destroy(ctx, runner.DestroyContainersVolumesImages)
					})
				},
			},
			{
				Label:       "Prune All",
				Description: "Full destroy plus docker builder prune",
				Action: func() tea.Cmd {
					return streamAction(func(ctx context.Context) (interface{}, error) {
						return r.Destroy(ctx, runner.DestroyPrune)
					})
				},
			},
			{Label: "Cancel", Description: "Close this menu"},
		},
	}
}

// ShowEnvSaveMenu builds the env-editor exit popup.
func ShowEnvSaveMenu() PopupModel {
	return PopupModel{
		Title: "Save Changes?",
		Options: []Option{
			{
				Label:       "Save & Restart",
				Description: "Save .env and restart containers",
				Action: func() tea.Cmd {
					return func() tea.Msg { return envSaveActionMsg{mode: envSaveRestart} }
				},
			},
			{
				Label:       "Save & Rebuild",
				Description: "Save .env and rebuild containers",
				Action: func() tea.Cmd {
					return func() tea.Msg { return envSaveActionMsg{mode: envSaveRebuild} }
				},
			},
			{
				Label:       "Save & Exit",
				Description: "Save .env and return to dashboard",
				Action: func() tea.Cmd {
					return func() tea.Msg { return envSaveActionMsg{mode: envSaveExit} }
				},
			},
			{
				Label:       "Discard & Exit",
				Description: "Discard all changes and return to dashboard",
				Action: func() tea.Cmd {
					return func() tea.Msg { return envSaveActionMsg{mode: envSaveDiscard} }
				},
			},
			{
				Label:       "Cancel",
				Description: "Stay in the .env editor",
				Action:      nil,
			},
		},
	}
}

// ShowTestingMenu builds the testing / simulation popup.
func ShowTestingMenu(testURL string) PopupModel {
	url := testURL
	return PopupModel{
		Title: "Testing & Simulation",
		Options: []Option{
			{
				Label:       "Open in Browser",
				Description: "Open the testing page in your browser",
				Action: func() tea.Cmd {
					return openURLCmd(url)
				},
			},
			{
				Label:       "Simulate Single Device",
				Description: "Start a single MQTT device simulator",
				Action: func() tea.Cmd {
					return func() tea.Msg { return simulationModeMsg{swarm: false} }
				},
			},
			{
				Label:       "Simulate Swarm",
				Description: "Configure and start a multi-device MQTT swarm",
				Action: func() tea.Cmd {
					return func() tea.Msg { return simulationModeMsg{swarm: true} }
				},
			},
			{Label: "Cancel", Description: "Close this menu"},
		},
	}
}

// ---------------------------------------------------------------------------
// Internal message types for popup actions
// ---------------------------------------------------------------------------

// navigateToLogsMsg asks the root model to switch to full-screen logs for a service.
type navigateToLogsMsg struct{ Service string }

// envSaveMode enumerates the env-save popup choices.
type envSaveMode int

const (
	envSaveRestart envSaveMode = iota
	envSaveRebuild
	envSaveExit
	envSaveDiscard
)

// envSaveActionMsg carries the chosen save mode back to the root model.
type envSaveActionMsg struct{ mode envSaveMode }

// Mode returns the chosen save mode for use in app.go.
func (e envSaveActionMsg) Mode() envSaveMode { return e.mode }

// EnvSaveMode re-exports for use in app.go without import cycle.
type EnvSaveMode = envSaveMode

const (
	EnvSaveRestart = envSaveRestart
	EnvSaveRebuild = envSaveRebuild
	EnvSaveExit    = envSaveExit
	EnvSaveDiscard = envSaveDiscard
)

// EnvSaveActionMsg re-exports for app.go.
type EnvSaveActionMsg = envSaveActionMsg

// simulationModeMsg asks root model to navigate to simulation view.
type simulationModeMsg struct{ swarm bool }

// SimulationModeMsg re-exports for app.go.
type SimulationModeMsg = simulationModeMsg

// Swarm returns true if swarm mode was selected.
func (s simulationModeMsg) Swarm() bool { return s.swarm }

// NavigateToLogsMsg re-exports for app.go.
type NavigateToLogsMsg = navigateToLogsMsg

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// streamAction wraps a runner call that returns (CommandStream, error) into a tea.Cmd.
// It creates a cancellable context so the TUI [S] Stop key can kill the process.
func streamAction(fn func(ctx context.Context) (interface{}, error)) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	return func() tea.Msg {
		stream, err := fn(ctx)
		if err != nil {
			cancel()
			return streamStartedMsg{err: err}
		}
		return streamStartedMsg{stream: stream, cancel: cancel}
	}
}

// streamStartedMsg is emitted after a runner action is started.
type streamStartedMsg struct {
	stream interface{}
	cancel context.CancelFunc
	err    error
}

// Err returns the error from the stream start attempt.
func (s streamStartedMsg) Err() error { return s.err }

// Cancel returns the cancel function to stop the running command.
func (s streamStartedMsg) Cancel() context.CancelFunc { return s.cancel }

// Stream returns the raw stream value (type-assert to runner.CommandStream in app.go).
func (s streamStartedMsg) Stream() interface{} { return s.stream }

// StreamStartedMsg re-exports for app.go.
type StreamStartedMsg = streamStartedMsg

// openURLCmd attempts to open a URL in the default browser.
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return openBrowserMsg{URL: url}
	}
}

// openBrowserMsg carries the URL to open.
type openBrowserMsg struct{ URL string }

// OpenBrowserMsg re-exports for app.go.
type OpenBrowserMsg = openBrowserMsg

// ---------------------------------------------------------------------------
// Help overlay
// ---------------------------------------------------------------------------

const helpText = `  Dashboard
  ↑ ↓        Navigate containers
  Enter      Open container menu
  S          Start all containers
  R          Restart all containers
  B          Rebuild all containers
  X          Stop all containers
  D          Destroy (pick mode)
  T          Testing & Simulation
  E          Edit .env file
  Q          Quit
  ?          Show this help

  Log panel (Tab to focus)
  ↑ ↓        Scroll logs
  End        Auto-tail (resume)
  F          Full-screen logs
  Tab        Switch focus

  Container Menu
  Full Logs  Stream container logs
  Start      Start container
  Stop       Stop container
  Restart    Restart container
  Build      Rebuild image
  Open       Open in browser

  Env Editor
  ↑ ↓        Navigate fields
  Enter      Edit field value
  G          Generate value
  Ctrl+S     Save
  Ctrl+R     Save + Restart
  Ctrl+B     Save + Rebuild
  Esc        Save/Discard menu

  [Esc] Close`

// ShowHelpPopup builds a viewport-based help popup for the dashboard ? key.
func ShowHelpPopup(width, height int) PopupModel {
	vpW := 44
	vpH := height - 8
	if vpH < 5 {
		vpH = 5
	}
	vp := viewport.New(vpW, vpH)
	vp.SetContent(helpText)
	return PopupModel{
		Title:       "Keyboard Shortcuts",
		isHelp:      true,
		helpVP:      vp,
		helpContent: helpText,
		Width:       width,
		Height:      height,
	}
}

// renderHelp renders the viewport-based help popup box.
func (m PopupModel) renderHelp() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(popColorText)
	header := centeredPad(titleStyle.Render(m.Title), m.helpVP.Width)
	divider := strings.Repeat("─", m.helpVP.Width)
	footer := lipgloss.NewStyle().Foreground(popColorMuted).Render(centeredPad("[Esc] Close", m.helpVP.Width))

	inner := strings.Join([]string{header, divider, "", m.helpVP.View(), "", divider, footer}, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(popColorSubtle).
		Padding(0, 1)
	return boxStyle.Render(inner)
}
