# Claude Code parity: watch, MCP, install-agent, and a tab-bar dashboard

**Date:** 2026-07-21
**Status:** Approved design, pre-implementation
**Builds on:** PRs #15–#18 (generic `tool` profiles, `--tool` on status/report/clean/restore/prune, TUI Claude screen)

## Goal

Close the remaining Claude Code gaps so both supported tools get the same
treatment end to end, and make the codex/claude mode switch obvious in both
the README and the TUI.

Four workstreams, in one round:

1. `watch --tool claude`
2. Tool-aware MCP server (five tools, one optional argument)
3. `install-agent --tool claude` → writes `CLAUDE.md`
4. TUI tab-bar switcher + README visibility restructure

Everything reuses the existing, safety-tested `internal/tool` profiles and
`internal/cleaner` engine. No new engine logic that mutates files. No new
third-party deps anywhere; charmbracelet stays confined to `internal/tui`.

## 1. `watch --tool claude`

`cmd/codexssd/watch.go` gains `--tool codex|claude` (default `codex`),
resolved through the same `resolveTool` helper the other commands use.

**Codex path is byte-for-byte unchanged.** The default flag value must produce
exactly today's output, receipts, and notifications — pinned by regression
tests.

The `watchDeps` seams get tool-aware constructors:

- `scan` — for codex, the existing `codex.ScanLogs` three-file report,
  untouched. For claude, a new pure function `tool.ScanDirSize` (in
  `internal/tool`) that walks the profile's dir and returns total bytes,
  **excluding `codexssd-backups/`** so our own tidies never count as agent
  writes. It takes the dir explicitly for `t.TempDir()` testability.
- `running` / `memory` — `tool.IsRunning(profile)` and the existing
  process-RSS helper, matched on the claude profile's `ProcessNames`.
- Risk engine (`internal/monitor`) is unchanged: it already scores rate
  (MB/min), WAL size, and memory from plain numbers. Claude has no WAL, so
  the WAL fields are passed as 0 and those checks simply never fire — no
  special-casing inside the engine.

Thresholds come from the same config keys (`*_mb_per_min`, `*_mem_mb`).
Per-tool thresholds are deliberately out of scope this round (YAGNI).

Output changes (both tools):

- `--json` lines gain a `"tool"` field.
- The session receipt records the tool name.
- Human output and notifications use the profile's `DisplayName`
  ("Claude Code is writing 130 MB/min…").
- The `watch` usage line and top-level help drop the "Codex-only" caveat.

Behavioral tracking (`observeBehavior` / `internal/behavior`) stays wired for
codex only this round; for claude it is passed as nil (disabled). Extending
provenance tracking to Claude is a separate, later decision.

## 2. Tool-aware MCP server

`internal/mcpserver` keeps **exactly five tools with their existing names** —
renaming would break existing client configs, and the "five read-only tools,
never a mutating one" invariant stays literal.

Each tool gains one optional argument:

```json
{ "tool": "codex" | "claude" }   // default "codex"
```

Plumbing: `tools/call` currently ignores arguments (`callTool(params.Name)`).
The dispatcher will parse `params.Arguments`, validate the `tool` value with
`tool.ByName` (unknown values return a JSON-RPC invalid-params error, not a
silent default), and pass the resolved profile to each tool implementation.

Per-tool semantics with `"tool": "claude"`:

- `codex_status` — the claude profile's own-file report: cleanable-stale
  summary (count + bytes) for `~/.claude`. The historical name is explained in
  the descriptor text ("status of the selected tool; named for the founding
  profile").
- `clean_plan` — `cleaner.PlanTool` for the claude profile (still a plan;
  this server can never execute it).
- `list_backups` — claude's recycling bin.
- `disk_report` — `internal/visibility` scan of `~/.claude`.
- `self_report` — tool-agnostic; the argument is accepted and ignored
  (documented as such) so clients can pass it uniformly.

Tool descriptors advertise the argument via their JSON schema so agents
discover it without reading docs.

## 3. `install-agent --tool claude`

`--tool claude` writes the disk/token-safe rules as **`CLAUDE.md`** instead of
`AGENTS.md`, with wording addressed to Claude Code. Same `--profile`
(default/strict), same `--print` preview, same refuse-to-overwrite unless
`--force`. The rule content is shared; only the filename and the tool-specific
phrasing differ — one template, small per-tool substitutions, no forked copies
to drift apart.

`install-agent` targets a *repo* file, not `~/.claude`, so none of the
profile allow-list machinery applies; the only safety rule in play is
"never overwrite without `--force`", which is unchanged.

## 4. TUI tab-bar switcher

The dashboard becomes tool-scoped with a persistent tab header under the logo:

```
  [ Codex ]   Claude Code        ← active tab highlighted
```

**Keys:** `tab` cycles tools; `1`/`2` jump directly; `l` remains as a
compatibility alias for the Claude tab (existing muscle memory + help text).
All keys listed in footer and help.

**The active tab drives everything:**

- Folder panel: codex shows the three log files (unchanged); claude shows the
  itemized cleanable-stale listing that currently lives on the dedicated `l`
  screen, which is folded into this tab and removed as a separate state.
- Risk panel: the active tool's risk level, rate, process state, memory.
- Recycling bin panel, banner, and the `c`/`r` actions all operate on the
  active tool, each behind its own independent `tool.IsRunning(profile)`
  gate exactly as PR #18 established. Confirm/working/result/blocked screens
  reuse the existing `returnState`/`workingLabel` generalization and return
  to the tab they came from.

**Sampling:** the poll loop scans **both** tools every tick and keeps a
per-tool sample window, so switching tabs never shows an empty rate history.
Two read-only dir scans per poll interval is acceptable; the claude scan
reuses the same exclusion of `codexssd-backups/` as watch.

**Session receipt:** the dashboard receipt reports the tool whose session
peak rate was higher, and gains a `tool` field saying which one that was
(empty/absent means codex, so old receipt lines stay readable). One receipt
per session, exactly as today.

## 5. README visibility

- New callout near the top of Usage — "**Works with Codex and Claude Code**" —
  with the one-line rule (`--tool codex|claude`, default codex) and a
  command × tool support matrix (all rows ✓/✓ once watch ships).
- The TUI section documents the tab bar and keys (`tab`, `1`/`2`, `l`).
- "Beyond Codex: Claude Code" slims to the Claude-specific safety rationale
  (why only *stale* transcripts/snapshots are cleanable, why memory/settings
  are NeverTouch) and is linked from the callout.
- `CLAUDE.md` (repo instructions) updated: watch/MCP/install-agent lose their
  Codex-only descriptions.

## Testing

- **Regression:** pinned-output tests that `watch` (no flag) and every MCP
  tool (no argument) behave byte-for-byte as before.
- **Watch:** hermetic `t.TempDir()` tests for the claude dir scan (exclusion
  of the backups bin; growth math), flag parsing, JSON `tool` field.
- **MCP:** contract tests for `tool` argument parsing, unknown-tool error,
  per-tool dispatch, descriptor schemas.
- **install-agent:** filename/wording per tool, `--force`/`--print` parity.
- **TUI:** update-loop tests for tab/1/2/l switching, per-tab action routing,
  per-tab running-check gates, both-tool sampling; view tests for the tab bar
  at wide and narrow widths.
- All engine packages remain stdlib-only (`go.mod` unchanged outside tui).

## Out of scope (this round)

- Per-tool risk thresholds in config.
- Behavioral/provenance tracking for Claude Code.
- Any third supported tool (the profile system is ready; adding one is a
  separate product decision).
- Any change to what is cleanable for Claude Code (the profile's allow-list
  is untouched).
