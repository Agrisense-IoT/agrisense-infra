package tui

import "github.com/charmbracelet/bubbles/key"

// DashboardSidebarKeyMap defines keys for the dashboard sidebar (left focus).
type DashboardSidebarKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Tab     key.Binding
	Start   key.Binding
	Restart key.Binding
	Build   key.Binding
	Stop    key.Binding
	Destroy key.Binding
	Testing key.Binding
	Env     key.Binding
	Quit    key.Binding
	Help    key.Binding
}

// DashboardLogKeyMap defines keys for the dashboard log panel (right focus).
type DashboardLogKeyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Home       key.Binding
	End        key.Binding
	Tab        key.Binding
	Esc        key.Binding
	Fullscreen key.Binding
}

// ActionKeyMap defines keys for the action view.
type ActionKeyMap struct {
	Esc      key.Binding
	Stop     key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
}

// LogsKeyMap defines keys for the full-screen log view.
type LogsKeyMap struct {
	Esc      key.Binding
	Fullscreen key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
}

// EnvEditorKeyMap defines keys for the env editor.
type EnvEditorKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Enter    key.Binding
	Generate key.Binding
	Save     key.Binding
	SaveRestart key.Binding
	SaveRebuild key.Binding
	Esc      key.Binding
}

// PopupKeyMap defines keys for the popup overlay.
type PopupKeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
	Enter key.Binding
	Esc   key.Binding
}

// KeyMap groups all key maps.
var KeyMap = struct {
	Sidebar DashboardSidebarKeyMap
	LogPanel DashboardLogKeyMap
	Action  ActionKeyMap
	Logs    LogsKeyMap
	EnvEditor EnvEditorKeyMap
	Popup   PopupKeyMap
}{
	Sidebar: DashboardSidebarKeyMap{
		Up:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "navigate")),
		Down:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "navigate")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
		Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus log panel")),
		Start:   key.NewBinding(key.WithKeys("s", "S"), key.WithHelp("s", "start all")),
		Restart: key.NewBinding(key.WithKeys("r", "R"), key.WithHelp("r", "restart all")),
		Build:   key.NewBinding(key.WithKeys("b", "B"), key.WithHelp("b", "build all")),
		Stop:    key.NewBinding(key.WithKeys("x", "X"), key.WithHelp("x", "stop all")),
		Destroy: key.NewBinding(key.WithKeys("d", "D"), key.WithHelp("d", "destroy")),
		Testing: key.NewBinding(key.WithKeys("t", "T"), key.WithHelp("t", "testing")),
		Env:     key.NewBinding(key.WithKeys("e", "E"), key.WithHelp("e", ".env")),
		Quit:    key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("q", "quit")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	},
	LogPanel: DashboardLogKeyMap{
		Up:         key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "scroll")),
		Down:       key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "scroll")),
		PageUp:     key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Home:       key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		End:        key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
		Tab:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "sidebar")),
		Esc:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "sidebar")),
		Fullscreen: key.NewBinding(key.WithKeys("f", "F"), key.WithHelp("f", "full-screen logs")),
	},
	Action: ActionKeyMap{
		Esc:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Stop:     key.NewBinding(key.WithKeys("s", "S"), key.WithHelp("s", "stop")),
		Up:       key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "scroll")),
		Down:     key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "scroll")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		End:      key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
	},
	Logs: LogsKeyMap{
		Esc:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Fullscreen: key.NewBinding(key.WithKeys("f", "F"), key.WithHelp("f", "back")),
		Up:         key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "scroll")),
		Down:       key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "scroll")),
		PageUp:     key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Home:       key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		End:        key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
	},
	EnvEditor: EnvEditorKeyMap{
		Up:          key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "navigate")),
		Down:        key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "navigate")),
		PageUp:      key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:    key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Enter:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
		Generate:    key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g", "generate")),
		Save:        key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		SaveRestart: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "save & restart")),
		SaveRebuild: key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "save & rebuild")),
		Esc:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "save/discard")),
	},
	Popup: PopupKeyMap{
		Up:    key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "navigate")),
		Down:  key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "navigate")),
		Left:  key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "navigate")),
		Right: key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "navigate")),
		Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Esc:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	},
}
