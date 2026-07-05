# TUI Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reskin CodexSSD's interactive dashboard to an OpenCode-style look — ASCII-art wordmark, cyan/teal accent, bordered panels, styled status bar — with zero behavior change, per `docs/superpowers/specs/2026-07-05-tui-overhaul-design.md`.

**Architecture:** A shared Lip Gloss style system (`styles.go`), a width-aware ASCII logo (`logo.go`), and small composition helpers (`panel`, `statusBar`, `effectiveWidth`) that a restructured `view.go` uses to render each screen. `View()` stays a pure function of `Model`; the only non-view change is one additive `Model.height` field.

**Tech Stack:** Go, Bubble Tea, Lip Gloss (both already direct deps). No new modules.

## Global Constraints

Every task implicitly includes these; any violation is an automatic review reject.

- **No new third-party dependency.** Only `bubbletea` + `lipgloss` (direct) and their existing transitive deps (incl. `github.com/muesli/termenv`, already in `go.mod` as indirect). `go.mod` must not gain a NEW `require` module. (Dropping termenv's `// indirect` marker when a test imports it is allowed — it is not a new module.)
- **No behavior change.** No state added/removed/renamed; no keybinding changed; no command touched; no safety gate altered. `internal/tui/update.go` control flow, `commands.go`, `model.go` fields (except the one additive `height int`), and every non-`tui` package stay unchanged.
- **Charm-only-in-tui boundary preserved.** `lipgloss`/`bubbletea`/`termenv` imports appear only under `internal/tui`.
- **Readable without color.** Every color-carrying signal (risk, banner) also carries a text label or glyph.
- **Friendly plain-language text preserved.** Reuse the existing user-facing wording verbatim; only its styling/placement changes.
- **Determinism in tests:** a package `TestMain` sets the ascii color profile so `View()` output is plain text (Task 1). Assert on plain-text substrings, never ANSI escapes.
- Gate before any task is done: `go build ./... && go vet ./... && go test ./... && gofmt -l .` (gofmt output empty).
- Commit messages: `type(scope): summary`.

**Execution order:** Tasks run 1 → 5 in order. Task 2 is independent of Task 1. Tasks 3–5 consume Tasks 1–2. Task 4 consumes Task 3's helpers; Task 5 consumes Task 3's helpers and Task 2's logo.

---

### Task 1: Style system (`styles.go`) + test harness

**Files:**
- Create: `internal/tui/styles.go`
- Create: `internal/tui/styles_test.go`
- Create: `internal/tui/tui_test.go` (package `TestMain`)

**Interfaces:**
- Consumes: `monitor.Risk` and its constants (`monitor.RiskLow/RiskMedium/RiskHigh/RiskCritical`) from `internal/monitor`.
- Produces (used by all later tasks):
  - Color vars: `accentColor`, `mutedColor` (both `lipgloss.AdaptiveColor`).
  - Style vars: `logoStyle`, `subtitleStyle`, `headerStyle`, `panelTitleStyle`, `panelBorderStyle`, `statusBarStyle`, `selectedRowStyle`, `mutedTextStyle` (all `lipgloss.Style`).
  - `riskColor(level monitor.Risk) lipgloss.TerminalColor`
  - `riskStyle(level monitor.Risk) lipgloss.Style`
  - `riskGlyph(level monitor.Risk) string` → `"●"`

- [ ] **Step 1: Write the failing test** — `internal/tui/styles_test.go`:

```go
package tui

import (
	"testing"

	"github.com/0xdefence/codexssd/internal/monitor"
)

func TestRiskColorPerLevel(t *testing.T) {
	// Each level maps to its own distinct color; adjacent levels must differ.
	seen := map[lipgloss_TerminalColor]bool{}
	levels := []monitor.Risk{monitor.RiskLow, monitor.RiskMedium, monitor.RiskHigh, monitor.RiskCritical}
	for _, lv := range levels {
		c := riskColor(lv)
		if c == nil {
			t.Fatalf("riskColor(%v) is nil", lv)
		}
		if seen[c] {
			t.Errorf("riskColor(%v) duplicates an earlier level's color", lv)
		}
		seen[c] = true
	}
}

func TestRiskStyleUsesRiskColor(t *testing.T) {
	if got := riskStyle(monitor.RiskCritical).GetForeground(); got != riskColor(monitor.RiskCritical) {
		t.Errorf("riskStyle foreground = %v, want riskColor(Critical) %v", got, riskColor(monitor.RiskCritical))
	}
	if !riskStyle(monitor.RiskHigh).GetBold() {
		t.Error("riskStyle should be bold")
	}
}

func TestRiskGlyph(t *testing.T) {
	if riskGlyph(monitor.RiskLow) != "●" {
		t.Errorf("riskGlyph = %q, want ●", riskGlyph(monitor.RiskLow))
	}
}
```

Replace the map key type: use `lipgloss.TerminalColor` directly. Corrected imports/keys shown in Step 3's note.

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/ -run TestRisk` → FAIL (undefined: riskColor / styles).

- [ ] **Step 3: Implement `internal/tui/styles.go`**

```go
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/monitor"
)

