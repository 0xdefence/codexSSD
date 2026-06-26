# Interactive Shell (Dashboard MVP) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make bare `codexssd` launch an interactive Bubble Tea dashboard that shows Codex log status, flags deadweight, and offers in-app Tidy (clean) and Restore over the existing safety-tested engine.

**Architecture:** A new `internal/tui` package implements the Elm-style Bubble Tea `Model`/`Update`/`View`. `Update` is a pure function (the testable core); all engine work runs as async `tea.Cmd`s through package-level function seams (so the running-gate and actions are hermetically testable). The TUI adds no file-mutating logic — it calls `internal/codex` and `internal/cleaner`, which are already unit + mutation tested.

**Tech Stack:** Go 1.25; `github.com/charmbracelet/bubbletea` (v1.3.x) + `github.com/charmbracelet/lipgloss` (v1.x). These are used ONLY in `internal/tui`.

## Global Constraints

Copied from `docs/superpowers/specs/2026-06-26-interactive-shell-design.md`. Every task implicitly includes these:

- **Bare `codexssd` (no args) launches the TUI.** Subcommands (`status`/`clean`/`restore`/`install-agent`/`self`/`watch`/`help`) remain unchanged.
- **Dependency boundary:** the charmbracelet libraries may be imported ONLY in `internal/tui`. The engine packages (`internal/codex`, `internal/cleaner`, and any future `internal/*`) stay **standard-library only**. Still one static binary.
- **No new file-mutating logic in the TUI.** Tidy → `cleaner.PlanCodexLogs` + `Plan.Apply`; Restore → `cleaner.Restore`. Move-aside only, never delete.
- **Fresh running-check before every action.** Tidy and Restore each re-check `codex.IsCodexRunning` inside their command; if Codex is running, the platform is unsupported, or the check errors, the engine is NOT invoked and the app shows a blocked screen.
- **Deadweight threshold:** `deadweightThreshold` constant in `internal/tui`, default **100 MiB** (`100 * 1024 * 1024`).
- **`bubbles` (spinner) is NOT added in this slice** — use a static "Working…" line (YAGNI). Only `bubbletea` + `lipgloss`.
- **Naming:** product **CodexSSD**; module/binary/command lowercase `codexssd`; module path `github.com/0xdefence/codexssd`.
- **Verification gate:** `go build ./... && go vet ./... && go test ./...` green and `gofmt -l .` empty before each commit. The TUI program (`tui.Run`) is never started from tests.

---

## File Structure

- `internal/tui/model.go` — **create.** `Model`, `state` enum, all model fields, message types, `New()`, `Init()`, `Run()`, `deadweightThreshold`.
- `internal/tui/commands.go` — **create.** Engine seams (function vars) + `tea.Cmd`s (`loadCmd`, `cleanCmd`, `restoreCmd`).
- `internal/tui/update.go` — **create.** `Update` — key handling + message handling + transitions.
- `internal/tui/view.go` — **create.** `View` + per-state renderers.
- `internal/tui/*_test.go` — **create.** Transition + command tests.
- `cmd/codexssd/main.go` — **modify.** No-args → `tui.Run()`.
- `go.mod` / `go.sum` — **modify.** Add charmbracelet deps.
- `docs/stack.md`, `CLAUDE.md` — **modify.** Record the dependency boundary.

Engine API the TUI consumes (already implemented):
- `codex.Dir() (string, error)`
- `codex.ScanLogs(dir string) codex.LogReport` where `LogReport{CodexDir string; DirExists bool; Files []LogFile; TotalBytes int64}` and `LogFile{Name, Path string; Exists bool; Size int64}`
- `codex.IsCodexRunning() (bool, error)`, `codex.ErrUnsupportedPlatform`
- `codex.HumanBytes(n int64) string`
- `cleaner.PlanCodexLogs(dir string) (cleaner.Plan, error)`, `Plan{CodexDir, BackupRoot string; Items []PlanItem; TotalBytes int64}`, `Plan.Empty() bool`, `Plan.Apply(now time.Time) (string, error)`
- `cleaner.ListBackups(dir string) ([]cleaner.Backup, error)`, `Backup{Dir string; Manifest Manifest}`, `Manifest{MovedAt time.Time; HoldUntil time.Time; Items []ManifestItem}`
- `cleaner.Restore(backupDir string) error`

---

## Task 1: TUI skeleton, dependencies, and entry point

