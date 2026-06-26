# Interactive Shell (Dashboard MVP) — Design

**Date:** 2026-06-26
**Status:** Approved (brainstorming) — ready for implementation plan
**Slice:** First interactive slice. Dashboard + in-app Tidy (clean) and Restore.

## Why

CodexSSD must be usable by *anyone*, the way you just type `claude` and a screen
appears. Today it is a multi-subcommand CLI — fine for power users and scripts,
but it asks a non-technical person to learn `status` / `clean --yes` / `restore`.
The product's promise ("notice deadweight on your behalf, ask before acting")
lands far better as a single interactive screen than as a command cheat-sheet.

This slice makes **`codexssd` (typed alone) the product**: an interactive app
that shows the current state, flags Codex deadweight, and offers safe actions —
without the user needing to know any subcommand.

## Product shape & entry point

- **`codexssd`** with no arguments → launches the interactive app (the TUI).
  Today no-args prints usage and exits 2; this changes to launch the app.
- **`codexssd <subcommand>`** → unchanged. `status`, `clean`, `restore`,
  `install-agent`, `self`, `watch`, `help` remain the **engine + automation
  surface**: used by scripts, power users, and our tests. The app invokes the
  same engine functions (`internal/codex`, `internal/cleaner`).
- `codexssd help` / `-h` still prints command help, with a note that bare
  `codexssd` opens the app.

The subcommands are not deprecated — they are the engine; the TUI is the face.

## Scope

### In scope (this slice)

A dashboard the user opens that, on launch:

- shows the Codex directory, each known log file + size, and the total;
- shows whether Codex appears to be running;
- shows how many recoverable backups exist and the most recent tidy (from
  backup manifests);
- flags deadweight in plain language when the total is worth tidying — defined
  as total log bytes >= a `deadweightThreshold` constant (default **100 MiB**),
  living in `internal/tui` and tunable later; below it the dashboard still shows
  the size but without the "worth tidying" emphasis;
- offers in-app **Tidy** (`c`) and **Restore** (`r`), each with a confirmation;
- offers quit (`q`) and a help overlay (`?`).

### Out of scope (explicitly deferred to later slices)

- **Background watching / auto-popups / polling** — the idle loop that notices
  deadweight while you are away. Next slice. This slice "notices" each time you
  open it, which already delivers much of the feel.
- **`install-agent` as an in-app action** — later slice (engine design already
  brainstormed).
- **`self` footprint** — its own slice. See decision below.

### Resolved decisions

- **Footprint deferred.** Instead of half-building `self`, the dashboard shows
  recoverable backups / last tidy, which speaks more directly to deadweight.
  A real footprint line arrives with the dedicated `self` slice.
- **Bare `codexssd` launches the app.** Subcommands still work unchanged.

## Architecture — `internal/tui`

Bubble Tea (the Elm architecture: `Model` / `Update` / `View`).

- **Model** holds: the loaded `codex.LogReport`; `codexRunning bool` and whether
  the platform supports the check; the list of `cleaner.Backup`; the current
  screen; a transient status/error line; and the selected backup index for the
  restore list.
- **Update** is a pure function `(Model, tea.Msg) -> (Model, tea.Cmd)`. All
  decisions (which key does what, which transition is allowed) live here, which
  is what makes the app unit-testable.
- **View** renders the current screen from the Model (Lip Gloss for layout).
- **Engine work runs as async `tea.Cmd`s** so the UI never blocks. Each command
  calls an engine function and returns a typed message:
  - `loadCmd` → `codex.ScanLogs` + `codex.IsCodexRunning` + `cleaner.ListBackups`
    → `loadedMsg{report, running, supported, backups}`
  - `recheckRunningCmd` → `codex.IsCodexRunning` → `runningMsg{running, supported, err}`
  - `cleanCmd` → `cleaner.PlanCodexLogs` + `Plan.Apply(time.Now())` →
    `cleanResultMsg{dest, movedBytes, err}`
  - `restoreCmd(dir)` → `cleaner.Restore(dir)` → `restoreResultMsg{id, err}`
- `func tui.Run() error` builds the initial model and runs
  `tea.NewProgram(model, tea.WithAltScreen()).Run()`. `main` calls it when no
  subcommand is given.