// Palette. Adaptive colors pick a legible shade for light or dark terminals;
// under NO_COLOR / non-color terminals Lip Gloss drops color automatically.
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#0e7490", Dark: "#22d3ee"} // cyan/teal brand
	mutedColor  = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"} // secondary text

	// Risk colors are semantic and deliberately distinct from the brand accent.
	riskGreenColor  = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#22c55e"}
	riskYellowColor = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#eab308"}
	riskOrangeColor = lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#f97316"}
	riskRedColor    = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#ef4444"}
)

// Shared styles. Centralized so every screen reads the same.
var (
	logoStyle     = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	subtitleStyle = lipgloss.NewStyle().Foreground(mutedColor)
	headerStyle   = lipgloss.NewStyle().Foreground(accentColor).Bold(true)

	panelTitleStyle  = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	panelBorderStyle = lipgloss.NewStyle().Foreground(accentColor)

	// The status bar fills its width with the accent as a background; text on it
	// stays high-contrast. Foreground("0")/Background(accent) reads as dark-on-cyan.
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor)

	// Selected restore-list row: inverse-style highlight.
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor)

	mutedTextStyle = lipgloss.NewStyle().Foreground(mutedColor)
)

// riskColor maps a risk level to its semantic color.
func riskColor(level monitor.Risk) lipgloss.TerminalColor {
	switch level {
	case monitor.RiskCritical:
		return riskRedColor
	case monitor.RiskHigh:
		return riskOrangeColor
	case monitor.RiskMedium:
		return riskYellowColor
	default:
		return riskGreenColor
	}
}

// riskStyle is the bold, colored style for a risk level's label and glyph.
func riskStyle(level monitor.Risk) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(riskColor(level)).Bold(true)
}

// riskGlyph is the status dot shown beside a risk level. Always present (even at
// LOW) so the layout does not shift as risk changes.
func riskGlyph(monitor.Risk) string { return "●" }
```

Now fix the test's map key: change `map[lipgloss_TerminalColor]bool` to `map[lipgloss.TerminalColor]bool` and add `"github.com/charmbracelet/lipgloss"` to the test imports. (`AdaptiveColor` is comparable, so it is a valid map key.)

- [ ] **Step 4: Create the test harness `internal/tui/tui_test.go`**

```go
package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces the ascii color profile so every View()/render in this
// package's tests produces deterministic plain text (no ANSI escapes) to assert
// against. termenv is already a transitive dependency — no new module is added.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}
```

- [ ] **Step 5: Run to verify pass** — `go test ./internal/tui/ -run TestRisk` → PASS. Then `go test ./internal/tui/` → the whole package still passes under the new ascii-profile harness (existing update/session tests included).

- [ ] **Step 6: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/styles.go internal/tui/styles_test.go internal/tui/tui_test.go
git commit -m "feat(tui): shared style system (accent palette, risk styles) + ascii test harness"
```