**Files:**
- Create: `internal/tui/model.go`, `internal/tui/update.go`, `internal/tui/view.go`
- Create: `internal/tui/update_test.go`
- Modify: `cmd/codexssd/main.go`, `go.mod`, `go.sum`, `docs/stack.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: nothing (skeleton). Engine field types imported for later tasks.
- Produces:
  - `tui.Run() error`
  - `tui.New() Model`, `Model` with fields: `state state`, `showHelp bool`, `width int`, `report codex.LogReport`, `running bool`, `supported bool`, `runErr error`, `loadErr error`, `plan cleaner.Plan`, `backups []cleaner.Backup`, `selected int`, `resultMsg string`, `resultErr error`, `blockedReason string`
  - `state` constants: `stateDashboard, stateConfirmClean, stateCleaning, stateRestoreList, stateConfirmRestore, stateRestoring, stateResult, stateBlocked, stateError`
  - `const deadweightThreshold int64 = 100 * 1024 * 1024`

- [ ] **Step 1: Add dependencies**

Run:
```bash
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
```
Expected: `go.mod` gains `require` lines for both; `go.sum` populated.

- [ ] **Step 2: Write the failing test**

Create `internal/tui/update_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key builds a KeyMsg for a single key like "q", "?", "enter", "esc", "up".
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// step sends one message and returns the updated Model and any command.
func step(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := step(New(), key(k))
		if cmd == nil {
			t.Fatalf("%q produced no command, want quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%q did not produce tea.QuitMsg", k)
		}
	}
}

func TestHelpToggle(t *testing.T) {
	m := New()
	if m.showHelp {
		t.Fatal("help should start hidden")
	}
	m, _ = step(m, key("?"))
	if !m.showHelp {
		t.Error("? did not show help")
	}
	m, _ = step(m, key("?"))
	if m.showHelp {
		t.Error("? did not hide help again")
	}
}

