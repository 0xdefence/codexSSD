# Design: TUI Overhaul — OpenCode-style dashboard with ASCII art

**Date:** 2026-07-05
**Status:** Approved by product owner (pre-implementation)
**Branch base:** `staging`

## Goal

Reskin CodexSSD's interactive dashboard (`internal/tui`) to look and feel like
OpenCode: a large ASCII-art wordmark, a cyan/teal brand accent, bordered panels,
and a styled status bar — while keeping every state, keybinding, command, and
safety gate **byte-for-byte identical**. This is a pure presentation change:
only `View()` rendering and the styles/layout it uses change.

## Decisions locked in

| Decision | Choice |
|---|---|
| Scope | Full reskin **+** panel layout + status bar. Same states/keys/behavior. No separate splash screen. |
| Logo | ANSI-shadow block wordmark **CODEX** with a `SSD · the disk watchdog` subtitle; width-aware. |
| Brand accent | Cyan / teal. |
| Risk colors | Semantic, independent of the accent: LOW green, MEDIUM yellow, HIGH orange, CRITICAL red. |
| Terminals | Adaptive light/dark; `NO_COLOR` and non-truecolor degrade cleanly. |

## Non-negotiable constraints

From `CLAUDE.md` and the repo conventions:

- **No new third-party dependencies.** Only `bubbletea` + `lipgloss` (already
  direct deps). The charmbracelet-only-in-`internal/tui` boundary stays intact;
  engine packages remain stdlib-only. `go.mod` must not gain a `require` line.
- **No behavior change.** No state added/removed/renamed, no keybinding changed,
  no command touched, no safety gate altered. `internal/tui/update.go` control
  flow, `commands.go`, and all non-`tui` packages are untouched except the one
  additive `Model.height` field and its assignment.
- **Friendly, plain-language text preserved.** The tool is for non-technical
  users; wording stays warm and clear.
- **Readable without color.** Every signal that uses color (risk level, banner)
  also carries a text label or glyph, so `NO_COLOR` terminals lose nothing.
- Gate before done: `go build ./... && go vet ./... && go test ./... && gofmt -l .`
  (gofmt output empty).

## Architecture

Four files in `internal/tui`; the split keeps each unit focused and testable.

### `styles.go` (new) — the design system

A single source of truth for colors and Lip Gloss styles, so every render shares
them.

- **Palette** (package-level `lipgloss.AdaptiveColor` / `lipgloss.Color`):
  - `accent` — cyan/teal (e.g. adaptive `{Light: "#0891b2", Dark: "#22d3ee"}`).
  - `muted` — dim gray for secondary text/subtitles.
  - `fg` — default foreground (adaptive).
  - Risk colors: `riskGreen`, `riskYellow`, `riskOrange`, `riskRed`.
- **Styles** (package-level `lipgloss.Style` vars):
  - `logoStyle` (accent, bold), `subtitleStyle` (muted).
  - `panelStyle` — `RoundedBorder`, accent border, padding `0 1`.
  - `panelTitleStyle` — accent, bold (rendered into the top border via a titled
    panel helper).
  - `headerStyle` (accent bold), `labelStyle`, `valueStyle`.
  - `statusBarStyle` — full-width, accent background, contrasting fg.
  - `keyStyle` / `keyDescStyle` — for the keybinding hints.
  - `selectedRowStyle` — accent background for the restore-list cursor row.
- **`riskStyle(level monitor.Risk) lipgloss.Style`** — maps a level to its
  semantic color + bold. Pure, table-tested.
- **`riskGlyph(level monitor.Risk) string`** — returns `●` (colored via
  riskStyle by the caller). LOW still shows the glyph so the layout is stable.
- **Profile handling:** the package does NOT force a profile in production
  (Lip Gloss auto-detects and honors `NO_COLOR`). Tests set the ascii profile
  (see Testing).

### `logo.go` (new) — the ASCII wordmark

- `blockLogo` — a `const`/`var` string: the 6-row ANSI-shadow **CODEX** art.
- **`renderLogo(width int) string`** — returns the logo block, horizontally
  centered within `width`, styled with `logoStyle`, plus the `subtitleStyle`
  subtitle line `SSD · the disk watchdog` centered beneath. If `width` is less
  than the art's natural width (~42) it returns a **compact** fallback: the
  accent-bold string `codexSSD` centered, with the same subtitle. Callers always
  pass a resolved, positive width (see `effectiveWidth`), so `renderLogo` need
  not special-case zero/negative width.
- Pure and deterministic — unit-tested for both the wide and narrow branches.

### `view.go` (restructured) — panels & composition

Keeps the existing `View()` state switch and the `bannerState()` logic verbatim.
Each `render*` function is rewritten to compose styled panels.

- **`effectiveWidth(m Model) int`** — returns `m.width` if > 0, else `80`
  (before the first `WindowSizeMsg`). Single helper used by every render so
  width handling is consistent.
- **`panel(title, body string, width int) string`** — wraps `body` in
  `panelStyle` with `title` styled into the top border. The one place border
  drawing lives.
- **`statusBar(keys, status string, width int) string`** — renders the
  full-width bottom bar: `keys` left-aligned, `status` right-aligned, padded to
  `width` with `statusBarStyle`.
