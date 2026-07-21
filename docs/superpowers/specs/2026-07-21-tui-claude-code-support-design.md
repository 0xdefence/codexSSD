# TUI Round 3: Claude Code Support

## Summary

The TUI dashboard (`internal/tui`) has been Codex-only since it was built.
CodexSSD's CLI already supports Claude Code as a second tool profile end to
end — `status`, `report`, `clean`, `restore`, and `prune --tool claude` all
work today — but the interactive dashboard never surfaced it. This round adds
full Claude Code visibility AND actions (tidy/restore) to the TUI, reusing the
CLI's already-generic, already-safety-tested engine functions
(`cleaner.PlanTool`, `cleaner.ListBackups`, `cleaner.Restore` all already
accept/resolve a `tool.Profile` — confirmed by reading the actual code, not
assumed) rather than adding new engine logic. This is a pure TUI-layer change.

## Component 1: Architecture overview

The model stays a single flat struct — Codex's existing fields (`report`,
`running`, `supported`, `runErr`, `plan`, `backups`, `processes`, `memBytes`,
etc. in `internal/tui/model.go`) are UNTOUCHED. A parallel set of
`claude`-prefixed fields is added alongside:

```go
claudeDir       string
claudeLoadErr   error
claudeRunning   bool
claudeSupported bool
claudeRunErr    error
claudeProcesses []tool.Process
claudeCleanable []tool.FoundFile
claudePlan      cleaner.Plan
claudeBackups   []cleaner.Backup
```

Four new states in the `state` enum (`internal/tui/model.go`), fully
additive: `stateClaude` (the new main Claude screen), `stateClaudeConfirmClean`,
`stateClaudeRestoreList`, `stateClaudeConfirmRestore`. Each renders directly
from the `claude*` fields — no branching flag needed, since each state already
implies which tool it's acting on.

Two small, deliberate generalizations to existing shared code, so
`stateCleaning`, `stateRestoring`, `stateResult`, `stateBlocked`, `stateError`
(the "please wait" and message screens, whose render functions in `view.go`
— `renderWorking`, `renderResult`, `renderBlocked` — take no tool-specific
data today, just already-generic strings/fields: confirmed by reading
`view.go`, e.g. `renderResult` reads only `m.resultMsg`/`m.resultErr` and
`renderBlocked` reads only `m.blockedReason`) can be reused for Claude instead
of being duplicated four times over:

- New `Model.returnState state` field. Set immediately before dispatching any
  clean/restore action (both Codex's existing dispatch sites AND the new
  Claude ones), telling the shared result/blocked/error screens whether
  `esc`/`enter` should transition back to `stateDashboard` (+ trigger
  `loadCmd`) or `stateClaude` (+ trigger `loadClaudeCmd`) — a simple 2-way
  branch since there are only ever two tools (`tool.All()` returns exactly
  `[]Profile{Codex(), Claude()}`, confirmed in `internal/tool/profile.go`).
  The existing Codex dispatch sites — inside `handleKey`'s
  `stateConfirmClean`/`stateConfirmRestore` "y" cases (`internal/tui/update.go`,
  currently `m.state = stateCleaning; return m, cleanCmd(...)` and
  `m.state = stateRestoring; return m, restoreCmd(...)`), and
  `handleDashboardKey`'s "c"/"r" blocked/empty-result early-outs (three sites
  under "c": not-supported, running, empty-plan; one site under "r":
  empty-backups) — must set `m.returnState = stateDashboard` explicitly,
  reproducing today's behavior byte-for-byte. This is the one place existing
  code gets touched, so it must not change observable behavior for the Codex
  path at all.