func TestWindowSizeStored(t *testing.T) {
	m, _ := step(New(), tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.width != 80 {
		t.Errorf("width = %d, want 80", m.width)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tui/ -v`
Expected: FAIL — `New`, `Model`, `Update` undefined (build error).

- [ ] **Step 4: Create model.go**

Create `internal/tui/model.go`:

```go
// Package tui is CodexSSD's interactive app: `codexssd` with no subcommand opens
// this screen. It is a thin Bubble Tea layer over the safety-tested engine
// packages (internal/codex, internal/cleaner) — it adds no file-mutating logic
// of its own.
//
// DEPENDENCY BOUNDARY: the charmbracelet libraries are imported ONLY in this
// package. The engine packages remain standard-library only.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
)

// deadweightThreshold is the total Codex-log size at or above which the
// dashboard emphasizes that tidying is worthwhile.
const deadweightThreshold int64 = 100 * 1024 * 1024 // 100 MiB

// state is the current screen.
type state int

const (
	stateDashboard state = iota
	stateConfirmClean
	stateCleaning
	stateRestoreList
	stateConfirmRestore
	stateRestoring
	stateResult
	stateBlocked
	stateError
)

// Model is the whole application state. Fields beyond the skeleton are populated
// by later tasks (load, clean, restore); unused-but-declared fields are fine.
type Model struct {
	state    state
	showHelp bool
	width    int

	// status (populated by loadCmd in Task 2)
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup

	// interaction state
	selected      int    // restore list cursor
	resultMsg     string // success text on the result screen
	resultErr     error  // error on the result screen
	blockedReason string // why an action was refused
}

// New returns the initial model.
func New() Model {
	return Model{state: stateDashboard}
}

// Init implements tea.Model. (Task 2 returns loadCmd here.)
func (m Model) Init() tea.Cmd {
	return nil
}

// Run launches the interactive app. Called by main when no subcommand is given.
// Never called from tests.
func Run() error {
	_, err := tea.NewProgram(New(), tea.WithAltScreen()).Run()
	return err
}
```

- [ ] **Step 5: Create update.go**

Create `internal/tui/update.go`:

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// Update implements tea.Model. It is a pure function over (Model, Msg) and is
// the testable core of the app.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a keypress. Global keys first, then per-state keys (added in
// later tasks).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	}
	return m, nil
}
```

- [ ] **Step 6: Create view.go**

Create `internal/tui/view.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

// View implements tea.Model.
func (m Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Starting…")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, m.footer())
	return b.String()
}

func (m Model) footer() string {
	return "c tidy · r restore · ? help · q quit"
}

func (m Model) renderHelp() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD — help"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "c    tidy Codex's logs aside (recoverable)")
	fmt.Fprintln(&b, "r    restore previously tidied logs")
	fmt.Fprintln(&b, "?    toggle this help")
	fmt.Fprintln(&b, "q    quit")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "press ? or esc to close")
	return b.String()
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (`TestQuitKeys`, `TestHelpToggle`, `TestWindowSizeStored`).

- [ ] **Step 8: Wire the entry point**

In `cmd/codexssd/main.go`, add the import:

```go
	"github.com/0xdefence/codexssd/internal/tui"
```

Replace the no-args branch in `run` (currently prints usage and returns 2):

```go
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
```

with:

```go
	if len(args) == 0 {
		if err := tui.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
			return 1
		}
		return 0
	}
```

- [ ] **Step 9: Update docs for the dependency boundary**

In `docs/stack.md`, under "One deliberate non-choice: no database" (or at the end of the summary), add a short subsection:

```markdown
## One allowed dependency family: the interactive UI

CodexSSD's interactive app (`codexssd` with no subcommand) is built with the
charmbracelet TUI libraries (Bubble Tea, Lip Gloss). These are the only
third-party dependencies, and they are confined to `internal/tui`. Everything
else — the engine that watches files and moves logs — stays standard-library
only. Go still links it all into a single static binary, so the "one file you
just run" promise is unchanged.
```

In `CLAUDE.md`, under "## Conventions", change the dependency line to:

```markdown
- No third-party deps in the engine packages (single small binary is a product
  promise — see `docs/stack.md`). The ONE exception: the interactive app in
  `internal/tui` uses the charmbracelet libraries (Bubble Tea, Lip Gloss).
```

And in `CLAUDE.md` "## Current state", note: `codexssd` with no arguments now launches the interactive dashboard (`internal/tui`).

- [ ] **Step 10: Verify build/vet/format/test**

Run:
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
```
Expected: no `gofmt` output; all green. (Optional manual: `go run ./cmd/codexssd` opens the screen; `q` quits.)

- [ ] **Step 11: Commit**

```bash
git add internal/tui cmd/codexssd/main.go go.mod go.sum docs/stack.md CLAUDE.md
git commit -m "feat(tui): interactive shell skeleton + entry point (bare codexssd)"
```

---

## Task 2: Load and render the dashboard

**Files:**
- Create: `internal/tui/commands.go`
- Modify: `internal/tui/model.go` (Init), `internal/tui/update.go` (handle loadedMsg), `internal/tui/view.go` (real dashboard)
- Modify: `internal/tui/update_test.go` (add dashboard tests)

**Interfaces:**
- Consumes: `codex.Dir`, `codex.ScanLogs`, `codex.IsCodexRunning`, `codex.ErrUnsupportedPlatform`, `codex.HumanBytes`, `cleaner.PlanCodexLogs`, `cleaner.ListBackups`.
- Produces:
  - seams (function vars): `codexDir`, `scanLogs`, `isCodexRunning`, `planLogs`, `listBackups`, `applyPlan`, `restoreBackup`
  - `loadCmd() tea.Msg` returning `loadedMsg`
  - `type loadedMsg struct { report codex.LogReport; running, supported bool; runErr, loadErr error; plan cleaner.Plan; backups []cleaner.Backup }`
  - `Model.deadweight() bool`, `Model.lastTidy() (time.Time, bool)`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/update_test.go`:

```go
import (
	"strings"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
)

func sampleLoaded() loadedMsg {
	return loadedMsg{
		report: codex.LogReport{
			CodexDir:   "/home/u/.codex",
			DirExists:  true,
			Files:      []codex.LogFile{{Name: "logs_2.sqlite", Exists: true, Size: 200 * 1024 * 1024}},
			TotalBytes: 200 * 1024 * 1024,
		},
		running:   false,
		supported: true,
		plan:      cleaner.Plan{TotalBytes: 200 * 1024 * 1024},
		backups:   nil,
	}
}

func TestLoadedPopulatesDashboard(t *testing.T) {
	m, _ := step(New(), sampleLoaded())
	if m.report.TotalBytes != 200*1024*1024 {
		t.Errorf("report not stored: %d", m.report.TotalBytes)
	}
	if !m.deadweight() {
		t.Error("200 MiB should count as deadweight (>= 100 MiB)")
	}
	view := m.View()
	for _, want := range []string{"logs_2.sqlite", "200.0 MiB", "tidy"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard view missing %q:\n%s", want, view)
		}
	}
}

func TestNotDeadweightBelowThreshold(t *testing.T) {
	msg := sampleLoaded()
	msg.report.TotalBytes = 1 * 1024 * 1024 // 1 MiB
	msg.plan.TotalBytes = 1 * 1024 * 1024
	m, _ := step(New(), msg)
	if m.deadweight() {
		t.Error("1 MiB should not count as deadweight")
	}
}

