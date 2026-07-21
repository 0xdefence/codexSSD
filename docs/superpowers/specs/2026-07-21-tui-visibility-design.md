# TUI Visibility: Surfacing Tracked Data in the Dashboard

## Summary

The interactive TUI at `internal/tui` currently only surfaces a subset of what
CodexSSD tracks — it imports `cleaner`, `codex`, `config`, `monitor`, and
`recorder`, but never `visibility`, `self`, or `notify`. As a result, the
dashboard shows Codex's log folder and risk reading, but the user has no way
to see CodexSSD's own footprint, a full disk breakdown of `~/.codex`, their
configured thresholds, the complete set of reasons behind a risk reading, or
to get a desktop notification when risk escalates while the dashboard is
open. This spec scopes four components — dashboard rendering fixes, a new
read-only Info screen, dashboard-driven desktop notifications, and the
corresponding tests — that wire these already-existing, already-tested
packages into `internal/tui` without adding any new engine logic.

## Component 1: Dashboard enhancements (small, in-place changes)

These are pure `view.go` rendering changes plus reading two already-tracked
fields. No new state, no new keybindings, no new screens, no new Bubble Tea
messages.

**a) Full risk reasons.** `internal/tui/view.go:93-94` currently renders only
`m.assessment.Reasons[0]`:

```go
if len(m.assessment.Reasons) > 0 {
    reason = " · " + m.assessment.Reasons[0]
}
```

Change to join all reasons instead of just the first:

```go
reason = " · " + strings.Join(m.assessment.Reasons, " · ")
```

No new state — `Assessment.Reasons` is already on the model. Panel bodies
already support multi-line text (see the "Codex folder" panel in
`renderDashboard`).

**b) Live config values.** Two fixes:

  i. The footer string `"watching ~/.codex · updates every 30s"`
     (`internal/tui/view.go:145`) must derive from `m.cfg.PollInterval()`,
     formatted friendly (e.g. "every 30s", "every 1m") instead of being
     hardcoded.

  ii. The full threshold values (MB/min tiers, WAL tiers, mem tiers,
      bin-hold days, stale-after days, notifications on/off) do NOT belong
      on the compact dashboard — they go in the new Info screen's
      "Settings" panel (Component 2).

**c) Session peaks.** Add a line to the existing "Risk" panel in
`renderDashboard`: once `m.startedAt` is non-zero, show
`session peak: <peakRisk> · <peakRate MB/min>` under the current risk
reading. This reads fields (`peakRisk`, `peakRate`) already tracked on
`Model` (`internal/tui/model.go:75-76`) for the quit-time receipt — no new
tracking logic, just new rendering.

## Component 2: New Info screen (self-footprint + disk visibility + config settings)

**Trigger:** new key `i` on the dashboard (`c` tidy, `r` restore, `?` help,
`q` quit are already taken in `internal/tui/update.go`'s
`handleDashboardKey`/global switch). Footer becomes
`"c tidy · r restore · i info · ? help · q quit"`, and `renderHelp()` gets an
`i` line.

**Navigation:** new `stateInfo` constant (in the existing `state` enum in
`internal/tui/model.go:33-42`), following the exact pattern `stateRestoreList`
already uses — `esc` returns to the dashboard, and returning triggers a
refresh (matching how `stateResult`/`stateBlocked` re-trigger `loadCmd` on
return, see `internal/tui/update.go:144`, `case stateResult, stateBlocked,
stateError`). Data is fetched lazily on entry: pressing `i` fires a new
`infoCmd` (a `tea.Cmd`, non-blocking, following the seam-based pattern in
`internal/tui/commands.go`) that returns an `infoMsg{self.Report,
visibility.Report}`. The screen shows "loading…" for the one tick until it
arrives (same pattern as `renderWorking`, `internal/tui/view.go:172`).

**Data sourced** (read-only, just wiring existing packages into the TUI — no
new engine logic):

- `self.Measure(recorder.Dir())` → mode, state dir, history bytes, record
  count, last action (see `internal/self/report.go`)
