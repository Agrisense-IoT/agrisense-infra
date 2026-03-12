package views

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agrisense/internal/config"
)

// Inline color values matching internal/tui/styles.go — avoids import cycle.
var (
	startupColorText   = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	startupColorMuted  = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	startupColorSubtle = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
	startupColorGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	startupColorYellow = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	startupColorRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	startupColorBlue   = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
)

// ---------------------------------------------------------------------------
// Health check message types
// ---------------------------------------------------------------------------

type healthCheckResult struct {
	step    int
	ok      bool
	label   string
	warning string
	fatal   string
}

type healthCheckAllDone struct {
	needsWizard  bool
	missingFiles []string
}

// ---------------------------------------------------------------------------
// Wizard step enum
// ---------------------------------------------------------------------------

type wizardStep int

const (
	stepExistingEnv  wizardStep = iota
	stepDashboard
	stepURLs
	stepDatabase
	stepPorts
	stepStartupOpts
	stepSecrets
	stepWriting
)

// ---------------------------------------------------------------------------
// Startup phases
// ---------------------------------------------------------------------------

type startupPhase int

const (
	phaseHealthCheck startupPhase = iota
	phaseFatalError
	phaseWizard
	phaseComplete
)

// ---------------------------------------------------------------------------
// choiceWidget
// ---------------------------------------------------------------------------

type choiceWidget struct {
	options  []string
	cursor   int
	selected int
}

func newChoiceWidget(options []string) choiceWidget {
	return choiceWidget{options: options, cursor: 0, selected: -1}
}

func (c *choiceWidget) Up() {
	if c.cursor > 0 {
		c.cursor--
	}
}

func (c *choiceWidget) Down() {
	if c.cursor < len(c.options)-1 {
		c.cursor++
	}
}

func (c *choiceWidget) Select() int {
	c.selected = c.cursor
	return c.cursor
}