---

### Task 2: ASCII logo (`logo.go`)

**Files:**
- Create: `internal/tui/logo.go`
- Create: `internal/tui/logo_test.go`

**Interfaces:**
- Consumes: `logoStyle`, `subtitleStyle` (Task 1).
- Produces:
  - `renderLogo(width int) string` — the styled, horizontally-centered CODEX block wordmark plus subtitle when `width` is at least the art's natural width; otherwise the compact form. Callers pass a positive width. Used by the dashboard (Task 4).
  - `renderCompactLogo(width int) string` — always the one-line `codexSSD` wordmark plus subtitle, centered. Used as the header on secondary screens (Task 5), where vertical space is tight.

- [ ] **Step 1: Write the failing test** — `internal/tui/logo_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestRenderLogoWideHasBlockArt(t *testing.T) {
	out := renderLogo(100)
	if !strings.Contains(out, "██████╗") {
		t.Errorf("wide logo should contain the block art, got:\n%s", out)
	}
	if !strings.Contains(out, "the disk watchdog") {
		t.Errorf("logo should contain the subtitle, got:\n%s", out)
	}
}

func TestRenderLogoNarrowFallsBackToCompact(t *testing.T) {
	out := renderLogo(30)
	if strings.Contains(out, "██████╗") {
		t.Errorf("narrow logo must NOT use the block art, got:\n%s", out)
	}
	if !strings.Contains(out, "codexSSD") {
		t.Errorf("narrow logo should show the compact wordmark, got:\n%s", out)
	}
	if !strings.Contains(out, "the disk watchdog") {
		t.Errorf("narrow logo should still show the subtitle, got:\n%s", out)
	}
}

func TestRenderCompactLogoAlwaysOneLineWordmark(t *testing.T) {
	out := renderCompactLogo(100) // wide, but compact form is forced
	if strings.Contains(out, "██████╗") {
		t.Errorf("compact logo must never use the block art, got:\n%s", out)
	}
	if !strings.Contains(out, "codexSSD") || !strings.Contains(out, "the disk watchdog") {
		t.Errorf("compact logo should show wordmark + subtitle, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/ -run TestRenderLogo` → FAIL (undefined: renderLogo).

- [ ] **Step 3: Implement `internal/tui/logo.go`** (preserve every character, including trailing spaces, exactly):

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// blockLogo is the ANSI-shadow wordmark "CODEX". Its lines are equal width; do
// not reflow or trim — the trailing spaces keep the block rectangular.
const blockLogo = ` ██████╗ ██████╗ ██████╗ ███████╗██╗  ██╗
██╔════╝██╔═══██╗██╔══██╗██╔════╝╚██╗██╔╝
██║     ██║   ██║██║  ██║█████╗   ╚███╔╝ 
██║     ██║   ██║██║  ██║██╔══╝   ██╔██╗ 
╚██████╗╚██████╔╝██████╔╝███████╗██╔╝ ██╗
 ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝`

const logoSubtitle = "SSD · the disk watchdog"

// logoWidth is the natural rendered width of blockLogo (widest line).
func logoWidth() int {
	w := 0
	for _, line := range strings.Split(blockLogo, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			w = lw
		}
	}
	return w
}

// renderCompactLogo is the one-line wordmark used as a header on secondary
// screens, where vertical space is at a premium. Always compact, never the block.
func renderCompactLogo(width int) string {
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	return center.Render(logoStyle.Render("codexSSD")) + "\n" + center.Render(subtitleStyle.Render(logoSubtitle))
}