func TestLastTidyFromBackups(t *testing.T) {
	when := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	msg := sampleLoaded()
	msg.backups = []cleaner.Backup{{Dir: "/b/20260620-090000", Manifest: cleaner.Manifest{MovedAt: when}}}
	m, _ := step(New(), msg)
	got, ok := m.lastTidy()
	if !ok || !got.Equal(when) {
		t.Errorf("lastTidy = %v ok=%v, want %v", got, ok, when)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestLoaded|TestNotDeadweight|TestLastTidy' -v`
Expected: FAIL — `loadedMsg`, `deadweight`, `lastTidy` undefined.

- [ ] **Step 3: Create commands.go**

Create `internal/tui/commands.go`:

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
)

// Engine seams. They default to the real engine functions and are overridden in
// tests so the commands (including the running-gate) are hermetically testable.
var (
	codexDir       = codex.Dir
	scanLogs       = codex.ScanLogs
	isCodexRunning = codex.IsCodexRunning
	planLogs       = cleaner.PlanCodexLogs
	listBackups    = cleaner.ListBackups
	restoreBackup  = cleaner.Restore
	// applyPlan moves a plan's logs aside and reports (dest, bytesMoved, err).
	applyPlan = func(p cleaner.Plan) (string, int64, error) {
		dest, err := p.Apply(time.Now())
		return dest, p.TotalBytes, err
	}
)

// loadedMsg carries a full status snapshot for the dashboard.
type loadedMsg struct {
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup
}

// loadCmd gathers the dashboard snapshot (read-only).
func loadCmd() tea.Msg {
	dir, err := codexDir()
	if err != nil {
		return loadedMsg{loadErr: err}
	}
	report := scanLogs(dir)
	running, runErr := isCodexRunning()
	supported := runErr != codex.ErrUnsupportedPlatform
	plan, _ := planLogs(dir)
	backups, _ := listBackups(dir)
	return loadedMsg{
		report: report, running: running, supported: supported,
		runErr: runErr, plan: plan, backups: backups,
	}
}
```

- [ ] **Step 4: Have Init load, and handle loadedMsg**

In `internal/tui/model.go`, change `Init`:

```go
func (m Model) Init() tea.Cmd {
	return loadCmd
}
```

In `internal/tui/update.go`, add a `loadedMsg` case to `Update` (before the `tea.KeyMsg` case is fine; place inside the `switch msg := msg.(type)`):

```go
	case loadedMsg:
		m.report = msg.report
		m.running = msg.running
		m.supported = msg.supported
		m.runErr = msg.runErr
		m.loadErr = msg.loadErr
		m.plan = msg.plan
		m.backups = msg.backups
		return m, nil
```

Add these helper methods to `internal/tui/model.go`:

```go
import "time" // add to model.go's imports

// deadweight reports whether the Codex logs are large enough to emphasize.
func (m Model) deadweight() bool {
	return m.report.TotalBytes >= deadweightThreshold
}

// lastTidy returns the most recent backup time, if any backups exist.
func (m Model) lastTidy() (time.Time, bool) {
	var newest time.Time
	found := false
	for _, b := range m.backups {
		if b.Manifest.MovedAt.After(newest) {
			newest = b.Manifest.MovedAt
			found = true
		}
	}
	return newest, found
}
```

- [ ] **Step 5: Render the real dashboard**

Replace the ENTIRE contents of `internal/tui/view.go` with the following (this supersedes the Task 1 version — it adds the `codex` import and `renderDashboard`, and keeps `renderHelp` unchanged):

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/codex"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

// View implements tea.Model.
func (m Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	return m.renderDashboard()
}

func (m Model) renderDashboard() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD"))
	fmt.Fprintln(&b)

	if m.loadErr != nil {
		fmt.Fprintf(&b, "Could not read Codex's folder: %v\n\n", m.loadErr)
		fmt.Fprintln(&b, m.footer())
		return b.String()
	}

	fmt.Fprintf(&b, "Codex folder: %s\n", m.report.CodexDir)
	if !m.report.DirExists {
		fmt.Fprintln(&b, "  (not found — Codex may not have run yet)")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, m.footer())
		return b.String()
	}

	fmt.Fprintln(&b, "Codex logs:")
	for _, f := range m.report.Files {
		if f.Exists {
			fmt.Fprintf(&b, "  %-20s %10s\n", f.Name, codex.HumanBytes(f.Size))
		}
	}
	fmt.Fprintf(&b, "  %-20s %10s\n\n", "Total", codex.HumanBytes(m.report.TotalBytes))

	if m.deadweight() {
		fmt.Fprintf(&b, "⚠  %s of Codex logs are sitting here — worth tidying.\n", codex.HumanBytes(m.report.TotalBytes))
	} else {
		fmt.Fprintln(&b, "Nothing alarming right now.")
	}

	switch {
	case !m.supported:
		fmt.Fprintln(&b, "(This platform can't check whether Codex is running.)")
	case m.running:
		fmt.Fprintln(&b, "Codex appears to be running.")
	default:
		fmt.Fprintln(&b, "Codex doesn't appear to be running.")
	}

	if t, ok := m.lastTidy(); ok {
		fmt.Fprintf(&b, "Recoverable backups: %d (last tidy %s)\n", len(m.backups), t.Format("2006-01-02 15:04"))
	} else {
		fmt.Fprintln(&b, "Recoverable backups: none")
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, m.footer())
	return b.String()
}

func (m Model) footer() string {
	return "c tidy · r restore · ? help · q quit"
}

func (m Model) renderHelp() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD — help"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "c    tidy Codex's logs aside (recoverable)")
	fmt.Fprintln(&b, "r    restore previously tidied logs")
	fmt.Fprintln(&b, "?    toggle this help")
	fmt.Fprintln(&b, "q    quit")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "press ? or esc to close")
	return b.String()
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (all Task 1 + Task 2 tests).

- [ ] **Step 7: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): dashboard loads and renders real Codex status"
```

---

## Task 3: Tidy (clean) flow

**Files:**
- Modify: `internal/tui/commands.go` (cleanCmd + messages), `internal/tui/update.go` (clean keys + result), `internal/tui/view.go` (confirm/cleaning/result/blocked screens)
- Modify: `internal/tui/update_test.go` (clean transition + command tests)

**Interfaces:**
- Consumes: `planLogs`, `applyPlan`, `isCodexRunning`, `codex.ErrUnsupportedPlatform` (seams from Task 2).
- Produces:
  - `type cleanResultMsg struct { dest string; movedBytes int64; err error }`
  - `type blockedMsg struct { reason string }`
  - `cleanCmd() tea.Msg`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/update_test.go`:

```go
func TestCleanKeyBlockedWhileRunning(t *testing.T) {
	m, _ := step(New(), sampleLoaded()) // not running, has plan
	m.running = true                    // Codex started
	m, cmd := step(m, key("c"))
	if m.state != stateBlocked {
		t.Fatalf("state = %v, want stateBlocked", m.state)
	}
	if cmd != nil {
		t.Error("pressing c while running should not dispatch a command")
	}
}

func TestCleanKeyOpensConfirm(t *testing.T) {
	m, _ := step(New(), sampleLoaded()) // not running, 200 MiB plan
	m, _ = step(m, key("c"))
	if m.state != stateConfirmClean {
		t.Fatalf("state = %v, want stateConfirmClean", m.state)
	}
}

func TestConfirmCleanYesDispatchesAndResult(t *testing.T) {
	m, _ := step(New(), sampleLoaded())
	m, _ = step(m, key("c")) // -> confirm
	m, cmd := step(m, key("y"))
	if m.state != stateCleaning {
		t.Fatalf("state = %v, want stateCleaning", m.state)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch cleanCmd")
	}
	// Feed a successful result.
	m, _ = step(m, cleanResultMsg{dest: "/b/20260626-100000", movedBytes: 200 * 1024 * 1024})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if !strings.Contains(m.View(), "200.0 MiB") {
		t.Errorf("result view missing moved size:\n%s", m.View())
	}
}

func TestConfirmCleanNoReturnsToDashboard(t *testing.T) {
	m, _ := step(New(), sampleLoaded())
	m, _ = step(m, key("c"))
	m, _ = step(m, key("n"))
	if m.state != stateDashboard {
		t.Errorf("state = %v, want stateDashboard", m.state)
	}
}

// cleanCmd must NOT touch the engine while Codex is running.
func TestCleanCmdGateRefusesWhileRunning(t *testing.T) {
	origRun, origApply := isCodexRunning, applyPlan
	t.Cleanup(func() { isCodexRunning, applyPlan = origRun, origApply })

	applied := false
	isCodexRunning = func() (bool, error) { return true, nil }
	applyPlan = func(p cleaner.Plan) (string, int64, error) { applied = true; return "", 0, nil }

	msg := cleanCmd()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("cleanCmd returned %T, want blockedMsg", msg)
	}
	if applied {
		t.Error("applyPlan was called while Codex running — gate bypassed")
	}
}