func (c choiceWidget) View() string {
	var b strings.Builder
	cursorStyle := lipgloss.NewStyle().Foreground(startupColorBlue)
	normal := lipgloss.NewStyle().Foreground(startupColorText)
	for i, opt := range c.options {
		if i == c.cursor {
			b.WriteString(cursorStyle.Render("  ❯ " + opt))
		} else {
			b.WriteString(normal.Render("    " + opt))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Port check types
// ---------------------------------------------------------------------------

type portEntry struct {
	key     string
	label   string
	port    int
	free    bool
	checked bool
}

type portCheckDone struct {
	results []portEntry
}

type portCheckAdvance struct{}

// Secret generation
type secretGenProgress struct {
	index int
	label string
	done  bool
}

type secretGenAdvance struct{}

// Navigation signals emitted by the startup view
type NavigateToDashboardMsg struct{}
type OpenEnvEditorMsg struct{}

// ---------------------------------------------------------------------------
// StartupModel
// ---------------------------------------------------------------------------

type StartupModel struct {
	Width  int
	Height int

	ScriptDir   string
	EnvPath     string
	ExamplePath string

	phase startupPhase

	// Health check
	checkResults []healthCheckResult
	fatalMsg     string
	warnings     []string

	// Wizard
	wizStep      wizardStep
	envExists    bool
	skipExisting bool

	// Step 0: existing env
	existingChoice choiceWidget

	// Step 1: dashboard
	dashUser          textinput.Model
	dashPass          textinput.Model
	dashFocusField    int
	dashError         string
	dashPassGenerated bool

	// Step 2: URLs
	urlFrontend   textinput.Model
	urlSupabase   textinput.Model
	urlCallback   textinput.Model
	urlFocusField int
	urlError      string

	// Step 3: database
	dbPass          textinput.Model
	dbError         string
	dbPassGenerated bool

	// Step 4: ports
	portsChoice   choiceWidget
	portsCustom   bool
	portInputs    []textinput.Model
	portKeys      []string
	portLabels    []string
	portDefaults  []string
	portFocus     int
	portError     string
	portChecking  bool
	portResults   []portEntry
	portConflicts []int
	portFixInput  textinput.Model
	portFixIdx    int
	portFixError  string

	// Step 5: startup options
	seedChoice choiceWidget

	// Step 6: secrets
	secretsChoice     choiceWidget
	secretsAuto       bool
	secretGenList     []string
	secretGenIdx      int
	secretGenDone     bool
	secretManualKeys  []string
	secretManualIdx   int
	secretManualInput textinput.Model
	secretManualDone  bool

	// Step 7: writing
	writeError   string
	writeDone    bool
	placeholders []string
	sections     []config.EnvSection

	// Accumulated wizard values
	wizardValues map[string]string
}

func NewStartupModel(scriptDir string) StartupModel {
	envPath := filepath.Join(scriptDir, ".env")
	examplePath := filepath.Join(scriptDir, ".env.example")

	dashUser := textinput.New()
	dashUser.Placeholder = "admin"
	dashUser.SetValue("admin")
	dashUser.Focus()
	dashUser.CharLimit = 64

	dashPass := textinput.New()
	dashPass.Placeholder = "leave blank to auto-generate"
	dashPass.EchoMode = textinput.EchoPassword
	dashPass.CharLimit = 128

	urlFrontend := textinput.New()
	urlFrontend.Placeholder = "http://localhost:3000"
	urlFrontend.SetValue("http://localhost:3000")
	urlFrontend.CharLimit = 256

	urlSupabase := textinput.New()
	urlSupabase.Placeholder = "http://localhost:8000"
	urlSupabase.SetValue("http://localhost:8000")
	urlSupabase.CharLimit = 256

	urlCallback := textinput.New()
	urlCallback.Placeholder = "same as Supabase URL"
	urlCallback.SetValue("http://localhost:8000")
	urlCallback.CharLimit = 256

	dbPass := textinput.New()
	dbPass.Placeholder = "leave blank to auto-generate"
	dbPass.EchoMode = textinput.EchoPassword
	dbPass.CharLimit = 128

	portKeys := []string{"PORT", "FRONTEND_PORT", "KONG_HTTP_PORT", "KONG_HTTPS_PORT", "POSTGRES_PORT", "POOLER_PROXY_PORT_TRANSACTION", "MQTT_PORT"}
	portLabels := []string{"API", "Frontend", "Kong HTTP", "Kong HTTPS", "PostgreSQL", "Pooler Proxy", "MQTT"}
	portDefaults := []string{"3143", "3000", "8000", "8443", "5432", "6543", "1883"}

	portInputs := make([]textinput.Model, len(portKeys))
	for i := range portKeys {
		ti := textinput.New()
		ti.Placeholder = portDefaults[i]
		ti.SetValue(portDefaults[i])
		ti.CharLimit = 6
		portInputs[i] = ti
	}

	portFixInput := textinput.New()
	portFixInput.CharLimit = 6

	secretManualInput := textinput.New()
	secretManualInput.Placeholder = "press Enter to leave placeholder"
	secretManualInput.CharLimit = 512

	return StartupModel{
		ScriptDir:   scriptDir,
		EnvPath:     envPath,
		ExamplePath: examplePath,
		phase:       phaseHealthCheck,
		wizardValues: make(map[string]string),

		dashUser: dashUser,
		dashPass: dashPass,

		urlFrontend: urlFrontend,
		urlSupabase: urlSupabase,
		urlCallback: urlCallback,

		dbPass: dbPass,

		existingChoice: newChoiceWidget([]string{
			"Overwrite it",
			"Back it up, then create a fresh one",
			"Abort — keep the existing file",
		}),
		portsChoice: newChoiceWidget([]string{
			"No — use defaults  (API: 3143  Frontend: 3000  Kong: 8000/8443)",
			"Yes — enter manually",
		}),
		portKeys:     portKeys,
		portLabels:   portLabels,
		portDefaults: portDefaults,
		portInputs:   portInputs,
		portFixInput: portFixInput,

		seedChoice: newChoiceWidget([]string{"Yes", "No"}),
		secretsChoice: newChoiceWidget([]string{
			"Yes — generate everything automatically",
			"No  — I will enter them manually",
		}),
		secretManualInput: secretManualInput,

		secretGenList: []string{
			"JWT_SECRET",
			"ANON_KEY",
			"SERVICE_ROLE_KEY",
			"SECRET_KEY_BASE",
			"VAULT_ENC_KEY",
			"PG_META_CRYPTO_KEY",
			"LOGFLARE_PUBLIC_ACCESS_TOKEN",
			"LOGFLARE_PRIVATE_ACCESS_TOKEN",
			"S3_PROTOCOL_ACCESS_KEY_ID",
			"S3_PROTOCOL_ACCESS_KEY_SECRET",
		},
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m StartupModel) Init() tea.Cmd {
	return m.runHealthChecks()
}

func (m StartupModel) runHealthChecks() tea.Cmd {
	scriptDir := m.ScriptDir
	envPath := m.EnvPath
	examplePath := m.ExamplePath

	return func() tea.Msg {
		needsWizard := false
		var missingFiles []string

		// 1. .env.example must exist
		if _, err := os.Stat(examplePath); os.IsNotExist(err) {
			return healthCheckResult{
				step:  0,
				ok:    false,
				label: ".env.example",
				fatal: fmt.Sprintf(".env.example not found.\nExpected: %s", examplePath),
			}
		}

		// 2. .env exists and has no placeholders
		if _, err := os.Stat(envPath); os.IsNotExist(err) {
			needsWizard = true
		} else {
			sections, err := config.ParseEnvWithExample(envPath, examplePath)
			if err == nil {
				missing := config.FindMissingFields(sections)
				if len(missing) > 0 {
					needsWizard = true
				}
			}
		}

		// 3. Critical Supabase files
		critFiles := []string{
			filepath.Join(scriptDir, "supabase", "volumes", "db", "roles.sql"),
			filepath.Join(scriptDir, "supabase", "volumes", "api", "kong.yml"),
			filepath.Join(scriptDir, "supabase", "volumes", "logs", "vector.yml"),
		}
		for _, f := range critFiles {
			if _, err := os.Stat(f); os.IsNotExist(err) {
				missingFiles = append(missingFiles, f)
			}
		}

		// 4. Docker binary reachable
		if !isDockerAvailable() {
			return healthCheckResult{
				step:  3,
				ok:    false,
				label: "Docker",
				fatal: "Docker is not reachable.\nMake sure Docker is installed and running.",
			}
		}

		return healthCheckAllDone{
			needsWizard:  needsWizard,
			missingFiles: missingFiles,
		}
	}
}

func isDockerAvailable() bool {
	pathEnv := os.Getenv("PATH")
	sep := string(os.PathListSeparator)
	for _, dir := range strings.Split(pathEnv, sep) {
		for _, name := range []string{"docker", "docker.exe"} {
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m StartupModel) Update(msg tea.Msg) (StartupModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case healthCheckResult:
		if msg.fatal != "" {
			m.fatalMsg = msg.fatal
			m.phase = phaseFatalError
			return m, nil
		}
		m.checkResults = append(m.checkResults, msg)
		if msg.warning != "" {
			m.warnings = append(m.warnings, msg.warning)
		}
		return m, nil

	case healthCheckAllDone:
		if msg.needsWizard {
			m.phase = phaseWizard
			_, err := os.Stat(m.EnvPath)
			m.envExists = err == nil
			if m.envExists {
				m.wizStep = stepExistingEnv
			} else {
				m.wizStep = stepDashboard
				m.skipExisting = true
				m.dashUser.Focus()
			}
			if len(msg.missingFiles) > 0 {
				m.warnings = append(m.warnings, fmt.Sprintf("%d Supabase config file(s) missing", len(msg.missingFiles)))
			}
			return m, textinput.Blink
		}
		m.phase = phaseComplete
		return m, func() tea.Msg { return NavigateToDashboardMsg{} }

	case portCheckDone:
		m.portChecking = false
		m.portResults = msg.results
		m.portConflicts = nil
		for i, r := range msg.results {
			if !r.free {
				m.portConflicts = append(m.portConflicts, i)
			}
		}
		if len(m.portConflicts) == 0 {
			return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
				return portCheckAdvance{}
			})
		}
		m.portFixIdx = 0
		idx := m.portConflicts[0]
		m.portFixInput.Placeholder = fmt.Sprintf("free port for %s", m.portResults[idx].label)
		m.portFixInput.SetValue("")
		m.portFixInput.Focus()
		return m, textinput.Blink

	case portCheckAdvance:
		return m.advanceFromPorts()

	case secretGenProgress:
		if msg.done {
			m.secretGenDone = true
			return m, tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
				return secretGenAdvance{}
			})
		}
		m.secretGenIdx = msg.index + 1
		return m, nil

	case secretGenAdvance:
		return m.advanceWizard()

	case writeEnvDoneMsg:
		m.writeDone = true
		m.writeError = msg.err
		m.sections = msg.sections
		m.placeholders = msg.placeholders
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m.updateInputs(msg)
}

func (m StartupModel) handleKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	key := msg.String()

	if m.phase == phaseFatalError {
		switch key {
		case "q", "Q":
			return m, tea.Quit
		case "l", "L":
			logPath := filepath.Join(m.ScriptDir, "setup.log")
			content := fmt.Sprintf("AgriSense Setup Error\n%s\n\n%s\n", time.Now().Format(time.RFC3339), m.fatalMsg)
			_ = os.WriteFile(logPath, []byte(content), 0644)
			return m, tea.Quit
		}
		return m, nil
	}

	if m.phase == phaseComplete {
		if key == "enter" {
			return m, func() tea.Msg { return NavigateToDashboardMsg{} }
		}
		return m, nil
	}

	if m.phase != phaseWizard {
		return m, nil
	}

	switch m.wizStep {
	case stepExistingEnv:
		return m.handleExistingEnvKey(key)
	case stepDashboard:
		return m.handleDashboardKey(msg)
	case stepURLs:
		return m.handleURLsKey(msg)
	case stepDatabase:
		return m.handleDatabaseKey(msg)
	case stepPorts:
		return m.handlePortsKey(msg)
	case stepStartupOpts:
		return m.handleStartupOptsKey(key)
	case stepSecrets:
		return m.handleSecretsKey(msg)
	case stepWriting:
		return m.handleWritingKey(key)
	}
	return m, nil
}

func (m StartupModel) updateInputs(msg tea.Msg) (StartupModel, tea.Cmd) {
	var cmd tea.Cmd
	if m.phase != phaseWizard {
		return m, nil
	}
	switch m.wizStep {
	case stepDashboard:
		if m.dashFocusField == 0 {
			m.dashUser, cmd = m.dashUser.Update(msg)
		} else {
			m.dashPass, cmd = m.dashPass.Update(msg)
		}
	case stepURLs:
		switch m.urlFocusField {
		case 0:
			m.urlFrontend, cmd = m.urlFrontend.Update(msg)
		case 1:
			m.urlSupabase, cmd = m.urlSupabase.Update(msg)
		case 2:
			m.urlCallback, cmd = m.urlCallback.Update(msg)
		}
	case stepDatabase:
		m.dbPass, cmd = m.dbPass.Update(msg)
	case stepPorts:
		if m.portsCustom && !m.portChecking && len(m.portConflicts) == 0 && m.portResults == nil {
			if m.portFocus < len(m.portInputs) {
				m.portInputs[m.portFocus], cmd = m.portInputs[m.portFocus].Update(msg)
			}
		}
		if len(m.portConflicts) > 0 && m.portFixIdx < len(m.portConflicts) {
			m.portFixInput, cmd = m.portFixInput.Update(msg)
		}
	case stepSecrets:
		if !m.secretsAuto && !m.secretManualDone && m.secretManualIdx < len(m.secretManualKeys) {
			m.secretManualInput, cmd = m.secretManualInput.Update(msg)
		}
	}
	return m, cmd
}

// ---------------------------------------------------------------------------
// Step handlers
// ---------------------------------------------------------------------------

func (m StartupModel) handleExistingEnvKey(key string) (StartupModel, tea.Cmd) {
	switch key {
	case "up":
		m.existingChoice.Up()
	case "down":
		m.existingChoice.Down()
	case "enter":
		choice := m.existingChoice.Select()
		switch choice {
		case 0: // Overwrite
			m.wizStep = stepDashboard
			m.dashUser.Focus()
			return m, textinput.Blink
		case 1: // Backup
			ts := time.Now().Format("20060102_150405")
			backupPath := m.EnvPath + ".backup_" + ts
			data, err := os.ReadFile(m.EnvPath)
			if err == nil {
				_ = os.WriteFile(backupPath, data, 0600)
			}
			m.wizStep = stepDashboard
			m.dashUser.Focus()
			return m, textinput.Blink
		case 2: // Abort
			m.phase = phaseComplete
			return m, func() tea.Msg { return NavigateToDashboardMsg{} }
		}
	}
	return m, nil
}

func (m StartupModel) handleDashboardKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "tab", "down":
		if m.dashFocusField == 0 {
			m.dashFocusField = 1
			m.dashUser.Blur()
			m.dashPass.Focus()
			return m, textinput.Blink
		}
	case "shift+tab", "up":
		if m.dashFocusField == 1 {
			m.dashFocusField = 0
			m.dashPass.Blur()
			m.dashUser.Focus()
			return m, textinput.Blink
		}
	case "enter", "right":
		m.dashError = ""
		username := m.dashUser.Value()
		if username == "" {
			username = "admin"
			m.dashUser.SetValue(username)
		}
		password := m.dashPass.Value()
		if password == "" {
			pw, _ := config.GenerateValue("DASHBOARD_PASSWORD", m.wizardValues)
			password = pw
			m.dashPassGenerated = true
		} else if len(password) < 8 {
			m.dashError = "Password must be at least 8 characters"
			return m, nil
		}
		m.wizardValues["DASHBOARD_USERNAME"] = username
		m.wizardValues["DASHBOARD_PASSWORD"] = password
		m.dashPass.Blur()
		m.dashUser.Blur()
		return m.advanceWizard()
	case "left":
		if !m.skipExisting {
			m.wizStep = stepExistingEnv
			m.dashUser.Blur()
			m.dashPass.Blur()
			return m, nil
		}
	}
	var cmd tea.Cmd
	if m.dashFocusField == 0 {
		m.dashUser, cmd = m.dashUser.Update(msg)
	} else {
		m.dashPass, cmd = m.dashPass.Update(msg)
	}
	return m, cmd
}