// renderLogo returns the centered, styled wordmark for a terminal of the given
// width. When width is too narrow for the block art it falls back to the compact
// form. Both include the subtitle. Callers pass a positive width.
func renderLogo(width int) string {
	if width < logoWidth() {
		return renderCompactLogo(width)
	}
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	return center.Render(logoStyle.Render(blockLogo)) + "\n" + center.Render(subtitleStyle.Render(logoSubtitle))
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run TestRenderLogo` → PASS.

- [ ] **Step 5: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/logo.go internal/tui/logo_test.go
git commit -m "feat(tui): width-aware ANSI-shadow CODEX wordmark with compact fallback"
```

---

### Task 3: Layout helpers + `Model.height` wiring

**Files:**
- Create: `internal/tui/layout.go`
- Create: `internal/tui/layout_test.go`
- Modify: `internal/tui/model.go` (add `height int` to `Model`)
- Modify: `internal/tui/update.go:16-18` (set `m.height` in the `WindowSizeMsg` case)

**Interfaces:**
- Consumes: `panelBorderStyle`, `panelTitleStyle`, `statusBarStyle` (Task 1).
- Produces (used by Tasks 4 & 5):
  - `effectiveWidth(m Model) int` — `m.width` if > 0, else `80`.
  - `panel(title, body string, width int) string` — `body` inside a rounded box of total outer `width`, with `title` in the top border.
  - `statusBar(keys, status string, width int) string` — full-width accent bar, `keys` left, `status` right.

- [ ] **Step 1: Write the failing test** — `internal/tui/layout_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/0xdefence/codexssd/internal/config"
)

func TestEffectiveWidthFallsBackTo80(t *testing.T) {
	if got := effectiveWidth(New(config.Default())); got != 80 {
		t.Errorf("effectiveWidth with no size = %d, want 80", got)
	}
	m := New(config.Default())
	m.width = 120
	if got := effectiveWidth(m); got != 120 {
		t.Errorf("effectiveWidth = %d, want 120", got)
	}
}

func TestPanelWrapsBodyWithTitleAndBorder(t *testing.T) {
	out := panel("Risk", "● LOW", 30)
	if !strings.Contains(out, "Risk") {
		t.Errorf("panel should show its title, got:\n%s", out)
	}
	if !strings.Contains(out, "● LOW") {
		t.Errorf("panel should show its body, got:\n%s", out)
	}
	// Rounded border corners present (ascii profile keeps box-drawing runes).
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("panel should draw a rounded border, got:\n%s", out)
	}
	// Every rendered line is the full outer width.
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 30 {
			t.Errorf("panel line width = %d, want 30: %q", w, line)
		}
	}
}

func TestStatusBarPutsKeysLeftStatusRight(t *testing.T) {
	out := statusBar("q quit", "30s", 40)
	if lipgloss.Width(out) != 40 {
		t.Errorf("status bar width = %d, want 40", lipgloss.Width(out))
	}
	if !strings.HasPrefix(strings.TrimRight(out, " "), "q quit") {
		t.Errorf("keys should be left-aligned, got %q", out)
	}
	if !strings.Contains(out, "30s") {
		t.Errorf("status should appear, got %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/ -run 'TestEffectiveWidth|TestPanel|TestStatusBar'` → FAIL (undefined helpers).

- [ ] **Step 3: Implement `internal/tui/layout.go`**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// effectiveWidth resolves the width to render at. Before the first WindowSizeMsg
// the model width is 0; assume a conventional 80-column terminal so the first
// paint is sane.
func effectiveWidth(m Model) int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

// panel wraps body in a rounded border of total outer `width`, with `title`
// spliced into the top border. Border runes are colored with the brand accent;
// the title with panelTitleStyle. Body lines are left-padded one space and
// filled to the inner width so the right border aligns.
func panel(title, body string, width int) string {
	if width < 4 {
		width = 4
	}
	inner := width - 2 // columns between the two vertical borders
	b := lipgloss.RoundedBorder()

	// Top border: "╭─ Title ──────╮" (or a plain top when title is empty).
	label := ""
	if title != "" {
		label = b.Top + " " + panelTitleStyle.Render(title) + " "
	}
	dashes := inner - lipgloss.Width(label)
	if dashes < 0 {
		dashes = 0
	}
	top := panelBorderStyle.Render(b.TopLeft) + label + panelBorderStyle.Render(strings.Repeat(b.Top, dashes)) + panelBorderStyle.Render(b.TopRight)
	bottom := panelBorderStyle.Render(b.BottomLeft + strings.Repeat(b.Top, inner) + b.BottomRight)

	left := panelBorderStyle.Render(b.Left)
	right := panelBorderStyle.Render(b.Right)

	var rows []string
	rows = append(rows, top)
	for _, line := range strings.Split(body, "\n") {
		pad := inner - 1 - lipgloss.Width(line) // 1 leading space
		if pad < 0 {
			pad = 0
		}
		rows = append(rows, left+" "+line+strings.Repeat(" ", pad)+right)
	}
	rows = append(rows, bottom)
	return strings.Join(rows, "\n")
}

// statusBar renders the full-width bottom bar: keys left, status right, filled
// with the accent background.
func statusBar(keys, status string, width int) string {
	gap := width - lipgloss.Width(keys) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
	}
	line := keys + strings.Repeat(" ", gap) + status
	return statusBarStyle.Width(width).Render(line)
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run 'TestEffectiveWidth|TestPanel|TestStatusBar'` → PASS.

- [ ] **Step 5: Add `height` to the model** — in `internal/tui/model.go`, extend the `Model` struct's top group:

```go
type Model struct {
	state    state
	showHelp bool
	width    int
	height   int
	cfg      config.Config
	// ... rest unchanged
```

- [ ] **Step 6: Wire it in `update.go`** — change the `WindowSizeMsg` case (currently `internal/tui/update.go:16-18`):

```go
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
```

- [ ] **Step 7: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty (existing tests unaffected; `height` is additive).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/layout.go internal/tui/layout_test.go internal/tui/model.go internal/tui/update.go
git commit -m "feat(tui): panel + status-bar layout helpers; track terminal height"
```

---

### Task 4: Restyle the dashboard (`renderDashboard`)

**Files:**
- Modify: `internal/tui/view.go` (rewrite `renderDashboard`; keep `View()` switch and `bannerState()` verbatim)
- Create: `internal/tui/view_test.go`

**Interfaces:**
- Consumes: `renderLogo` (Task 2); `effectiveWidth`, `panel`, `statusBar` (Task 3); `headerStyle`, `riskStyle`, `riskGlyph`, `mutedTextStyle` (Task 1); existing `Model` fields and methods (`m.report`, `m.assessment`, `m.running`, `m.supported`, `m.memBytes`, `m.backups`, `m.releaseNote`, `m.loadErr`, `bannerState()`, `lastTidy()`, `soonestRelease()`, `deadweight()`).
- Produces: a restyled `renderDashboard()` returning the composed dashboard string. No new exported names.

- [ ] **Step 1: Write the failing test** — `internal/tui/view_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/0xdefence/codexssd/internal/config"
)

// dashboardAfterLoad returns a model that has received one sampleLoaded() frame.
func dashboardAfterLoad(t *testing.T) Model {
	t.Helper()
	m, _ := step(New(config.Default()), sampleLoaded())
	return m
}

func TestDashboardShowsKeyFacts(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 100
	out := m.View()
	for _, want := range []string{
		"/home/u/.codex", // folder path
		"logs_2.sqlite",  // a file name
		"Total",          // total row label
		"c tidy",         // a keybinding in the status bar
		"the disk watchdog", // the logo subtitle
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q; got:\n%s", want, out)
		}
	}
}

func TestDashboardNarrowRendersWithoutPanic(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 30 // below the logo's block width → compact logo, single column
	out := m.View()
	if !strings.Contains(out, "codexSSD") { // compact logo
		t.Errorf("narrow dashboard should use the compact logo; got:\n%s", out)
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("narrow dashboard should still show the logs; got:\n%s", out)
	}
}

func TestDashboardShowsRiskPanel(t *testing.T) {
	// A single load frame evaluates to LOW (rate needs two samples). The Risk
	// panel and its level label must still appear.
	m := dashboardAfterLoad(t)
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "Risk") {
		t.Errorf("dashboard should show a Risk panel; got:\n%s", out)
	}
	if !strings.Contains(out, "LOW") {
		t.Errorf("dashboard should show the LOW risk label; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/ -run TestDashboard` → FAIL (compile error until `renderDashboard` uses the helpers, or assertion failures against the old flat renderer). It is acceptable for this step to fail by assertion; confirm it is red.

- [ ] **Step 3: Rewrite `renderDashboard` in `internal/tui/view.go`** (leave `View()`, `bannerState()`, `footer()` — see note — and the other `render*` funcs for Task 5):

```go
func (m Model) renderDashboard() string {
	w := effectiveWidth(m)
	var sections []string
	sections = append(sections, renderLogo(w), "")

	if m.loadErr != nil {
		sections = append(sections,
			panel("Codex folder", fmt.Sprintf("Could not read Codex's folder: %v", m.loadErr), w),
			"",
			statusBar(m.footer(), "watching ~/.codex", w),
		)
		return strings.Join(sections, "\n")
	}

	// Left panel: the Codex folder + log sizes.
	var logs strings.Builder
	fmt.Fprintf(&logs, "%s\n", m.report.CodexDir)
	if !m.report.DirExists {
		fmt.Fprint(&logs, "(not found — Codex may not have run yet)")
	} else {
		for _, f := range m.report.Files {
			if f.Exists {
				fmt.Fprintf(&logs, "%-20s %10s\n", f.Name, codex.HumanBytes(f.Size))
			}
		}
		fmt.Fprintf(&logs, "%-20s %10s", "Total", codex.HumanBytes(m.report.TotalBytes))
	}

	// Right panel: risk + process + memory.
	var risk strings.Builder
	lvl := m.assessment.Level
	fmt.Fprintf(&risk, "%s %s\n", riskStyle(lvl).Render(riskGlyph(lvl)), riskStyle(lvl).Render(lvl.String()))
	if lvl >= monitor.RiskMedium {
		reason := ""
		if len(m.assessment.Reasons) > 0 {
			reason = " · " + m.assessment.Reasons[0]
		}
		fmt.Fprintf(&risk, "%.0f MB/min · WAL %s%s\n", m.assessment.RateMBPerMin, codex.HumanBytes(m.assessment.WALBytes), reason)
	}
	switch {
	case !m.supported:
		fmt.Fprint(&risk, "Codex: can't check")
	case m.running:
		fmt.Fprint(&risk, "Codex: running")
	default:
		fmt.Fprint(&risk, "Codex: not running")
	}
	if m.running && m.memBytes > 0 {
		fmt.Fprintf(&risk, "\nmemory: %s", codex.HumanBytes(m.memBytes))
	}

	// Compose the two panels: side by side when wide, stacked when narrow.
	const twoColMin = 72
	if w >= twoColMin {
		leftW := (w - 2) / 2
		rightW := w - 2 - leftW
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Codex folder", logs.String(), leftW), "  ", panel("Risk", risk.String(), rightW))
		sections = append(sections, row)
	} else {
		sections = append(sections, panel("Codex folder", logs.String(), w), panel("Risk", risk.String(), w))
	}

	// Recycling bin (full width).
	bin := "empty"
	if t, ok := m.lastTidy(); ok {
		bin = fmt.Sprintf("%d backup(s) · last tidy %s", len(m.backups), t.Format("2006-01-02 15:04"))
		if s, ok := m.soonestRelease(); ok {
			bin += fmt.Sprintf(" · next release %s", s.Format("2006-01-02"))
		}
	}
	sections = append(sections, panel("Recycling bin", bin, w))

	// Banner line (unchanged logic).
	switch m.bannerState() {
	case bannerActionable:
		sections = append(sections, headerStyle.Render(fmt.Sprintf("⚠  %s of Codex logs piled up — press c to tidy.", codex.HumanBytes(m.report.TotalBytes))))
	case bannerInformational:
		sections = append(sections, mutedTextStyle.Render(fmt.Sprintf("⚠  %s piling up — I'll offer to tidy when Codex is closed.", codex.HumanBytes(m.report.TotalBytes))))
	default:
		sections = append(sections, mutedTextStyle.Render("Nothing alarming right now."))
	}
	if m.releaseNote != "" {
		sections = append(sections, mutedTextStyle.Render(m.releaseNote))
	}

	sections = append(sections, "", statusBar(m.footer(), "watching ~/.codex · updates every 30s", w))
	return strings.Join(sections, "\n")
}
```

Add any now-needed imports to `view.go`: `"github.com/charmbracelet/lipgloss"` and keep `fmt`, `strings`, `codex`, `monitor`. Keep `footer()` as-is (it returns the `c tidy · r restore · ? help · q quit` string used by the status bar). Do NOT change `View()` or `bannerState()`.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run TestDashboard` → PASS. Then `go test ./internal/tui/` → whole package green.

- [ ] **Step 5: Manual smoke** — `go run ./cmd/codexssd` in a real terminal: confirm the logo, two-panel top row, bin panel, banner, and accent status bar render; resize narrow to confirm it stacks and the compact logo appears; `q` to quit. Note observations in the report.

- [ ] **Step 6: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/view.go internal/tui/view_test.go
git commit -m "feat(tui): OpenCode-style paneled dashboard with logo and status bar"
```

---

### Task 5: Restyle the secondary screens

**Files:**
- Modify: `internal/tui/view.go` (`renderConfirmClean`, `renderRestoreList`, `renderConfirmRestore`, `renderResult`, `renderBlocked`, `renderWorking`, `renderHelp`)
- Modify: `internal/tui/view_test.go` (add secondary-screen tests)

**Interfaces:**
- Consumes: `renderLogo` (Task 2); `panel`, `statusBar`, `effectiveWidth` (Task 3); `headerStyle`, `selectedRowStyle` (Task 1); existing fields/methods.
- Produces: restyled render funcs; no new exported names. A small shared `screen(m, body, keys)` helper (below) keeps them DRY.

- [ ] **Step 1: Write the failing tests** — append to `internal/tui/view_test.go`:

```go
func TestConfirmCleanScreenStyled(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 90
	m.state = stateConfirmClean
	out := m.View()
	if !strings.Contains(out, "codexSSD") { // compact logo header
		t.Errorf("confirm screen should show the logo header; got:\n%s", out)
	}
	if !strings.Contains(out, "y yes") || !strings.Contains(out, "n no") {
		t.Errorf("confirm screen should show its keys; got:\n%s", out)
	}
}

func TestRestoreListHighlightsSelection(t *testing.T) {
	m, _ := step(New(config.Default()), loadedWithBackup())
	m.width = 90
	m.state = stateRestoreList
	m.selected = 0
	out := m.View()
	// The selected backup id should appear; no reliance on the old "> " prefix.
	if !strings.Contains(out, filepathBase(m.backups[0].Dir)) {
		t.Errorf("restore list should show the backup id; got:\n%s", out)
	}
	if !strings.Contains(out, "choose") {
		t.Errorf("restore list should show its keys; got:\n%s", out)
	}
}

func TestResultAndBlockedScreensStyled(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 90

	m.state = stateResult
	m.resultMsg = "Tidied 9.5 GiB of Codex logs aside."
	if out := m.View(); !strings.Contains(out, "Tidied 9.5 GiB") {
		t.Errorf("result screen missing its message; got:\n%s", out)
	}

	m.state = stateBlocked
	m.blockedReason = "Codex appears to be running."
	if out := m.View(); !strings.Contains(out, "Codex appears to be running.") {
		t.Errorf("blocked screen missing its reason; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/ -run 'TestConfirmClean|TestRestoreList|TestResultAndBlocked'` → FAIL.

- [ ] **Step 3: Add the shared `screen` helper and rewrite the secondary renders** in `internal/tui/view.go`:

```go
// screen frames a secondary view: compact logo header, an accent-titled card,
// and the shared status bar. keys is the status bar's left content.
func (m Model) screen(title, body, keys string) string {
	w := effectiveWidth(m)
	return strings.Join([]string{
		renderCompactLogo(w),
		"",
		panel(title, body, w),
		"",
		statusBar(keys, "watching ~/.codex", w),
	}, "\n")
}

func (m Model) renderConfirmClean() string {
	body := fmt.Sprintf("Move %s of Codex's own logs into a recoverable bin?\nNothing is deleted — you can restore them any time.",
		codex.HumanBytes(m.report.TotalBytes))
	return m.screen("Tidy Codex logs", body, "y yes · n no")
}

func (m Model) renderWorking(label string) string {
	return m.screen("CodexSSD", label, "please wait…")
}

func (m Model) renderResult() string {
	body := m.resultMsg
	if m.resultErr != nil {
		body = fmt.Sprintf("Something went wrong: %v", m.resultErr)
	}
	return m.screen("CodexSSD", body, "enter return to dashboard")
}

func (m Model) renderBlocked() string {
	return m.screen("Can't do that right now", m.blockedReason, "enter return to dashboard")
}

func (m Model) renderConfirmRestore() string {
	id := filepathBase(m.backups[m.selected].Dir)
	body := fmt.Sprintf("Move the logs in backup %s back to your Codex folder?", id)
	return m.screen("Restore backup", body, "y yes · n no")
}

func (m Model) renderRestoreList() string {
	w := effectiveWidth(m)
	var body strings.Builder
	for i, bk := range m.backups {
		var total int64
		for _, it := range bk.Manifest.Items {
			total += it.Size
		}
		row := fmt.Sprintf("%-18s %10s   releases %s", filepathBase(bk.Dir), codex.HumanBytes(total), bk.Manifest.HoldUntil.Format("2006-01-02"))
		if i == m.selected {
			row = selectedRowStyle.Render(row)
		}
		if i > 0 {
			body.WriteString("\n")
		}
		body.WriteString(row)
	}
	return strings.Join([]string{
		renderCompactLogo(w), "",
		panel("Restore a backup", body.String(), w), "",
		statusBar("↑/↓ choose · enter select · esc back", "watching ~/.codex", w),
	}, "\n")
}

func (m Model) renderHelp() string {
	body := strings.Join([]string{
		"c    tidy Codex's logs aside (recoverable)",
		"r    restore previously tidied logs",
		"?    toggle this help",
		"q    quit",
	}, "\n")
	return m.screen("CodexSSD — help", body, "? or esc to close")
}
```

Remove the now-unused `titleStyle` var and any `fmt.Fprintln`-based helpers that these replaced. Keep `footer()` (still used by the dashboard). Ensure imports stay valid (`fmt`, `strings`, `lipgloss`, `codex`, `monitor`).

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run 'TestConfirmClean|TestRestoreList|TestResultAndBlocked'` → PASS. Then `go test ./internal/tui/` → whole package green (existing update/session tests, which drive these states, still pass).

- [ ] **Step 5: Manual smoke** — `go run ./cmd/codexssd`: press `c` (confirm screen), `n`; press `r` (restore list, selection highlighted) if backups exist; `?` (help); confirm each screen shows the logo header, a titled card, and the status bar. Note in the report.

- [ ] **Step 6: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/view.go internal/tui/view_test.go
git commit -m "feat(tui): restyle confirm/restore/result/blocked/help screens to match"
```

---

## Notes for the reviewer (all tasks)

- **Behavior invariance is the top check:** diff `update.go`, `commands.go`, `model.go` — the only allowed change outside `view.go`/`styles.go`/`logo.go`/`layout.go` is the additive `Model.height` field and its assignment. Any keybinding, state, or command change is a reject.
- **No new module in `go.mod`** (termenv un-indirecting is fine).
- **Every user-facing string** that existed before still appears (folder path, sizes, risk, running state, memory, bin summary, banner text, all screen prompts, all key hints).
- **Color-free readability:** under the ascii test profile the suite passes, proving nothing depends on color to convey meaning.
