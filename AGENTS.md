# AGENTS.md — Agrisense TUI Infrastructure (v1.3.1)

This file covers: **shared concepts referenced across the entire codebase**, things you
must not do, known traps, and invariants. Every session must read this file first.

For the task schedule (what to implement in each session and in what order), read
**`00_SESSIONS.md`**. The plan files below contain the full specifications —
`00_SESSIONS.md` tells you which ones to load for any given session.

---

## Plan file index

| File                     | Contents                                                                                                               |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `00_SESSIONS.md`         | **Session schedule — read this to know what to do and which files to load**                                            |
| `01_OVERVIEW.md`         | Project context, requirements, directory layout, go.mod, Makefile, build                                               |
| `02_STYLES_AND_KEYS.md`  | Color palette, lipgloss styles, border rules, state glyphs, keybinding defs                                            |
| `03_STATE_AND_RUNNER.md` | Root model (app.go), ViewID enum, Runner interface, profiles, DockerComposeRunner                                      |
| `04_MESSAGES.md`         | All bubbletea Msg types (messages.go), FocusZone, inline-log lifecycle                                                 |
| `05_CONFIG.md`           | config/env.go data types + functions, config/secrets.go, GeneratableKeys                                               |
| `06_SCAFFOLDING_CLI.md`  | main.go structure, CLI mode (§10), session-1 scaffolding steps                                                         |
| `07_VIEW_STARTUP.md`     | views/startup.go — health check phase + full setup wizard (all 7 steps)                                                |
| `08_VIEW_DASHBOARD.md`   | views/dashboard.go — split-pane bento layout, sidebar, log panel, details, focus model                                 |
| `09a_VIEW_ACTION.md`     | views/action.go — streaming command output, pull-progress bar, spinner                                                 |
| `09b_VIEW_LOGS.md`       | views/logs.go — full-screen log viewer, auto-tail, line colour filtering                                               |
| `10_VIEW_POPUP.md`       | views/popup.go — generic overlay, all 8 pre-built popup variants                                                       |
| `11_VIEW_ENV_EDITOR.md`  | views/env_editor.go — full-screen .env editor, G-generate, Ctrl+S/R/B                                                  |
| `12_VIEW_SIMULATION.md`  | views/simulation.go, internal/mqtt/simulator.go — single + swarm MQTT simulator                                        |
| `13_IMPL_NOTES.md`       | §8.4 startup wizard impl notes, §8.5 help overlay, §9 URL construction, §11 errors, §14 critical notes for Claude Code |

---

## Shared concepts (used by 4+ files — canonical definitions live here)

### ViewID enum

Defined in `internal/tui/app.go`. Every view file imports and uses these constants.

```go
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
```

### Color constants

Defined in `internal/tui/styles.go`. Import from there — **never redefine in a view file**.

```go
ColorText     = lipgloss.AdaptiveColor{Light: "#1a1a1a",  Dark: "#e6edf3"}
ColorMuted    = lipgloss.AdaptiveColor{Light: "#6e6e6e",  Dark: "#8b949e"}
ColorSubtle   = lipgloss.AdaptiveColor{Light: "#999999",  Dark: "#484f58"}
ColorGreen    = lipgloss.AdaptiveColor{Light: "#1a7f37",  Dark: "#3fb950"}
ColorYellow   = lipgloss.AdaptiveColor{Light: "#9a6700",  Dark: "#d29922"}
ColorRed      = lipgloss.AdaptiveColor{Light: "#cf222e",  Dark: "#f85149"}
ColorBlue     = lipgloss.AdaptiveColor{Light: "#0969da",  Dark: "#58a6ff"}
ColorCyan     = lipgloss.AdaptiveColor{Light: "#0598bc",  Dark: "#39c5cf"}
ColorMagenta  = lipgloss.AdaptiveColor{Light: "#8250df",  Dark: "#bc8cff"}
ColorOrange   = lipgloss.AdaptiveColor{Light: "#bc4c00",  Dark: "#e3b341"}
ColorSelectBg = lipgloss.AdaptiveColor{Light: "#dbeafe",  Dark: "#1d3a5e"}
ColorBadgeBg  = lipgloss.AdaptiveColor{Light: "#e6f4ea",  Dark: "#1a3a2a"}
```

### State indicator glyphs

Used in dashboard sidebar, details pane, health check screen, and action view:

```
● ColorGreen   = running
◌ ColorYellow  = restarting
✗ ColorRed     = exited / error
· ColorSubtle  = created / unknown
```

### Runner interface types