func (m StartupModel) handleURLsKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "tab", "down":
		if m.urlFocusField < 2 {
			m.blurURLField(m.urlFocusField)
			m.urlFocusField++
			m.focusURLField(m.urlFocusField)
			if m.urlFocusField == 2 && m.urlCallback.Value() == "" {
				m.urlCallback.SetValue(m.urlSupabase.Value())
			}
			return m, textinput.Blink
		}
	case "shift+tab", "up":
		if m.urlFocusField > 0 {
			m.blurURLField(m.urlFocusField)
			m.urlFocusField--
			m.focusURLField(m.urlFocusField)
			return m, textinput.Blink
		}
	case "enter", "right":
		m.urlError = ""
		frontend := m.urlFrontend.Value()
		supabase := m.urlSupabase.Value()
		callback := m.urlCallback.Value()
		if callback == "" {
			callback = supabase
			m.urlCallback.SetValue(callback)
		}
		for _, u := range []string{frontend, supabase, callback} {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				m.urlError = "URLs must start with http:// or https://"
				return m, nil
			}
		}
		m.wizardValues["SITE_URL"] = frontend
		m.wizardValues["API_EXTERNAL_URL"] = supabase
		m.wizardValues["SUPABASE_PUBLIC_URL"] = supabase
		m.wizardValues["ADDITIONAL_REDIRECT_URLS"] = callback
		m.blurURLField(m.urlFocusField)
		return m.advanceWizard()
	case "left":
		m.blurURLField(m.urlFocusField)
		m.wizStep = stepDashboard
		m.dashFocusField = 0
		m.dashUser.Focus()
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	switch m.urlFocusField {
	case 0:
		m.urlFrontend, cmd = m.urlFrontend.Update(msg)
	case 1:
		m.urlSupabase, cmd = m.urlSupabase.Update(msg)
	case 2:
		m.urlCallback, cmd = m.urlCallback.Update(msg)
	}
	return m, cmd
}