- `visibility.Scan(codexDir, time.Now(), m.cfg.StaleAfter())` → per-entry
  breakdown of `~/.codex` with stale flags (see `internal/visibility/scan.go`,
  signature `Scan(dir string, now time.Time, staleAfter time.Duration)
  Report`)
- Config settings pulled straight off `m.cfg`: all threshold fields,
  `BinHoldDays`, `Notifications` on/off, `PollIntervalSeconds` (see
  `internal/config/config.go`)

**Layout:** three stacked panels via the existing `panel()` helper
(`internal/tui/layout.go:36`, same technique `renderDashboard` uses), full
width, in this order:

1. **Settings** — raw config values as plain key/value lines.
2. **CodexSSD's own footprint** — self-report fields.
3. **Disk report (`~/.codex`)** — visibility entries: name, size, file
   count, newest-mod date, `STALE` tag where flagged, `⚠` where
   `ReadError != nil`.

**Scoped to Codex only** (matching the rest of the TUI today) — no `--tool`
switcher in this screen; that is explicitly out of scope / future work.

## Component 3: Desktop notifications from the dashboard

Reuses `internal/notify.Notify` exactly as `cmd/codexssd/watch.go` does — no
new notification package or logic.

**Trigger condition,** matching `watch.go`'s actual logic
(`a.Level >= monitor.RiskHigh && a.Level > last`, see
`cmd/codexssd/watch.go:206`): when a fresh `loadCmd` result's
`Assessment.Level` reaches HIGH or CRITICAL AND is higher than the
previously-displayed level. This comparison must happen in `Update()` at the
point where the new assessment is about to overwrite `m.assessment` — the
old value is still in scope there, so no new tracking field is needed beyond
what already exists.

**Firing:** wrapped as its own `tea.Cmd` (a goroutine Bubble Tea dispatches
and forgets, same shape as the existing `releaseCmd`/`loadCmd` pattern in
`internal/tui/commands.go`) — the result message is discarded either way.
This guarantees notification latency/failure can never block the dashboard's
render loop (CLAUDE.md safety rule 6: notifications are fire-and-forget).

**Respecting config:** gated on `m.cfg.Notifications` (same field
`watch --no-notify` respects). If a platform returns
`notify.ErrUnsupported`, it is silently swallowed, same as `watch`. No visual
change on the dashboard itself — this is a background side-effect wired into
the existing update loop.

## Component 4: Testing & verification

- **Reasons/config/peaks rendering:** extend the existing view-rendering
  test style with cases — multiple reasons all present in output; footer
  reflects a non-default `PollInterval`; peak line appears once
  `startedAt` is set and matches `peakRisk`/`peakRate`.
- **Info screen:** unit test `infoCmd` against seams (mirroring how
  `scanLogs`/`isCodexRunning` are overridden in `commands.go` tests) so
  `self.Measure`/`visibility.Scan` are faked with `t.TempDir()`, no real
  `~/.codex` touched. Test the loading→loaded transition and `esc`
  navigation back to dashboard.
- **Notifications:** unit test the escalation-comparison logic as a pure
  function (old level, new level) → bool; then a thin test that the
  `tea.Cmd` is only returned when that's true, with `notify.Notify` swapped
  via a package-level seam (matching the `notifier` pattern in
  `cmd/codexssd/watch.go:91-93`).
- **Full suite gate:** `go build ./... && go vet ./... && go test ./... &&
  gofmt -l .` must be clean — this is a CLAUDE.md requirement, not optional.

## Branch/workflow note

This work happens on `feat/tui-visibility`, cut from
`feat/phase3-4-multi-tool`. It stays scoped to `internal/tui` (reading, never
modifying, `internal/self`/`internal/visibility`/`internal/notify`). Once
implemented and verified, this is intended to be pushed and opened as a PR
against `staging` — but pushing/PR creation requires separate human
confirmation and is explicitly OUT OF SCOPE for whoever implements this
spec.