### Screens (states) and key bindings

| State | Shows | Keys |
| --- | --- | --- |
| `dashboard` | status, running?, backups/last tidy, deadweight flag | `c` tidy · `r` restore · `?` help · `q` quit |
| `confirmClean` | "Move <size> of Codex logs aside?" | `y` confirm · `n`/`esc` cancel |
| `cleaning` | spinner | (in progress) |
| `result` | outcome (moved to <dest> / error) | `enter`/`esc` back to dashboard |
| `restoreList` | recoverable backups | `↑/↓` select · `enter` choose · `esc` back |
| `confirmRestore` | "Restore backup <id>?" | `y` confirm · `n`/`esc` cancel |
| `restoring` | spinner | (in progress) |
| `blocked` | "Codex is running — close it first" | `enter`/`esc` back |
| `error` | error detail | `enter`/`esc` back |
| help overlay | key reference | `?`/`esc` close |

## Safety (reuses the proven, mutation-tested engine)

The TUI is a thin orchestration layer over already-safe code; it adds no new
file-mutating logic.

- **Fresh running-check before every action.** Tidy and Restore each dispatch
  `recheckRunningCmd` first (the user may have opened Codex after the dashboard
  loaded). If Codex is running, or the platform can't be checked, the app goes
  to `blocked` and **never calls the engine**. Same gate as the CLI.
- **Tidy** → `cleaner.PlanCodexLogs` + `Plan.Apply` (move-only via `os.Rename`,
  allow-list `isCodexLog`, rollback on partial failure). Never deletes.
- **Restore** → `cleaner.Restore` (pre-flight overwrite-refusal, allow-list,
  rollback). Never clobbers a live log.
- Confirmation screens mean no destructive-looking action happens on a single
  keystroke.

## Dependency decision

This slice introduces CodexSSD's first third-party dependencies:
`github.com/charmbracelet/bubbletea` (+ `lipgloss`, `bubbles`).

- **Distribution promise intact:** Go links these statically — CodexSSD remains
  a single binary you just run. Nothing for the user to install.
- **Containment:** the charmbracelet family is allowed **only in
  `internal/tui`**. The engine packages (`internal/codex`, `internal/cleaner`,
  and future `internal/monitor`/`agent`/`self`) stay **standard-library only**.
- `docs/stack.md` and `CLAUDE.md` are updated to record this relaxation and its
  boundary. `go.sum` is committed; CI (`go test ./...`) is unchanged.

## Testing

Bubble Tea's `Update` is a pure function, so the app is tested by feeding it
messages and asserting state transitions — no terminal or real Codex needed:

- `loadedMsg` populates the dashboard (status, running, backups).
- `c` with Codex **not** running and a non-empty plan → `confirmClean`; `y` →
  dispatches cleaning → `cleanResultMsg` → `result`.
- **`c` while Codex running → `blocked`; the engine is never invoked.**
- Restore: `r` → `restoreList`; select + `enter` → `confirmRestore`; `y` while
  running → `blocked`; while not running → restoring → `result`.
- `q` quits; `?` toggles help.

The engine packages already have unit + integration + mutation-tested coverage;
the TUI tests focus only on the orchestration/transition logic.

## Files

- `internal/tui/model.go` — `Model`, states, `Run()`, initial model + `Init`.
- `internal/tui/update.go` — `Update` (key handling, transitions, msg handling).
- `internal/tui/view.go` — `View` and per-screen rendering.
- `internal/tui/commands.go` — `tea.Cmd`s wrapping engine calls + message types.
- `internal/tui/*_test.go` — transition tests.
- `cmd/codexssd/main.go` — no-args → `tui.Run()`; keep subcommand dispatch.
- `go.mod` / `go.sum` — add charmbracelet deps.
- `docs/stack.md`, `CLAUDE.md` — record the dependency boundary.

## Future (context, not this slice)

- **Background watch loop** (`internal/monitor`): low-write polling of `~/.codex`,
  thresholds, auto-popup when deadweight crosses a limit while idle, quiet while
  Codex is active. This turns the dashboard into the "sits there and notices"
  experience.
- **`install-agent` action** and **`self` footprint** surfaced in the app.