// cleanCmd moves logs when Codex is not running.
func TestCleanCmdMovesWhenNotRunning(t *testing.T) {
	origDir, origRun := codexDir, isCodexRunning
	t.Cleanup(func() { codexDir, isCodexRunning = origDir, origRun })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs_2.sqlite"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	codexDir = func() (string, error) { return dir, nil }
	isCodexRunning = func() (bool, error) { return false, nil }

	msg := cleanCmd()
	res, ok := msg.(cleanResultMsg)
	if !ok {
		t.Fatalf("cleanCmd returned %T, want cleanResultMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("clean failed: %v", res.err)
	}
	if res.movedBytes != 128 {
		t.Errorf("movedBytes = %d, want 128", res.movedBytes)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs_2.sqlite")); !os.IsNotExist(err) {
		t.Error("log was not moved aside")
	}
}
```

Add imports `os` and `path/filepath` to the test file's import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'Clean' -v`
Expected: FAIL — `cleanResultMsg`, `blockedMsg`, `cleanCmd` undefined; clean keys not handled.

- [ ] **Step 3: Add cleanCmd and messages**

Append to `internal/tui/commands.go`:

```go
// cleanResultMsg reports the outcome of a tidy.
type cleanResultMsg struct {
	dest       string
	movedBytes int64
	err        error
}

// blockedMsg means an action was refused because Codex is running or the
// platform can't be checked.
type blockedMsg struct {
	reason string
}

// cleanCmd re-checks that Codex is stopped (authoritative gate), then moves the
// logs aside. It NEVER calls applyPlan while Codex is running.
func cleanCmd() tea.Msg {
	running, runErr := isCodexRunning()
	if runErr == codex.ErrUnsupportedPlatform {
		return blockedMsg{reason: "This platform can't verify Codex is closed, so tidying is disabled here."}
	}
	if runErr != nil {
		return cleanResultMsg{err: runErr}
	}
	if running {
		return blockedMsg{reason: "Codex appears to be running. Close it first, then try again."}
	}
	dir, err := codexDir()
	if err != nil {
		return cleanResultMsg{err: err}
	}
	plan, err := planLogs(dir)
	if err != nil {
		return cleanResultMsg{err: err}
	}
	if plan.Empty() {
		return cleanResultMsg{dest: "", movedBytes: 0}
	}
	dest, moved, err := applyPlan(plan)
	return cleanResultMsg{dest: dest, movedBytes: moved, err: err}
}
```

- [ ] **Step 4: Handle clean keys and results in Update**

In `internal/tui/update.go`, add cases to the `Update` type switch (alongside `loadedMsg`):

```go
	case cleanResultMsg:
		m.state = stateResult
		if msg.err != nil {
			m.resultErr = msg.err
			m.resultMsg = ""
		} else if msg.dest == "" {
			m.resultErr = nil
			m.resultMsg = "Nothing to tidy — no Codex logs are present."
		} else {
			m.resultErr = nil
			m.resultMsg = fmt.Sprintf("Tidied %s of Codex logs aside.\nBackup: %s\nNothing was deleted — restore any time.", codex.HumanBytes(msg.movedBytes), msg.dest)
		}
		return m, nil
	case blockedMsg:
		m.state = stateBlocked
		m.blockedReason = msg.reason
		return m, nil
```

Add the imports `fmt` and the codex package to `update.go`:

```go
import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/codex"
)
```

Replace `handleKey` with a state-aware version:

```go
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys.
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	}

	switch m.state {
	case stateDashboard:
		return m.handleDashboardKey(msg)
	case stateConfirmClean:
		switch msg.String() {
		case "y":
			m.state = stateCleaning
			return m, cleanCmd
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateResult, stateBlocked, stateError:
		switch msg.String() {
		case "enter", "esc":
			m.state = stateDashboard
			return m, loadCmd // refresh after returning
		}
	}
	return m, nil
}

// handleDashboardKey handles keys on the main screen.
func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "c":
		// Refuse up-front if we already know Codex is running / unsupported.
		if !m.supported {
			m.state = stateBlocked
			m.blockedReason = "This platform can't verify Codex is closed, so tidying is disabled here."
			return m, nil
		}
		if m.running {
			m.state = stateBlocked
			m.blockedReason = "Codex appears to be running. Close it first, then try again."
			return m, nil
		}
		if m.plan.Empty() {
			m.state = stateResult
			m.resultMsg = "Nothing to tidy — no Codex logs are present."
			m.resultErr = nil
			return m, nil
		}
		m.state = stateConfirmClean
		return m, nil
	}
	return m, nil
}
```

- [ ] **Step 5: Render the clean-flow screens**

Append per-state renderers to `internal/tui/view.go` and route to them. Replace `View` with:

```go
// View implements tea.Model.
func (m Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	switch m.state {
	case stateConfirmClean:
		return m.renderConfirmClean()
	case stateCleaning:
		return m.renderWorking("Tidying Codex logs aside…")
	case stateResult:
		return m.renderResult()
	case stateBlocked:
		return m.renderBlocked()
	default:
		return m.renderDashboard()
	}
}
```

Add these functions to `view.go`:

```go
func (m Model) renderConfirmClean() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Tidy Codex logs"))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Move %s of Codex's own logs into a recoverable bin?\n", codex.HumanBytes(m.report.TotalBytes))
	fmt.Fprintln(&b, "Nothing is deleted — you can restore them any time.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "y yes · n no")
	return b.String()
}

func (m Model) renderWorking(label string) string {
	return titleStyle.Render("CodexSSD") + "\n\n" + label + "\n"
}

func (m Model) renderResult() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD"))
	fmt.Fprintln(&b)
	if m.resultErr != nil {
		fmt.Fprintf(&b, "Something went wrong: %v\n", m.resultErr)
	} else {
		fmt.Fprintln(&b, m.resultMsg)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "enter return to dashboard")
	return b.String()
}

func (m Model) renderBlocked() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Can't do that right now"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, m.blockedReason)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "enter return to dashboard")
	return b.String()
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (all clean-flow tests + prior tests).

- [ ] **Step 7: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): in-app Tidy flow (confirm, gated clean, result)"
```

---

## Task 4: Restore flow

**Files:**
- Modify: `internal/tui/commands.go` (restoreCmd + message), `internal/tui/update.go` (restore keys + result), `internal/tui/view.go` (restore-list/confirm/result screens)
- Modify: `internal/tui/update_test.go` (restore transition + command tests)

**Interfaces:**
- Consumes: `isCodexRunning`, `restoreBackup`, `codex.ErrUnsupportedPlatform`, `cleaner.Backup`, `filepath.Base`.
- Produces:
  - `type restoreResultMsg struct { id string; err error }`
  - `restoreCmd(dir string) tea.Cmd`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/update_test.go`:

```go
func loadedWithBackup() loadedMsg {
	msg := sampleLoaded()
	msg.backups = []cleaner.Backup{{
		Dir:      "/home/u/.codex/codexssd-backups/20260626-100000",
		Manifest: cleaner.Manifest{MovedAt: time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)},
	}}
	return msg
}