func (m *StartupModel) blurURLField(idx int) {
	switch idx {
	case 0:
		m.urlFrontend.Blur()
	case 1:
		m.urlSupabase.Blur()
	case 2:
		m.urlCallback.Blur()
	}
}

func (m *StartupModel) focusURLField(idx int) {
	switch idx {
	case 0:
		m.urlFrontend.Focus()
	case 1:
		m.urlSupabase.Focus()
	case 2:
		m.urlCallback.Focus()
	}
}

func (m StartupModel) handleDatabaseKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter", "right":
		m.dbError = ""
		password := m.dbPass.Value()
		if password == "" {
			pw, _ := config.GenerateValue("POSTGRES_PASSWORD", m.wizardValues)
			password = pw
			m.dbPassGenerated = true
		} else if len(password) < 12 {
			m.dbError = "Password must be at least 12 characters"
			return m, nil
		}
		m.wizardValues["POSTGRES_PASSWORD"] = password
		m.dbPass.Blur()
		return m.advanceWizard()
	case "left":
		m.dbPass.Blur()
		m.wizStep = stepURLs
		m.urlFocusField = 0
		m.urlFrontend.Focus()
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.dbPass, cmd = m.dbPass.Update(msg)
	return m, cmd
}

func (m StartupModel) handlePortsKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	key := msg.String()

	// Fixing conflicts
	if len(m.portConflicts) > 0 && m.portFixIdx < len(m.portConflicts) {
		switch key {
		case "enter":
			val := m.portFixInput.Value()
			port, err := strconv.Atoi(val)
			if err != nil || port < 1 || port > 65535 {
				m.portFixError = "Enter a valid port number (1-65535)"
				return m, nil
			}
			if !isPortFree(port) {
				m.portFixError = fmt.Sprintf("Port %d is also in use", port)
				return m, nil
			}
			m.portFixError = ""
			idx := m.portConflicts[m.portFixIdx]
			m.portResults[idx].port = port
			m.portResults[idx].free = true
			m.portFixIdx++
			if m.portFixIdx >= len(m.portConflicts) {
				return m.advanceFromPorts()
			}
			nextIdx := m.portConflicts[m.portFixIdx]
			m.portFixInput.SetValue("")
			m.portFixInput.Placeholder = fmt.Sprintf("free port for %s", m.portResults[nextIdx].label)
			return m, nil
		}
		var cmd tea.Cmd
		m.portFixInput, cmd = m.portFixInput.Update(msg)
		return m, cmd
	}

	if m.portChecking {
		return m, nil
	}

	if m.portResults != nil && len(m.portConflicts) == 0 {
		return m, nil
	}

	if !m.portsCustom {
		switch key {
		case "up":
			m.portsChoice.Up()
		case "down":
			m.portsChoice.Down()
		case "enter":
			choice := m.portsChoice.Select()
			if choice == 0 {
				return m.startPortCheck()
			}
			m.portsCustom = true
			m.portFocus = 0
			m.portInputs[0].Focus()
			return m, textinput.Blink
		case "left":
			m.wizStep = stepDatabase
			m.dbPass.Focus()
			return m, textinput.Blink
		}
		return m, nil
	}

	// Custom port entry
	switch key {
	case "tab", "down":
		if m.portFocus < len(m.portInputs)-1 {
			m.portInputs[m.portFocus].Blur()
			m.portFocus++
			m.portInputs[m.portFocus].Focus()
			return m, textinput.Blink
		}
	case "shift+tab", "up":
		if m.portFocus > 0 {
			m.portInputs[m.portFocus].Blur()
			m.portFocus--
			m.portInputs[m.portFocus].Focus()
			return m, textinput.Blink
		}
	case "enter", "right":
		m.portError = ""
		for i, ti := range m.portInputs {
			val := ti.Value()
			if val == "" {
				val = m.portDefaults[i]
			}
			port, err := strconv.Atoi(val)
			if err != nil || port < 1 || port > 65535 {
				m.portError = fmt.Sprintf("Invalid port for %s: %s", m.portLabels[i], val)
				return m, nil
			}
			m.portInputs[i].SetValue(strconv.Itoa(port))
		}
		m.portInputs[m.portFocus].Blur()
		return m.startPortCheck()
	case "left":
		m.portsCustom = false
		m.portInputs[m.portFocus].Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.portInputs[m.portFocus], cmd = m.portInputs[m.portFocus].Update(msg)
	return m, cmd
}

