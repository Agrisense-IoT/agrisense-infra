package tui

import "github.com/charmbracelet/lipgloss"

// Foreground accent colors — no surface backgrounds (see AGENTS.md background invariant).
var (
	ColorText     = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6edf3"}
	ColorMuted    = lipgloss.AdaptiveColor{Light: "#6e6e6e", Dark: "#8b949e"}
	ColorSubtle   = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#484f58"}
	ColorGreen    = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	ColorYellow   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	ColorRed      = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	ColorBlue     = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	ColorCyan     = lipgloss.AdaptiveColor{Light: "#0598bc", Dark: "#39c5cf"}
	ColorMagenta  = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#bc8cff"}
	ColorOrange   = lipgloss.AdaptiveColor{Light: "#bc4c00", Dark: "#e3b341"}
	ColorSelectBg = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1d3a5e"}
	ColorBadgeBg  = lipgloss.AdaptiveColor{Light: "#e6f4ea", Dark: "#1a3a2a"}
)

// PanelBorder is the default unfocused rounded border style.
var PanelBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorSubtle)

// FocusedBorder is the highlighted rounded border used when a pane has focus.
var FocusedBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue)
