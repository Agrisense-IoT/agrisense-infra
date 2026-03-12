package tui

import (
	"time"

	"agrisense/internal/config"
	"agrisense/internal/runner"
)

// FocusZone identifies which pane of the dashboard has keyboard focus.
type FocusZone int

const (
	FocusLeft  FocusZone = iota
	FocusRight
)

// Navigation messages
type NavigateTo struct{ View ViewID }
type GoBack struct{}

// Runner — service management results
type ServicesRefreshed struct{ Services []runner.ServiceStatus }
type ActionLineReceived struct{ Line string }
type ActionCompleted struct{ ExitCode int }

// Inline log stream (dashboard right panel)
type InlineLogLine struct{ Line string }
type InlineLogStopped struct{}

// Full-screen log view
type FullscreenLogLine struct{ Line string }

// Focus
type FocusChanged struct{ Zone FocusZone }

// .env
type EnvLoaded struct{ Sections []config.EnvSection }
type EnvSaved struct{}

// Startup
type StartupCheckDone struct {
	NeedsWizard  bool
	MissingFiles []string
}
type WizardComplete struct{}

// Simulation
type SimulationStarted struct {
	Mode    string
	Devices int
}

// Ticker
type TickMsg struct{ Time time.Time }