func TestRestoreKeyOpensListWhenBackupsExist(t *testing.T) {
	m, _ := step(New(), loadedWithBackup())
	m, _ = step(m, key("r"))
	if m.state != stateRestoreList {
		t.Fatalf("state = %v, want stateRestoreList", m.state)
	}
	if !strings.Contains(m.View(), "20260626-100000") {
		t.Errorf("restore list view missing backup id:\n%s", m.View())
	}
}

func TestRestoreKeyNoBackupsShowsResult(t *testing.T) {
	m, _ := step(New(), sampleLoaded()) // no backups
	m, _ = step(m, key("r"))
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if !strings.Contains(m.View(), "No backups") {
		t.Errorf("expected a 'no backups' message:\n%s", m.View())
	}
}

func TestRestoreConfirmYesDispatches(t *testing.T) {
	m, _ := step(New(), loadedWithBackup())
	m, _ = step(m, key("r"))     // list
	m, _ = step(m, key("enter")) // select -> confirm
	if m.state != stateConfirmRestore {
		t.Fatalf("state = %v, want stateConfirmRestore", m.state)
	}
	m, cmd := step(m, key("y"))
	if m.state != stateRestoring {
		t.Fatalf("state = %v, want stateRestoring", m.state)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch restoreCmd")
	}
	m, _ = step(m, restoreResultMsg{id: "20260626-100000"})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
}