- **`renderDashboard`** — composes: centered logo → a top row of two panels
  (`Codex folder`/logs on the left, `Risk`/process/memory on the right) joined
  with `lipgloss.JoinHorizontal` when `effectiveWidth >= 72`, else stacked with
  `JoinVertical` → a full-width `Recycling bin` panel → the banner line
  (`bannerState()` unchanged) → the status bar. All existing facts are shown:
  folder path, per-file sizes, total, risk level/rate/WAL/reason, running state,
  memory (when `m.running && m.memBytes > 0`), bin summary + next release,
  `releaseNote`, and the `watching ~/.codex · updates every 30s` text (moved
  into the status bar's right side).
- **`renderConfirmClean`, `renderRestoreList`, `renderConfirmRestore`,
  `renderResult`, `renderBlocked`, `renderWorking`, `renderHelp`** — each: a
  compact logo header (single `logoStyle` `codexSSD` line, not the full block,
  to save vertical space), an accent-titled `panel` card with the same text as
  today, and the shared `statusBar` with that screen's keys. `renderRestoreList`
  highlights the selected row with `selectedRowStyle` instead of the `> ` prefix
  (a non-selected row is plain, keeping column alignment).

### `model.go` (one additive change)

- Add `height int` to `Model` (next to `width`).
- In `update.go`, the existing `tea.WindowSizeMsg` case sets `m.height =
  msg.Height` alongside the existing `m.width = msg.Width`. No other change.

## Layout reference (wide terminal)

```
              [ CODEX block logo, cyan, centered ]
               SSD · the disk watchdog

┌─ Codex folder ─────────────────┐  ┌─ Risk ───────────────┐
│ ~/.codex                       │  │ ● LOW                │
│ logs_2.sqlite         142 MiB  │  │ 0 MB/min · WAL 0 B    │
│ logs_2.sqlite-wal     9.4 GiB  │  │ Codex: not running    │
│ Total                 9.5 GiB  │  │ memory: 1.2 GiB       │
└────────────────────────────────┘  └───────────────────────┘
┌─ Recycling bin ──────────────────────────────────────────┐
│ 2 backups · last tidy 2026-07-04 · next release 07-18    │
└───────────────────────────────────────────────────────────┘
  ⚠  9.5 GiB of Codex logs piled up — press c to tidy

 c tidy · r restore · ? help · q quit      watching ~/.codex · 30s
```

Below ~72 cols the two top panels stack vertically and the logo uses the compact
fallback.

## Data flow

Unchanged. `View()` is still a pure function of `Model`. The new render helpers
read only fields already on `Model` (plus the new `height`). No I/O, no new
messages, no new commands.

## Error handling

- `loadErr` / `DirExists == false` paths keep their existing messages, now shown
  inside a panel with the status bar.
- Rendering never panics on unusual sizes: `effectiveWidth` floors width; panels
  clamp to available width; the logo has a narrow fallback.

## Testing strategy

- **Profile:** a `TestMain` in the `tui` package sets
  `lipgloss.SetColorProfile(termenv.Ascii)` so `View()` output is deterministic
  plain text — no ANSI escapes in assertions. `termenv` is already a transitive
  dependency, so this imports no new module (it may drop termenv's `// indirect`
  marker in `go.mod`, which is an annotation change, not a new `require`). Under
  `go test`, stdout is not a TTY so Lip Gloss already defaults to no-color; the
  explicit profile set only makes this deterministic rather than environment-
  dependent. If the reviewer prefers zero `go.mod` churn, rely on the non-TTY
  default instead and drop the explicit set.
- **`logo_test.go`:** `renderLogo(100)` contains the block art (assert a known
  row substring) and the subtitle; `renderLogo(40)` returns the compact
  `codexSSD` + subtitle and does NOT contain the block art.
- **`styles_test.go`:** `riskStyle` returns a distinct style per level (assert
  via `.GetForeground()` or by rendering a token under a color profile and
  checking they differ); under the ascii profile a styled string renders as its
  plain content.
- **`view_test.go` (new):** a dashboard smoke test — feed a `loadedMsg`, render
  `View()`, assert the folder path, a file size, the total, the risk label, and
  a keybinding (`c tidy`) all appear as plain text; assert narrow width
  (`m.width = 50`) still renders without panic and includes the compact logo.
- **Existing `update_test.go` / `session_test.go`:** unchanged; must still pass
  (they assert on model state and plain text, which the ascii profile preserves).

## Out of scope

- No splash/landing screen (dashboard is the entry).
- No new keybindings, themes selector, mouse support, or animation.
- No changes outside `internal/tui` except nothing — even `main.go` is untouched.
- No configurable color themes (single built-in cyan accent this pass).

## Execution

Subagent-driven (implementer + staff-engineer reviewer per task), as with the
prior sprint. Likely task split: (1) `styles.go`, (2) `logo.go`, (3) `view.go`
dashboard + `model.go`/`update.go` height wiring, (4) secondary screens, (5)
tests + profile harness — with the reviewer verifying no behavior/keybinding
drift and no new deps at each step.
