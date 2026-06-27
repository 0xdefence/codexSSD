# Background Watch Loop — Design

**Date:** 2026-06-26
**Status:** Approved design — PARKED (plan written, not yet executed)
**Slice:** Make the interactive dashboard live: low-write polling + a passive deadweight banner.

## Why

The dashboard (merged) shows Codex log status once, on open. The product vision is
that `codexssd` *sits there and notices* — so the dashboard should stay live and
raise the tidy question on its own when deadweight builds, without ever hijacking
the screen.

## Decisions (settled in brainstorming)

- **Passive banner**, not an auto-popup or terminal bell. The app never grabs the
  screen; it shows a prominent, persistent alert line and the user acts when they
  look. Calmer; fits "a tool you trust near your machine."
- **Fixed 30s poll cadence.** Simple, predictable, lightweight (read-only `os.Stat`).
- All in `internal/tui`. The heavier process-write-rate risk engine remains a
  future `internal/monitor` job and is NOT touched here.

## Behavior

While the dashboard is open, every 30 seconds it re-reads `~/.codex` (the existing
read-only `loadCmd`) and updates the display. The deadweight banner is a pure,
stateless function of the current state:

- deadweight present **and Codex idle** → **actionable**:
  `⚠ <size> of Codex logs piled up — press c to tidy`
- deadweight present **and Codex active** → **informational** (quiet while active):
  `⚠ <size> piling up — I'll offer to tidy when Codex is closed` (no `c` hint —
  tidy is blocked while Codex runs anyway)
- no deadweight → calm: `Nothing alarming right now`

A small live indicator (`watching ~/.codex · updates every 30s`) makes it clear the
app is actively keeping watch.

Because the banner is passive, there is **no snooze / re-prompt state** to track —
it is simply a function of the current numbers.

## Architecture

Idiomatic Bubble Tea self-rescheduling tick:

- `Init` returns `tea.Batch(loadCmd, tickCmd)`.
- `tickCmd` = `tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })`.
- On `tickMsg`: re-dispatch `loadCmd` and reschedule (`tea.Batch(loadCmd, tickCmd())`).
- `loadedMsg` only updates the data fields, never `m.state`, so a refresh landing
  while the user is mid-confirm/cleaning/restoring is harmless. The safety gate in
  `cleanCmd`/`restoreCmd` remains authoritative (re-checks `IsCodexRunning`).

`pollInterval = 30 * time.Second`.

## Files (all `internal/tui`)

- `model.go` — add `pollInterval` const.
- `commands.go` — add `tickMsg` type + `tickCmd`.
- `update.go` — `Init` batches load + tick; handle `tickMsg` (re-load + reschedule).
- `view.go` — `bannerState` pure function + rendering + the watching indicator.

## Testing

- `bannerState`: idle+deadweight → actionable; active+deadweight → informational;
  none → calm; (deadweight uses the existing 100 MiB threshold).
- `Update(tickMsg{})` returns a non-nil command and leaves `m.state` unchanged
  (keeps watching without disturbing the user).
- `loadedMsg` arriving in a non-dashboard state updates data but does not change
  `m.state` (the refresh-is-harmless invariant).

## Out of scope (later slices)

Auto-popup / interrupt, terminal bell, OS-level notifications, the process-write-rate
risk engine (`internal/monitor`), `install-agent`, `self`.