// restoreCmd must NOT touch the engine while Codex is running.
func TestRestoreCmdGateRefusesWhileRunning(t *testing.T) {
	origRun, origRestore := isCodexRunning, restoreBackup
	t.Cleanup(func() { isCodexRunning, restoreBackup = origRun, origRestore })

	called := false
	isCodexRunning = func() (bool, error) { return true, nil }
	restoreBackup = func(dir string) error { called = true; return nil }

	msg := restoreCmd("/some/backup")()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("restoreCmd returned %T, want blockedMsg", msg)
	}
	if called {
		t.Error("restoreBackup was called while Codex running — gate bypassed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'Restore' -v`
Expected: FAIL — `restoreResultMsg`, `restoreCmd` undefined; restore keys not handled.

- [ ] **Step 3: Add restoreCmd and message**

Append to `internal/tui/commands.go`:

```go
// restoreResultMsg reports the outcome of a restore.
type restoreResultMsg struct {
	id  string
	err error
}

// restoreCmd re-checks that Codex is stopped, then restores the backup at dir.
// It NEVER calls restoreBackup while Codex is running.
func restoreCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		running, runErr := isCodexRunning()
		if runErr == codex.ErrUnsupportedPlatform {
			return blockedMsg{reason: "This platform can't verify Codex is closed, so restoring is disabled here."}
		}
		if runErr != nil {
			return restoreResultMsg{err: runErr}
		}
		if running {
			return blockedMsg{reason: "Codex appears to be running. Close it first, then try again."}
		}
		err := restoreBackup(dir)
		return restoreResultMsg{id: filepathBase(dir), err: err}
	}
}
```

Add a tiny helper (so `commands.go` doesn't import `path/filepath` only for one call — but importing it is fine; use the standard import):

```go
import "path/filepath"

func filepathBase(p string) string { return filepath.Base(p) }
```

(If `path/filepath` is already imported in `commands.go`, call `filepath.Base` directly and skip the helper.)

- [ ] **Step 4: Handle restore keys and result in Update**

In `internal/tui/update.go` `handleDashboardKey`, add an `"r"` case before the closing of the switch:

```go
	case "r":
		if len(m.backups) == 0 {
			m.state = stateResult
			m.resultMsg = "No backups to restore — nothing has been tidied yet."
			m.resultErr = nil
			return m, nil
		}
		m.selected = 0
		m.state = stateRestoreList
		return m, nil
```

Add `stateRestoreList` and `stateConfirmRestore` handling to `handleKey`'s state switch:

```go
	case stateRestoreList:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.backups)-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			m.state = stateConfirmRestore
			return m, nil
		case "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateConfirmRestore:
		switch msg.String() {
		case "y":
			m.state = stateRestoring
			return m, restoreCmd(m.backups[m.selected].Dir)
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
```

Add a `restoreResultMsg` case to the `Update` type switch:

```go
	case restoreResultMsg:
		m.state = stateResult
		if msg.err != nil {
			m.resultErr = msg.err
			m.resultMsg = ""
		} else {
			m.resultErr = nil
			m.resultMsg = fmt.Sprintf("Restored backup %s to your Codex folder.", msg.id)
		}
		return m, nil
```

- [ ] **Step 5: Render restore screens**

In `internal/tui/view.go`, add cases to the `View` switch:

```go
	case stateRestoreList:
		return m.renderRestoreList()
	case stateConfirmRestore:
		return m.renderConfirmRestore()
	case stateRestoring:
		return m.renderWorking("Restoring…")
```

Add these functions to `view.go`:

```go
func (m Model) renderRestoreList() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Restore a backup"))
	fmt.Fprintln(&b)
	for i, bk := range m.backups {
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		var total int64
		for _, it := range bk.Manifest.Items {
			total += it.Size
		}
		fmt.Fprintf(&b, "%s%-18s %10s\n", cursor, filepathBase(bk.Dir), codex.HumanBytes(total))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "↑/↓ choose · enter select · esc back")
	return b.String()
}

func (m Model) renderConfirmRestore() string {
	var b strings.Builder
	id := filepathBase(m.backups[m.selected].Dir)
	fmt.Fprintln(&b, titleStyle.Render("Restore backup"))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Move the logs in backup %s back to your Codex folder?\n", id)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "y yes · n no")
	return b.String()
}
```

(`view.go` already imports `codex`; `filepathBase` is defined in `commands.go` in the same package.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (all restore-flow tests + prior tests).

- [ ] **Step 7: Verify build/vet/format and exercise**

Run:
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
```
Expected: no `gofmt` output; all green. (Optional manual: `go run ./cmd/codexssd`, press `r`.)

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): in-app Restore flow (list, gated restore, result)"
```

---

## Self-Review notes

- **Spec coverage:** entry point change (Task 1); dependency boundary + docs (Task 1); dashboard status/running/backups/deadweight-threshold (Task 2); Tidy with fresh running-gate (Task 3); Restore with fresh running-gate (Task 4); help overlay (Task 1); confirmations before actions (Tasks 3–4); footprint deferred (not built, per spec); background watch / install-agent action out of scope (not built, per spec).
- **Safety verification:** `TestCleanCmdGateRefusesWhileRunning` and `TestRestoreCmdGateRefusesWhileRunning` assert the engine seam is never called while Codex runs (the cross-task safety invariant), plus the up-front dashboard refusals.
- **Type consistency:** seams (`codexDir`, `scanLogs`, `isCodexRunning`, `planLogs`, `applyPlan`, `listBackups`, `restoreBackup`), messages (`loadedMsg`, `cleanResultMsg`, `restoreResultMsg`, `blockedMsg`), and `state` constants are used identically across tasks. `applyPlan` returns `(string, int64, error)` everywhere; `restoreCmd` returns a `tea.Cmd`; `cleanCmd`/`loadCmd` are `func() tea.Msg`.
- **`bubbles` not added** (static "Working…" text) — deliberate YAGNI trim noted in Global Constraints.