- New `Model.workingLabel string` field, consulted by `view.go`'s
  `stateCleaning`/`stateRestoring` rendering (currently hardcoded literals
  `"Tidying Codex logs aside…"` and `"Restoring…"` passed straight into
  `renderWorking(label)` from the `View()` switch — confirmed at
  `internal/tui/view.go`'s `View()` method) instead of the hardcoded text.
  Existing Codex dispatch sites set this field to the exact current literal
  strings (byte-for-byte preserved); new Claude dispatch sites set it to
  `"Tidying Claude Code's stale files aside…"` / `"Restoring…"`.

Every Claude-side mutating action re-checks `tool.IsRunning(tool.Claude())`
immediately before acting — its own fully independent safety gate, completely
separate from Codex's existing `isCodexRunning()` checks in
`cleanCmd`/`restoreCmd`, which are NEVER modified.

## Component 2: Dashboard — compact Claude Code panel

New seams in `internal/tui/commands.go` (following the existing seam-variable
pattern, e.g. `codexDir = codex.Dir`):

```go
claudeDir             = func() (string, error) { return tool.Claude().Dir() }
isClaudeRunning       = func() (bool, error) { return tool.IsRunning(tool.Claude()) }
claudeCleanablePaths  = func(dir string, now time.Time, staleAfter time.Duration) []tool.FoundFile {
    return tool.Claude().CleanablePaths(dir, now, staleAfter)
}
detectClaudeProcesses = func() ([]tool.Process, error) { return tool.DetectProcesses(tool.Claude()) }
```

New `loadClaudeCmd` (sibling to the existing `loadCmd`, same best-effort/
never-blocks style) gathers: `claudeDir()`, `isClaudeRunning()`,
`claudeCleanablePaths(dir, time.Now(), cfg.StaleAfter())` (populates the
`claudeCleanable []tool.FoundFile` field, and is summed into a total for the
dashboard line), `detectClaudeProcesses()`, and `cleaner.ListBackups(dir)` —
reusing the EXISTING `listBackups` seam directly (it already just wraps
`cleaner.ListBackups`, which is tool-agnostic — it scans whatever directory
it's given for a `codexssd-backups` folder, no Codex-specific logic in it at
all; confirmed in `internal/cleaner/restore.go`).

**Gap found during verification:** the `claudePlan cleaner.Plan` field
declared in Component 1 (needed by Component 4's `claudePlan.Empty()`,
`claudePlan.TotalBytes`, and `applyPlan(claudePlan, hold)`) is not populated
by any of the calls listed above — `claudeCleanablePaths` returns
`[]tool.FoundFile`, not a `cleaner.Plan`. `loadClaudeCmd` must ALSO compute it
via a new seam mirroring the existing `planLogs = cleaner.PlanCodexLogs`:

```go
planClaudeLogs = func(dir string, staleAfter time.Duration) (cleaner.Plan, error) {
    return cleaner.PlanTool(tool.Claude(), dir, time.Now(), staleAfter)
}
```

This mirrors how the CLI itself already keeps the two calls separate and
independent: `cmdStatus` in `cmd/codexssd/main.go` calls
`p.CleanablePaths(...)` directly for display, while `cmdClean` calls
`cleaner.PlanTool(...)` separately to build the actual plan (confirmed at
`cmd/codexssd/main.go` lines ~141 and ~312-314) — the two calls scan the same
underlying files but serve different callers (raw listing vs. an
apply-ready `Plan` with `BackupRoot`/`Tool` set), so both are gathered by
`loadClaudeCmd` in the same best-effort style, and both fields end up
reflecting the same underlying stale-file set on every load.

`loadClaudeCmd` is dispatched alongside `loadCmd` from `Init()`
(`tea.Batch(loadCmd, loadClaudeCmd, tickCmd(m.cfg.PollInterval()), releaseCmd)`)
and on every regular dashboard tick refresh, so the Claude panel stays live on
the same cadence as everything else — no separate polling loop.

New dashboard panel (`internal/tui/view.go`'s `renderDashboard`), full-width,
placed after the existing "Recycling bin" panel and before the deadweight
banner line: titled `"Claude Code"`. One line, mirroring round 2's
PID-inclusive style for the Codex Risk panel: `"~/.claude · 3 stale file(s),
12.4 MiB cleanable · not running"`, or `"running (PID 4321)"` / `"running (2
processes, PIDs 4321, 5678)"` when running (same singular/plural phrasing
`formatRunningProcesses` from round 2 already established).

**Correction found during verification:** the spec as originally described
assumed `formatRunningProcesses(procs []codex.Process) string`
(`internal/tui/view.go`) could likely be reused as-is because `codex.Process`
is a genuine type alias for `tool.Process` (`type Process = tool.Process`,
confirmed in `internal/codex/process.go`), so a `[]tool.Process` value is
directly assignable — that type-compatibility claim is correct. However, the
function's actual return value is not just the PID fragment: it hardcodes a
`"Codex: "` prefix (`"Codex: running (PID %d)"` / `"Codex: running (%d
processes, PIDs %s)"`), which does not fit the Claude panel's line format
above (`"running (PID 4321)"`, appended inline after `"~/.claude · ... · "`
with no tool-name prefix — the panel title already says "Claude Code"). The
blocker to direct reuse is the hardcoded string content, not the type
signature. The implementer should either refactor
`formatRunningProcesses` to take a label parameter (e.g. `formatRunningProcesses(label string, procs []tool.Process) string`, called as
`formatRunningProcesses("Codex", m.processes)` and
`formatRunningProcesses("", m.claudeProcesses)` with empty-label formatting
dropping the `"Codex: "`-style prefix entirely), or write a small Claude-
specific equivalent that emits just the `"running (PID %d)"` /
`"running (%d processes, PIDs %s)"` fragment. Either is acceptable; the
existing Codex dashboard line's rendered text must not change.

A load error or missing `~/.claude` dir renders a short one-line explanation,
matching the tone of the existing Codex-folder-panel's own error path
(`"Could not read Codex's folder: %v"`). Footer gains `l` claude alongside
`c`/`r`/`i`/`?`/`q` (current footer: `"c tidy · r restore · i info · ? help ·
q quit"`, confirmed in `internal/tui/view.go`'s `footer()`), and
`renderHelp()` gets a line for it.

## Component 3: The Claude screen (`l` key)

Pressing `l` from `stateDashboard` → `stateClaude`, dispatching a FRESH
`loadClaudeCmd` (so the detail screen is never stale relative to when it was
opened) and showing "loading…" until the result arrives — same pattern as the
existing Info screen's `infoCmd`/`infoMsg` lazy-load (`internal/tui/commands.go`'s
`infoCmd`, `internal/tui/view.go`'s `renderInfo`'s loading-state branch,
gated on `m.infoLoaded`).

Layout via the existing `panel()` helper, STACKED full-width (matching the
Info screen's existing stacked-panel style, NOT side-by-side like the main
dashboard's two-column treatment):

- **"Claude Code"** panel — the directory, then either `"Nothing stale to
  report right now — no cleanable Claude Code files were found."` (mirroring
  the CLI's `printToolStatus` tone in `cmd/codexssd/main.go`) or an itemized
  list of every `tool.FoundFile` in `claudeCleanable` (name, size) with a
  total, plus a one-line note that fresh session files are deliberately
  excluded because they're still in use (same substance as
  `printToolStatus`'s existing note: `"Fresh Claude Code session files aren't
  listed here on purpose: they're still in use (for example, they power
  \"claude --resume\")."` — the TUI note can be a shorter paraphrase; it does
  not need to reproduce the CLI's follow-on sentence about running `codexssd
  clean --tool claude`, which doesn't apply inside the TUI).
- **"Recycling bin"** panel (Claude's own) — same format as the dashboard's
  existing Codex recycling-bin line: backup count, last-tidy date, soonest
  release date, computed from `claudeBackups` (reuse or mirror the existing
  `lastTidy()`/`soonestRelease()` model-method pattern in
  `internal/tui/model.go`, parameterized or duplicated for `claudeBackups`).

Footer: `"c tidy · r restore · esc back"`. `esc` → `stateDashboard`
unconditionally (this screen never mutates anything itself, nothing to guard
on the way out).

## Component 4: Claude tidy flow (safety-critical)

From `stateClaude`, pressing `c`:

- If `!claudeSupported` → `stateBlocked`, `m.returnState = stateClaude`,
  reason: `"This platform can't verify Claude Code is closed, so tidying is
  disabled here."`
- If `claudeRunning` → `stateBlocked`, `m.returnState = stateClaude`, reason:
  `"Claude Code appears to be running. Close it first, then try again."`
- If `claudePlan.Empty()` → `stateResult`, `m.returnState = stateClaude`,
  message: `"Nothing to tidy — no stale Claude Code files are present."`
- Otherwise → `stateClaudeConfirmClean`.

`stateClaudeConfirmClean` renders: `"Move {size} of Claude Code's stale
session files into a recoverable bin?\nNothing is deleted — you can restore
them any time."` from `claudePlan.TotalBytes` (using `codex.HumanBytes`,
which is tool-agnostic despite living in the `codex` package — confirmed it
is a plain `int64 -> string` formatter with no Codex-specific logic).

`y` → re-checks `isClaudeRunning()` AGAIN right here (the authoritative gate
— mirrors the existing `cleanCmd`'s own re-check pattern exactly in
`internal/tui/commands.go`, never trusts the state captured when the screen
was entered), then dispatches a new `claudeCleanCmd(hold)` — a near-duplicate
of the existing `cleanCmd` (same shape: re-check running → refuse via
`blockedMsg` or apply via the EXISTING generic `applyPlan(claudePlan, hold)`
seam, which is already tool-agnostic since `Plan.ApplyWithHold` doesn't care
which tool produced the plan). Sets `m.returnState = stateClaude`,
`m.workingLabel = "Tidying Claude Code's stale files aside…"`, `m.state =
stateCleaning`. Reuses the EXISTING `cleanResultMsg`/`blockedMsg` message
types unchanged — they already carry generic dest/bytes/err data, nothing
Codex-specific (confirmed in `internal/tui/commands.go`).

`n`/`esc` → back to `stateClaude` (not `stateDashboard`).

## Component 5: Claude restore flow (safety-critical)

From `stateClaude`, pressing `r`:

- If `claudeBackups` is empty → `stateResult`, `m.returnState = stateClaude`,
  message: `"No Claude Code backups to restore — nothing has been tidied
  yet."`
- Otherwise → `m.selected = 0`, `stateClaudeRestoreList`.

`stateClaudeRestoreList` renders `claudeBackups` in the exact same row format
as the existing Restore List screen (`id · size · releases <date>`, confirmed
in `internal/tui/view.go`'s `renderRestoreList`), same `↑`/`↓`/`enter`/`esc`
handling — reuses `m.selected` as the cursor (safe to share the single field
since only one restore flow is ever active on screen at a time; no risk of
cross-contamination between Codex's and Claude's restore-list cursors).

`enter` → `stateClaudeConfirmRestore`, rendering the selected
`claudeBackups[m.selected]`'s id plus every `ManifestItem`'s `OriginalPath`
(confirmed field name in `internal/cleaner/apply.go`'s `ManifestItem`) —
same per-file listing format round 2 already added to Codex's
`renderConfirmRestore` (confirmed in `internal/tui/view.go`) — mirror that
exact rendering, worded for Claude Code: `"Move the files in backup <id> back
to your Claude Code folder?"`.

`y` → re-checks `isClaudeRunning()` AGAIN right here (same authoritative-gate
pattern as the existing `restoreCmd`), then dispatches a new
`claudeRestoreCmd(dir)` — a near-duplicate of the existing `restoreCmd`,
reusing the EXISTING generic `restoreBackup` seam (`cleaner.Restore`, which
ALREADY resolves the correct profile/allow-list from the backup's own
manifest's `Tool` field via `profileFor` internally — confirmed by reading
`internal/cleaner/restore.go` (`Restore` calls `profileFor(m.Tool)`, then
`prof.Allows(toolDir, it.OriginalPath)` for every item before moving anything)
and `internal/cleaner/clean.go`'s `profileFor` (empty string means codex,
legacy manifests) — so there is no way to accidentally restore a Claude
backup under Codex's allow-list or vice versa; safety here comes from the
manifest, not from which TUI seam function got called). Sets `m.returnState =
stateClaude`, `m.workingLabel = "Restoring…"`, `m.state = stateRestoring`.

`n`/`esc` → back to `stateClaudeRestoreList` or `stateClaude` respectively
(matching the existing Codex restore flow's exact esc semantics at each
corresponding step).

## Component 6: Testing & verification

- Dashboard panel: populated cleanable summary, "nothing stale" case,
  running-state text (with/without PID, singular/plural), load-error case.
- Claude screen: load→loaded transition (faked `claudeDir`/`isClaudeRunning`/
  `claudeCleanablePaths`/`planClaudeLogs`/`detectClaudeProcesses`/
  `listBackups` seams, `t.TempDir()`-backed, no real `~/.claude` touched),
  `esc` back to dashboard, itemized cleanable-file listing renders correctly,
  "nothing stale" message.
- Claude tidy flow: blocked-when-running, blocked-when-unsupported,
  empty-plan result message, confirm-clean renders correct total/wording,
  `claudeCleanCmd` re-checks running immediately before applying (test a
  run-state flip between screen-entry and confirm is caught), successful
  clean transitions through `stateCleaning` (verify `workingLabel` is the
  Claude-specific text) to `stateResult`, and verify `returnState` sends
  `esc`/`enter` back to `stateClaude` (NOT `stateDashboard`) with a Claude
  reload (`loadClaudeCmd`), not a Codex one (`loadCmd`).
- Claude restore flow: empty-backups result message, restore-list renders
  `claudeBackups`, confirm-restore lists `OriginalPath`s, restore re-checks
  running immediately before restoring, successful restore returns to
  `stateClaude` via `returnState`.
- Existing Codex flow regression check: explicit tests (or re-verification of
  existing ones) confirming `returnState`/`workingLabel` defaulting at the
  EXISTING Codex dispatch sites reproduces today's exact behavior/text
  byte-for-byte — this is the one place existing code gets touched, so it is
  the one place regression risk actually lives, and it needs deliberate test
  coverage, not just "existing tests still pass by accident."
- Gate (mandatory, from CLAUDE.md): `go build ./... && go vet ./... && go
  test ./... && gofmt -l .` must be clean.
- Manual verification (documented as an expectation in the spec, to be
  performed by whoever verifies the implementation, not necessarily the
  implementer): drive the built binary in tmux, exercise `l`, the Claude
  screen, and navigate into (but do NOT confirm) an actual Claude tidy/
  restore against whatever real `~/.claude` state exists on the test
  machine, to avoid mutating real data during verification — mirroring
  exactly how rounds 1–2 were manually verified.

## Branch/workflow note

This work happens on `feat/tui-claude-code-support`, cut from
`origin/staging` (which includes rounds 1 and 2, merged via PR #15 and PR
#16). Stays scoped to `internal/tui` only — no changes to `internal/tool`,
`internal/cleaner`, `internal/codex`, or `cmd/codexssd`, since every
engine-level function this round needs (`cleaner.PlanTool`,
`cleaner.ListBackups`, `cleaner.Restore`, `tool.Claude()`, `tool.IsRunning`,
`tool.DetectProcesses`, `Profile.CleanablePaths`) already exists and is
already safety-tested. Once implemented and verified, this is intended to be
pushed and opened as a PR against `staging` — but pushing/PR creation
requires separate human confirmation and is explicitly OUT OF SCOPE for
whoever implements this spec.