func (m StartupModel) startPortCheck() (StartupModel, tea.Cmd) {
	m.portChecking = true
	m.portResults = nil
	m.portConflicts = nil

	entries := make([]portEntry, len(m.portKeys))
	for i := range m.portKeys {
		val := m.portInputs[i].Value()
		if val == "" {
			val = m.portDefaults[i]
		}
		port, _ := strconv.Atoi(val)
		entries[i] = portEntry{
			key:   m.portKeys[i],
			label: m.portLabels[i],
			port:  port,
		}
	}

	return m, func() tea.Msg {
		for i := range entries {
			entries[i].free = isPortFree(entries[i].port)
			entries[i].checked = true
		}
		return portCheckDone{results: entries}
	}
}

func (m StartupModel) advanceFromPorts() (StartupModel, tea.Cmd) {
	for _, r := range m.portResults {
		m.wizardValues[r.key] = strconv.Itoa(r.port)
	}
	return m.advanceWizard()
}

func (m StartupModel) handleStartupOptsKey(key string) (StartupModel, tea.Cmd) {
	switch key {
	case "up":
		m.seedChoice.Up()
	case "down":
		m.seedChoice.Down()
	case "enter", "right":
		choice := m.seedChoice.Select()
		if choice == 0 {
			m.wizardValues["SEED"] = "true"
		} else {
			m.wizardValues["SEED"] = "false"
		}
		pw, _ := config.GenerateValue("MINIO_ROOT_PASSWORD", m.wizardValues)
		m.wizardValues["MINIO_ROOT_PASSWORD"] = pw
		return m.advanceWizard()
	case "left":
		m.wizStep = stepPorts
		m.portsCustom = false
		m.portResults = nil
		m.portConflicts = nil
		m.portChecking = false
		return m, nil
	}
	return m, nil
}

func (m StartupModel) handleSecretsKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	key := msg.String()

	// Manual entry mode active
	if !m.secretsAuto && m.secretManualIdx > 0 && !m.secretManualDone {
		switch key {
		case "enter":
			val := m.secretManualInput.Value()
			if val != "" {
				m.wizardValues[m.secretManualKeys[m.secretManualIdx]] = val
			}
			m.secretManualIdx++
			if m.secretManualIdx >= len(m.secretManualKeys) {
				m.secretManualDone = true
				return m.advanceWizard()
			}
			m.secretManualInput.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.secretManualInput, cmd = m.secretManualInput.Update(msg)
		return m, cmd
	}

	// Manual entry — first key
	if !m.secretsAuto && m.secretManualIdx == 0 && len(m.secretManualKeys) > 0 && !m.secretManualDone {
		switch key {
		case "enter":
			val := m.secretManualInput.Value()
			if val != "" {
				m.wizardValues[m.secretManualKeys[0]] = val
			}
			m.secretManualIdx = 1
			if m.secretManualIdx >= len(m.secretManualKeys) {
				m.secretManualDone = true
				return m.advanceWizard()
			}
			m.secretManualInput.SetValue("")
			return m, nil
		}
		// Only if manual input is focused
		if m.secretManualInput.Focused() {
			var cmd tea.Cmd
			m.secretManualInput, cmd = m.secretManualInput.Update(msg)
			return m, cmd
		}
	}

	// Auto generation in progress
	if m.secretsAuto && !m.secretGenDone {
		return m, nil
	}

	// Choice
	switch key {
	case "up":
		m.secretsChoice.Up()
	case "down":
		m.secretsChoice.Down()
	case "enter", "right":
		choice := m.secretsChoice.Select()
		if choice == 0 {
			m.secretsAuto = true
			m.secretGenIdx = 0
			return m, m.generateAllSecrets()
		}
		m.secretsAuto = false
		m.secretManualKeys = m.secretGenList
		m.secretManualIdx = 0
		m.secretManualInput.SetValue("")
		m.secretManualInput.Focus()
		return m, textinput.Blink
	case "left":
		m.wizStep = stepStartupOpts
		return m, nil
	}
	return m, nil
}

func (m StartupModel) generateAllSecrets() tea.Cmd {
	vals := m.wizardValues
	genList := m.secretGenList

	return func() tea.Msg {
		for _, key := range genList {
			val, _ := config.GenerateValue(key, vals)
			vals[key] = val
		}
		return secretGenProgress{index: len(genList) - 1, done: true}
	}
}

func (m StartupModel) handleWritingKey(key string) (StartupModel, tea.Cmd) {
	if !m.writeDone {
		return m, nil
	}
	switch key {
	case "enter":
		m.phase = phaseComplete
		return m, func() tea.Msg { return NavigateToDashboardMsg{} }
	case "e", "E":
		if len(m.placeholders) > 0 {
			return m, func() tea.Msg { return OpenEnvEditorMsg{} }
		}
	}
	return m, nil
}

// advanceWizard moves to the next wizard step.
func (m StartupModel) advanceWizard() (StartupModel, tea.Cmd) {
	switch m.wizStep {
	case stepExistingEnv:
		m.wizStep = stepDashboard
		m.dashUser.Focus()
		return m, textinput.Blink
	case stepDashboard:
		m.wizStep = stepURLs
		m.urlFocusField = 0
		m.urlFrontend.Focus()
		return m, textinput.Blink
	case stepURLs:
		m.wizStep = stepDatabase
		m.dbPass.Focus()
		return m, textinput.Blink
	case stepDatabase:
		m.wizStep = stepPorts
		return m, nil
	case stepPorts:
		m.wizStep = stepStartupOpts
		return m, nil
	case stepStartupOpts:
		m.wizStep = stepSecrets
		return m, nil
	case stepSecrets:
		m.wizStep = stepWriting
		return m, m.writeEnvFile()
	}
	return m, nil
}