Defined in `internal/runner/runner.go`. Imported by app.go, dashboard, action, logs, simulation views.

```go
type ServiceStatus struct {
    Name    string  // display name
    Service string  // compose service name
    State   string  // "running" | "stopped" | "restarting" | "error" | "unknown"
    Status  string  // human-readable, e.g. "Up 2 hours"
    Health  string  // "healthy" | "unhealthy" | ""
}

type CommandStream struct {
    Lines    <-chan string
    ExitCode <-chan int
}

type DestroyMode int
const (
    DestroyContainersOnly        DestroyMode = iota
    DestroyContainersAndVolumes
    DestroyContainersVolumesImages
    DestroyPrune
)
```

### Key bubbletea rules (apply everywhere)

- **Never block in `Update()`**. All async work (docker commands, log streaming, container
  refresh) must go through `tea.Cmd`.
- **Never use `fmt.Println` in TUI mode** — it breaks the alternate screen. Only use it
  in CLI mode (`main.go` subcommand path).
- **Window resize:** handle `tea.WindowSizeMsg` in the root model's `Update()`. Propagate
  `width` and `height` to all sub-models.
- **Popup overlay rendering:** in the root model's `View()`, render the background view
  first, then overlay the popup box using string line manipulation (split → overwrite
  center region → rejoin).

### File path conventions

These are the three canonical paths. All code derives them from `scriptDir`.

```go
scriptDir   // agrisense-infra/ absolute path  (detected via os.Executable walk)
envPath     = filepath.Join(scriptDir, ".env")
examplePath = filepath.Join(scriptDir, ".env.example")
composePath = filepath.Join(scriptDir, "docker-compose.yml")
```

`scriptDir` detection in `main.go`: use `os.Executable()`, walk up the tree until
`docker-compose.yml` is found. Fall back to `filepath.Abs(".")`.

---

## Do not touch these

- `supabase/volumes/db/data/` and `supabase/volumes/storage/` — live bind-mounts.
  Do not read, write, or delete unless the task is explicitly a destroy operation
  **and the user has confirmed**.
- `.env` — contains real secrets. Never log, print, or commit its contents. When
  displaying values for fields matching `PASSWORD|SECRET|KEY|TOKEN`, show only the
  first 4 characters followed by `****`.
- `supabase/docker-compose.yml` and files under `supabase/volumes/api/` and
  `supabase/volumes/db/` — pulled from upstream Supabase; treat as stable. Do not
  modify without an explicit task.

## Do not run these mid-task

- `docker compose up` / `docker compose down` / `docker compose restart` — disrupts
  the running stack. Only run if the task explicitly requires it and the user has confirmed.
- `docker builder prune` or any `prune` variant — only when implementing the Prune
  destroy mode.

---

## Traps

### JWT generation order — affects secrets.go, env_editor.go, startup wizard

`ANON_KEY` and `SERVICE_ROLE_KEY` are HS256 JWTs signed with `JWT_SECRET`. The rule:

> When generating `ANON_KEY` or `SERVICE_ROLE_KEY`, if `JWT_SECRET` is empty or still
> a placeholder, **generate `JWT_SECRET` first**, then sign the JWT with it, then update
> **both** fields in memory. Generating the JWT against a placeholder produces a
> valid-looking but cryptographically useless token that will silently break Supabase auth.

This applies to: single-field `G` key in env editor, Generate All (Ctrl+A) in env editor,
the §7.5 wizard's "generate everything" step, and any programmatic call to
`secrets.GenerateValue`.

### Inline log process leak — affects dashboard.go

The dashboard keeps one `docker logs -f` process alive for the selected container.
When selection changes, cancel the old process's `context.CancelFunc` **before** starting
the new one. If you forget, old and new log output interleave and the old process leaks
indefinitely.

### Supabase bind-mount cleanup — affects docker.go destroy modes

`docker compose down -v` removes named Docker volumes but does **not** touch bind-mounts.
Destroy modes that include volumes must also delete the _contents_ (not the directories) of:

- `supabase/volumes/db/data/`
- `supabase/volumes/storage/`

The directories themselves must survive as mount targets when the stack restarts.

---

## Invariant: terminal background

The TUI **never** sets a background color on any surface that spans the full terminal
width or height. Panels, header line, footer line, and the container list all use
`lipgloss.NoColor` for background so the UI works on dark and light terminal themes.

**Permitted background colors only:**

- The selected-row highlight (`ColorSelectBg`)
- Popup / overlay boxes
- Inline badges

If something looks wrong on a dark terminal, fix the foreground color — not by adding
a background.
