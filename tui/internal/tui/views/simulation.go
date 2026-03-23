package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	mqttsim "agrisense/internal/mqtt"
)

// SimLineReceived carries a line of output from the MQTT simulator.
type SimLineReceived struct{ Line string }

// simDoneMsg is sent when the simulator output channel is closed.
type simDoneMsg struct{}

type simState int

const (
	simStatePromptDevices  simState = iota // swarm only: ask # devices
	simStatePromptRequests                  // swarm only: ask # requests
	simStateRunning
)

var (
	simColorText  = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	simColorMuted = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	simColorBlue  = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
)

// SimulationModel is the MQTT simulation view.
type SimulationModel struct {
	Width  int
	Height int

	// mode
	swarm bool
	state simState

	// config
	mqttHost     string
	mqttPort     string
	mqttUsername string
	mqttPassword string

	// prompts (swarm only)
	input       textinput.Model
	numDevices  int
	numRequests int

	// running
	action   ActionModel
	lines    chan string
	ctx      context.Context
	cancel   context.CancelFunc
	devSim   *mqttsim.DeviceSimulator
	swarmSim *mqttsim.SwarmSimulator
}

// NewSimulationModel constructs a SimulationModel for the given mode.
func NewSimulationModel(swarm bool, mqttHost, mqttPort, mqttUsername, mqttPassword string, width, height int) SimulationModel {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 8
	ti.Width = 20

	m := SimulationModel{
		Width:        width,
		Height:       height,
		swarm:        swarm,
		mqttHost:     mqttHost,
		mqttPort:     mqttPort,
		mqttUsername: mqttUsername,
		mqttPassword: mqttPassword,
		input:        ti,
	}

	if swarm {
		m.state = simStatePromptDevices
		m.input.Placeholder = "5"
	} else {
		m.state = simStateRunning
		m = m.startSingle()
	}
	return m
}

func (m SimulationModel) Init() tea.Cmd {
	if m.state == simStateRunning {
		return tea.Batch(m.action.Init(), m.listenCmd())
	}
	return textinput.Blink
}

func (m SimulationModel) Update(msg tea.Msg) (SimulationModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.state == simStateRunning {
			var cmd tea.Cmd
			m.action, cmd = m.action.Update(msg)
			return m, cmd
		}
		return m, nil

	case SimLineReceived:
		if m.state == simStateRunning {
			m.action.AppendLine(msg.Line)
		}
		return m, m.listenCmd()

	case simDoneMsg:
		if m.state == simStateRunning {
			m.action.SetDone(0)
		}
		return m, nil

	case ActionGoBackMsg:
		// stop simulator and return to dashboard
		m.stopSim()
		return m, func() tea.Msg { return ActionGoBackMsg{} }

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.state == simStateRunning {
		var cmd tea.Cmd
		m.action, cmd = m.action.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m SimulationModel) handleKey(msg tea.KeyMsg) (SimulationModel, tea.Cmd) {
	if m.state == simStateRunning {
		switch msg.String() {
		case "s", "S":
			m.stopSim()
			m.action.SetDone(0)
			return m, nil
		case "esc":
			if !m.action.running {
				m.stopSim()
				return m, func() tea.Msg { return ActionGoBackMsg{} }
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.action, cmd = m.action.Update(msg)
		return m, cmd
	}

	// prompt states
	switch msg.String() {
	case "enter":
		return m.confirmPrompt()
	case "esc":
		return m, func() tea.Msg { return ActionGoBackMsg{} }
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m SimulationModel) confirmPrompt() (SimulationModel, tea.Cmd) {
	val := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")

	switch m.state {
	case simStatePromptDevices:
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			n = 5
		}
		m.numDevices = n
		m.state = simStatePromptRequests
		m.input.Placeholder = "20"
		m.input.Focus()
		return m, textinput.Blink

	case simStatePromptRequests:
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			n = 20
		}
		m.numRequests = n
		m.state = simStateRunning
		m = m.startSwarm()
		return m, tea.Batch(m.action.Init(), m.listenCmd())
	}
	return m, nil
}

func (m SimulationModel) startSingle() SimulationModel {
	lines := make(chan string, 128)
	ctx, cancel := context.WithCancel(context.Background())
	dev := mqttsim.NewDeviceSimulator(0, m.mqttHost, m.mqttPort, m.mqttUsername, m.mqttPassword, lines)
	go dev.Start(ctx)

	m.lines = lines
	m.ctx = ctx
	m.cancel = cancel
	m.devSim = dev
	m.action = NewActionModel("Simulate Single Device", cancel, m.Width, m.Height)
	return m
}

func (m SimulationModel) startSwarm() SimulationModel {
	lines := make(chan string, 256)
	ctx, cancel := context.WithCancel(context.Background())
	sw := mqttsim.NewSwarmSimulator(m.numDevices, m.numRequests, m.mqttHost, m.mqttPort, m.mqttUsername, m.mqttPassword, lines)
	sw.Start(ctx)

	m.lines = lines
	m.ctx = ctx
	m.cancel = cancel
	m.swarmSim = sw
	title := fmt.Sprintf("Simulate Swarm (%d devices)", m.numDevices)
	m.action = NewActionModel(title, cancel, m.Width, m.Height)
	return m
}

func (m *SimulationModel) stopSim() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// listenCmd returns a Cmd that reads the next line from the simulator channel.
func (m SimulationModel) listenCmd() tea.Cmd {
	if m.lines == nil {
		return nil
	}
	ch := m.lines
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return simDoneMsg{}
		}
		return SimLineReceived{Line: line}
	}
}

func (m SimulationModel) View() string {
	if m.Width == 0 {
		return ""
	}
	if m.state == simStateRunning {
		return m.action.View()
	}
	return m.renderPrompt()
}

func (m SimulationModel) renderPrompt() string {
	var title string
	switch m.state {
	case simStatePromptDevices:
		title = "Number of devices (default 5)"
	case simStatePromptRequests:
		title = "Requests per device (0 = infinite, default 20)"
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(simColorBlue).
		Padding(1, 3).
		Width(50)

	titleStyle := lipgloss.NewStyle().Foreground(simColorText).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(simColorMuted)

	inner := titleStyle.Render(title) + "\n\n" +
		m.input.View() + "\n\n" +
		hintStyle.Render("[Enter] Confirm  [Esc] Cancel")

	box := boxStyle.Render(inner)
	bw := lipgloss.Width(box)
	bh := lipgloss.Height(box)

	left := (m.Width - bw) / 2
	top := (m.Height - bh) / 2
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}

	lines := make([]string, top)
	for _, l := range strings.Split(box, "\n") {
		lines = append(lines, strings.Repeat(" ", left)+l)
	}
	return strings.Join(lines, "\n")
}