func (m *StartupModel) writeEnvFile() tea.Cmd {
	envPath := m.EnvPath
	examplePath := m.ExamplePath
	vals := m.wizardValues

	return func() tea.Msg {
		// Parse structure from .env.example
		sections, err := config.ParseEnvWithExample(examplePath, examplePath)
		if err != nil {
			return writeEnvDoneMsg{err: fmt.Sprintf("Failed to parse .env.example: %v", err)}
		}

		// Apply wizard values
		for si := range sections {
			for ii := range sections[si].Items {
				item := &sections[si].Items[ii]
				if item.Field != nil {
					if val, ok := vals[item.Field.Key]; ok {
						item.Field.Value = val
						item.Field.IsPlaceholder = false
					}
				}
			}
		}

		if err := config.WriteEnv(envPath, examplePath, sections); err != nil {
			return writeEnvDoneMsg{err: fmt.Sprintf("Failed to write .env: %v", err)}
		}

		placeholders := config.FindMissingFields(sections)
		return writeEnvDoneMsg{sections: sections, placeholders: placeholders}
	}
}

type writeEnvDoneMsg struct {
	err          string
	sections     []config.EnvSection
	placeholders []string
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m StartupModel) View() string {
	switch m.phase {
	case phaseHealthCheck:
		return m.viewHealthCheck()
	case phaseFatalError:
		return m.viewFatalError()
	case phaseWizard:
		return m.viewWizard()
	case phaseComplete:
		return m.viewComplete()
	}
	return ""
}

