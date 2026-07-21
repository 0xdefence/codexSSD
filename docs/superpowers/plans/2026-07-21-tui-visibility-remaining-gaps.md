# TUI Visibility: Remaining Gaps (for the next agent run)

Backlog notes, not an approved design. This documents the full original gap
analysis (what CodexSSD tracks vs. what `internal/tui`'s dashboard actually
shows), what's been closed so far, and what's still open — so a future session
can pick this up without re-deriving it from scratch.

## Background

The interactive dashboard (bare `codexssd`, `internal/tui`) only ever surfaced
a subset of what CodexSSD computes. An initial audit (2026-07-21) diffed every
tracked/computed value across `internal/monitor`, `internal/recorder`,
`internal/visibility`, `internal/self`, `internal/codex`, `internal/notify`,
`internal/config`, `internal/cleaner`, and `internal/trash` against what the
TUI actually renders. Two rounds of work have addressed most of it.

## Round 1 — shipped (`feat/tui-visibility`, merged via PR #15)

Design: `docs/superpowers/specs/2026-07-21-tui-visibility-design.md`.

- Full `Assessment.Reasons` list rendered (was `Reasons[0]` only)
- Footer's poll-interval text now derives from real config (was hardcoded "30s")
- Live session-peak line (`peakRisk` / `peakRate`) on the Risk panel
- New Info screen (`i` key): Settings panel (all config thresholds, bin-hold
  days, stale-after days, notifications toggle), CodexSSD's own footprint
  (`internal/self`), full `~/.codex` disk-visibility report with STALE/⚠ flags
- Desktop notifications now fire from the dashboard itself on the same
  HIGH/CRITICAL escalation condition `watch.go` already used (previously only
  `watch` fired notifications)

## Round 2 — in progress as of this writing (not yet built)

Branch/scope discussed but not yet implemented at time of writing this doc:

- Codex process list (PID/name/command) — expand the dashboard's
  "Codex: running" line, since `codex.DetectProcesses()` already returns this,
  just never rendered
- Backup `ManifestItem.OriginalPath` per file — show on the Confirm Restore
  screen (the moment a user is about to decide whether to restore)
- Trash-destination detail — requires an actual API change:
  `internal/trash.Move` currently returns only `error`, no destination path.
  Needs `Move`, `cleaner.Release`, and `cleaner.ReleaseExpired` to propagate a
  destination so the TUI's release note can say more than a bare count
  (e.g. "released 2 backup(s) → ~/.Trash").

If this doc is being read before round 2 actually lands, check
`git log --oneline` on `feat/phase3-4-multi-tool` / `staging` for a branch
matching this description before starting fresh — it may already be underway
or merged.

## Still not covered by round 1 or round 2 (open backlog)

| Gap | What it is | Why it's still open |
|---|---|---|
| Raw sample history / trend | The in-memory ring buffer (`monitor.Sample`, ~20 samples of size/WAL/mem over time) behind `Assessment` | Never proposed as a concrete option — `Assessment` already summarizes it; would need a fresh idea (e.g. a sparkline) to be worth a screen. Not obviously high value. |
| Session start time / total session log growth (bytes) | Tracked (`startedAt`, `startBytes`) for the quit-time JSONL receipt, alongside peak risk/rate | Bundled implicitly with the "session peaks" round-1 option but never asked about specifically — round 1 only surfaced peak risk/rate, not the growth-in-bytes figure. |
| Dry-run `Plan` contents, itemized | `cleaner.Plan`/`PlanItem` — what a `clean` would actually move, file by file | Currently only used as a gate (`m.plan.Empty()`) and for the confirm-clean prompt's total. No screen lists plan items individually. Wasn't offered as a round-1 or round-2 option. |
| Full past-session history | Reading back `~/.codexssd/sessions.jsonl` via `recorder.SummarizeFile` (all past receipts, not just the live session) | Explicitly deferred by user choice during round-1 brainstorming — chose "current-session peaks only" over a full history screen. Revisit only if the user asks for session-over-session trends. |

## How to pick this up

Treat each remaining row as its own candidate for a future round — don't just
implement all four. Run the normal brainstorming flow (`superpowers:brainstorming`)
starting from "which of these open gaps, if any, are worth doing now," the same
way round 2's scope was chosen from round 1's leftovers. Confirm branch base
against whatever `staging` looks like at the time — don't assume `feat/tui-visibility`
or `feat/phase3-4-multi-tool` are still the tips.