func (m StartupModel) viewHealthCheck() string {
	title := lipgloss.NewStyle().Foreground(startupColorBlue).Bold(true)
	muted := lipgloss.NewStyle().Foreground(startupColorMuted)
	subtle := lipgloss.NewStyle().Foreground(startupColorSubtle)
	yellow := lipgloss.NewStyle().Foreground(startupColorYellow)

	w := m.Width
	if w == 0 {
		w = 80
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(title.Render(" AgriSense "))
	filler := w - 12
	if filler < 0 {
		filler = 0
	}
	b.WriteString(subtle.Render(strings.Repeat("─", filler)))
	b.WriteString("\n\n")
	b.WriteString(muted.Render("  Checking project status..."))
	b.WriteString("\n\n")

	checks := []string{
		".env.example found",
		".env found",
		"Supabase config files present",
		"Docker is reachable",
		"Querying containers...",
	}
	for i, label := range checks {
		_ = i
		b.WriteString(yellow.Render("  ◌  " + label))
		b.WriteString("\n")
	}

	return b.String()
}

func (m StartupModel) viewFatalError() string {
	boxWidth := m.Width - 4
	if boxWidth > 60 {
		boxWidth = 60
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(startupColorRed).
		Padding(1, 2).
		Width(boxWidth)

	errStyle := lipgloss.NewStyle().Foreground(startupColorRed).Bold(true)
	muted := lipgloss.NewStyle().Foreground(startupColorMuted)
	hint := lipgloss.NewStyle().Foreground(startupColorSubtle)

	var b strings.Builder
	b.WriteString(errStyle.Render("✗  Cannot start"))
	b.WriteString("\n\n")
	b.WriteString(muted.Render(m.fatalMsg))
	b.WriteString("\n\n")
	b.WriteString(hint.Render("[L] Save log    [Q] Quit"))

	box := border.Render(b.String())
	lines := strings.Split(box, "\n")
	padTop := (m.Height - len(lines)) / 2
	if padTop < 0 {
		padTop = 0
	}
	return strings.Repeat("\n", padTop) + box
}

func (m StartupModel) viewComplete() string {
	muted := lipgloss.NewStyle().Foreground(startupColorMuted)
	return "\n" + muted.Render("  Launching dashboard...") + "\n"
}

func (m StartupModel) viewWizard() string {
	switch m.wizStep {
	case stepExistingEnv:
		return m.viewExistingEnv()
	case stepDashboard:
		return m.viewDashboard()
	case stepURLs:
		return m.viewURLs()
	case stepDatabase:
		return m.viewDatabase()
	case stepPorts:
		return m.viewPorts()
	case stepStartupOpts:
		return m.viewStartupOpts()
	case stepSecrets:
		return m.viewSecrets()
	case stepWriting:
		return m.viewWriting()
	}
	return ""
}

// ---------------------------------------------------------------------------
// Wizard step views
// ---------------------------------------------------------------------------

func (m StartupModel) wizardHeader(stepNum, total int, title string) string {
	subtle := lipgloss.NewStyle().Foreground(startupColorSubtle)
	titleStyle := lipgloss.NewStyle().Foreground(startupColorBlue).Bold(true)

	left := fmt.Sprintf("── Step %d / %d ", stepNum, total)
	right := fmt.Sprintf(" %s ──", title)
	w := m.Width
	if w == 0 {
		w = 80
	}
	filler := w - len(left) - len(right)
	if filler < 0 {
		filler = 0
	}
	return subtle.Render(left+strings.Repeat("─", filler)) + titleStyle.Render(right) + "\n\n"
}

func (m StartupModel) viewExistingEnv() string {
	boxWidth := m.Width - 4
	if boxWidth > 66 {
		boxWidth = 66
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(startupColorBlue).
		Padding(1, 2).
		Width(boxWidth)

	titleStyle := lipgloss.NewStyle().Foreground(startupColorBlue).Bold(true)
	text := lipgloss.NewStyle().Foreground(startupColorText)

	var b strings.Builder
	b.WriteString(titleStyle.Render("AgriSense Setup"))
	b.WriteString("\n\n")
	b.WriteString(text.Render("A .env file already exists."))
	b.WriteString("\n")
	b.WriteString(text.Render("What would you like to do?"))
	b.WriteString("\n\n")
	b.WriteString(m.existingChoice.View())

	box := border.Render(b.String())
	lines := strings.Split(box, "\n")
	padTop := (m.Height - len(lines)) / 2
	if padTop < 0 {
		padTop = 0
	}
	return strings.Repeat("\n", padTop) + box
}

func (m StartupModel) viewDashboard() string {
	var b strings.Builder
	b.WriteString(m.wizardHeader(1, 6, "Supabase Dashboard"))

	icon := lipgloss.NewStyle().Foreground(startupColorBlue)
	label := lipgloss.NewStyle().Foreground(startupColorText).Bold(true)
	sep := lipgloss.NewStyle().Foreground(startupColorSubtle)
	hint := lipgloss.NewStyle().Foreground(startupColorMuted)
	errStyle := lipgloss.NewStyle().Foreground(startupColorRed)

	b.WriteString(icon.Render("  🔐  "))
	b.WriteString(label.Render("Dashboard Credentials"))
	b.WriteString("\n")
	b.WriteString(sep.Render("  " + strings.Repeat("·", 50)))
	b.WriteString("\n")
	b.WriteString(hint.Render("  Access the Supabase Studio web UI with these credentials."))
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Username  (admin)  :  "))
	b.WriteString(m.dashUser.View())
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Password  (leave blank to auto-generate)  :  "))
	b.WriteString(m.dashPass.View())
	b.WriteString("\n")

	if m.dashPassGenerated {
		b.WriteString(lipgloss.NewStyle().Foreground(startupColorGreen).Render("  ✔ Password generated"))
		b.WriteString("\n")
	}
	if m.dashError != "" {
		b.WriteString(errStyle.Render("  " + m.dashError))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.navHint(!m.skipExisting))
	return b.String()
}

func (m StartupModel) viewURLs() string {
	var b strings.Builder
	b.WriteString(m.wizardHeader(2, 6, "Public URLs"))

	icon := lipgloss.NewStyle().Foreground(startupColorBlue)
	label := lipgloss.NewStyle().Foreground(startupColorText).Bold(true)
	sep := lipgloss.NewStyle().Foreground(startupColorSubtle)
	hint := lipgloss.NewStyle().Foreground(startupColorMuted)
	errStyle := lipgloss.NewStyle().Foreground(startupColorRed)

	b.WriteString(icon.Render("  🌐  "))
	b.WriteString(label.Render("Public URLs"))
	b.WriteString("\n")
	b.WriteString(sep.Render("  " + strings.Repeat("·", 50)))
	b.WriteString("\n")
	b.WriteString(hint.Render("  Keep defaults for local development."))
	b.WriteString("\n")
	b.WriteString(hint.Render("  Use your actual domain for production deployments."))
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Frontend URL        (http://localhost:3000)  :  "))
	b.WriteString(m.urlFrontend.View())
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Supabase URL        (http://localhost:8000)  :  "))
	b.WriteString(m.urlSupabase.View())
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Auth callback URL   (↑ same as Supabase URL) :  "))
	b.WriteString(m.urlCallback.View())
	b.WriteString("\n")

	if m.urlError != "" {
		b.WriteString(errStyle.Render("  " + m.urlError))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.navHint(true))
	return b.String()
}

func (m StartupModel) viewDatabase() string {
	var b strings.Builder
	b.WriteString(m.wizardHeader(3, 6, "Database"))

	icon := lipgloss.NewStyle().Foreground(startupColorBlue)
	label := lipgloss.NewStyle().Foreground(startupColorText).Bold(true)
	sep := lipgloss.NewStyle().Foreground(startupColorSubtle)
	hint := lipgloss.NewStyle().Foreground(startupColorMuted)
	errStyle := lipgloss.NewStyle().Foreground(startupColorRed)

	b.WriteString(icon.Render("  🗄️  "))
	b.WriteString(label.Render("PostgreSQL Password"))
	b.WriteString("\n")
	b.WriteString(sep.Render("  " + strings.Repeat("·", 50)))
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Password  (leave blank to auto-generate)  :  "))
	b.WriteString(m.dbPass.View())
	b.WriteString("\n")

	if m.dbPassGenerated {
		b.WriteString(lipgloss.NewStyle().Foreground(startupColorGreen).Render("  ✔ Password generated"))
		b.WriteString("\n")
	}
	if m.dbError != "" {
		b.WriteString(errStyle.Render("  " + m.dbError))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.navHint(true))
	return b.String()
}

func (m StartupModel) viewPorts() string {
	var b strings.Builder
	b.WriteString(m.wizardHeader(4, 6, "Application Ports"))

	icon := lipgloss.NewStyle().Foreground(startupColorBlue)
	label := lipgloss.NewStyle().Foreground(startupColorText).Bold(true)
	sep := lipgloss.NewStyle().Foreground(startupColorSubtle)
	hint := lipgloss.NewStyle().Foreground(startupColorMuted)
	errStyle := lipgloss.NewStyle().Foreground(startupColorRed)
	green := lipgloss.NewStyle().Foreground(startupColorGreen)
	red := lipgloss.NewStyle().Foreground(startupColorRed)

	b.WriteString(icon.Render("  🔌  "))
	b.WriteString(label.Render("Application Ports"))
	b.WriteString("\n")
	b.WriteString(sep.Render("  " + strings.Repeat("·", 50)))
	b.WriteString("\n\n")

	if m.portChecking {
		b.WriteString(hint.Render("  Checking ports..."))
		b.WriteString("\n")
		return b.String()
	}

	if m.portResults != nil {
		for _, r := range m.portResults {
			if r.free {
				b.WriteString(green.Render(fmt.Sprintf("  ✔  %s :%d is free", r.label, r.port)))
			} else {
				b.WriteString(red.Render(fmt.Sprintf("  ✗  %s :%d is in use", r.label, r.port)))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")

		if len(m.portConflicts) > 0 && m.portFixIdx < len(m.portConflicts) {
			idx := m.portConflicts[m.portFixIdx]
			r := m.portResults[idx]
			b.WriteString(hint.Render(fmt.Sprintf("  %s port is already in use. Enter a free port:  ", r.label)))
			b.WriteString(m.portFixInput.View())
			b.WriteString("\n")
			if m.portFixError != "" {
				b.WriteString(errStyle.Render("  " + m.portFixError))
				b.WriteString("\n")
			}
		}
		return b.String()
	}

	if m.portsCustom {
		for i := range m.portKeys {
			b.WriteString(hint.Render(fmt.Sprintf("  %-20s :  ", m.portLabels[i])))
			b.WriteString(m.portInputs[i].View())
			b.WriteString("\n")
		}
		if m.portError != "" {
			b.WriteString(errStyle.Render("  " + m.portError))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(m.navHint(true))
		return b.String()
	}

	b.WriteString(hint.Render("  Customise ports?"))
	b.WriteString("\n\n")
	b.WriteString(m.portsChoice.View())
	b.WriteString("\n")
	b.WriteString(m.navHint(true))
	return b.String()
}

func (m StartupModel) viewStartupOpts() string {
	var b strings.Builder
	b.WriteString(m.wizardHeader(5, 6, "Startup Options"))

	icon := lipgloss.NewStyle().Foreground(startupColorBlue)
	label := lipgloss.NewStyle().Foreground(startupColorText).Bold(true)
	sep := lipgloss.NewStyle().Foreground(startupColorSubtle)
	hint := lipgloss.NewStyle().Foreground(startupColorMuted)

	b.WriteString(icon.Render("  ⚙️  "))
	b.WriteString(label.Render("Options"))
	b.WriteString("\n")
	b.WriteString(sep.Render("  " + strings.Repeat("·", 50)))
	b.WriteString("\n\n")
	b.WriteString(hint.Render("  Seed the database on first container start?"))
	b.WriteString("\n\n")
	b.WriteString(m.seedChoice.View())
	b.WriteString("\n")
	b.WriteString(m.navHint(true))
	return b.String()
}

func (m StartupModel) viewSecrets() string {
	var b strings.Builder
	b.WriteString(m.wizardHeader(6, 6, "Secret Generation"))

	hint := lipgloss.NewStyle().Foreground(startupColorMuted)
	green := lipgloss.NewStyle().Foreground(startupColorGreen)
	text := lipgloss.NewStyle().Foreground(startupColorText)

	secretLabels := []string{
		"JWT signing secret",
		"Anon key (JWT)",
		"Service role key (JWT)",
		"Secret key base",
		"Vault encryption key",
		"PG meta crypto key",
		"Logflare public token",
		"Logflare private token",
		"S3 access key ID",
		"S3 access key secret",
	}

	if m.secretsAuto {
		if m.secretGenDone {
			b.WriteString(hint.Render("  Generating..."))
			b.WriteString("\n\n")
			for _, l := range secretLabels {
				b.WriteString(green.Render("  ✔  " + l))
				b.WriteString("\n")
			}
			b.WriteString("\n")
			b.WriteString(green.Render("  ✔  All secrets generated."))
			b.WriteString("\n")
			return b.String()
		}
		b.WriteString(hint.Render("  Generating..."))
		b.WriteString("\n\n")
		for i, l := range secretLabels {
			if i < m.secretGenIdx {
				b.WriteString(green.Render("  ✔  " + l))
			} else {
				b.WriteString(hint.Render("  ◌  " + l))
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	// Manual entry active
	if len(m.secretManualKeys) > 0 {
		b.WriteString(hint.Render("  Enter values for each secret (Enter to skip):"))
		b.WriteString("\n\n")
		for i, k := range m.secretManualKeys {
			if i < m.secretManualIdx {
				if _, ok := m.wizardValues[k]; ok {
					b.WriteString(green.Render("  ✔  " + k))
				} else {
					b.WriteString(hint.Render("  ·  " + k + " (placeholder)"))
				}
				b.WriteString("\n")
			} else if i == m.secretManualIdx && !m.secretManualDone {
				b.WriteString(text.Render("  " + k + ":  "))
				b.WriteString(m.secretManualInput.View())
				b.WriteString("\n")
			}
		}
		return b.String()
	}

	// Initial choice
	b.WriteString(hint.Render("  JWT signing key, Supabase anon/service tokens, S3 credentials,"))
	b.WriteString("\n")
	b.WriteString(hint.Render("  and encryption keys will be generated using cryptographically"))
	b.WriteString("\n")
	b.WriteString(hint.Render("  secure randomness."))
	b.WriteString("\n\n")
	b.WriteString(text.Render("  Auto-generate all secrets and keys? (strongly recommended)"))
	b.WriteString("\n\n")
	b.WriteString(m.secretsChoice.View())
	b.WriteString("\n")
	b.WriteString(m.navHint(true))
	return b.String()
}

func (m StartupModel) viewWriting() string {
	subtle := lipgloss.NewStyle().Foreground(startupColorSubtle)
	green := lipgloss.NewStyle().Foreground(startupColorGreen)
	warn := lipgloss.NewStyle().Foreground(startupColorYellow)
	hint := lipgloss.NewStyle().Foreground(startupColorMuted)
	errStyle := lipgloss.NewStyle().Foreground(startupColorRed)

	w := m.Width
	if w == 0 {
		w = 80
	}

	var b strings.Builder
	b.WriteString("\n")
	headerText := " SETUP COMPLETE "
	filler := (w - len(headerText)) / 2
	if filler < 0 {
		filler = 0
	}
	b.WriteString(subtle.Render(strings.Repeat("─", filler) + headerText + strings.Repeat("─", filler)))
	b.WriteString("\n\n")

	if !m.writeDone {
		b.WriteString(hint.Render("  Writing .env..."))
		b.WriteString("\n")
		return b.String()
	}

	if m.writeError != "" {
		b.WriteString(errStyle.Render("  ✗  " + m.writeError))
		b.WriteString("\n\n")
		b.WriteString(hint.Render("  Press Enter to continue →"))
		return b.String()
	}

	b.WriteString(green.Render(fmt.Sprintf("  ✔  .env written → %s", m.EnvPath)))
	b.WriteString("\n")
	b.WriteString(green.Render("  ✔  All variables configured."))
	b.WriteString("\n\n")

	if len(m.placeholders) > 0 {
		b.WriteString(warn.Render("  ⚠  The following variables still need values:"))
		b.WriteString("\n")
		for _, k := range m.placeholders {
			b.WriteString(warn.Render("     " + k + "  = <placeholder>"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hint.Render("  Edit .env before starting services.  [E] Open editor now  [Enter] Continue"))
		b.WriteString("\n")
	} else {
		b.WriteString(hint.Render("  Press Enter to continue →"))
		b.WriteString("\n")
	}
	return b.String()
}

func (m StartupModel) navHint(hasBack bool) string {
	hint := lipgloss.NewStyle().Foreground(startupColorSubtle)
	if hasBack {
		return hint.Render("  [Enter/→] Continue    [←] Back") + "\n"
	}
	return hint.Render("  [Enter/→] Continue") + "\n"
}

// ---------------------------------------------------------------------------
// Port checking
// ---------------------------------------------------------------------------

func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
