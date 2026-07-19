# CLAUDE.md

Guidance for working in this repository. Read this before changing code.

## What this is

**CodexSSD** is a small, low-write local watchdog for AI coding agents (starting
with OpenAI's Codex). It watches what an agent does to disk/memory, warns in
plain language when things get alarming, safely tidies Codex's **own** log files
into a recoverable recycling bin, and flags other clutter for the user to decide
on. It never acts on the user's real project files on its own.

The wedge: Codex's local SQLite logs (`~/.codex/logs_2.sqlite` plus `-wal` and
`-shm`) can write extremely aggressively and bloat over time, eating SSD lifespan
and disk space.

Full product spec lives in [`docs/`](docs/) (mirrored from Notion). Start with
`docs/overview.md`, `docs/roadmap.md`, `docs/architecture.md`, `docs/scope.md`.

## CRITICAL SAFETY RULES — the product's integrity, never violate

These are not preferences. They are the reason the tool can be trusted near a
machine. Any change that weakens them is wrong by definition.

1. **Move aside, never hard-delete.** The tool may only ever MOVE files to a
   recycling bin. Permanent deletion must require a separate, explicit user
   action — never automatic.
2. **Only touch a tool's OWN known files** on the tool's own initiative. NEVER
   the user's project files. The allow-list lives in each tool's `Profile` in
   `internal/tool`: `OwnFixedFiles` (cleanable at any age, e.g. Codex's
   `~/.codex/logs_2.sqlite*`) and `OwnStaleGlobs` (cleanable only once stale,
   e.g. Claude Code's `projects/*/*.jsonl` session transcripts) — with
   `NeverTouch` prefixes always winning over both. Do not widen a profile
   casually. `codex.LogFileNames` now aliases `tool.Codex().OwnFixedFiles`, so
   the Codex profile stays the single source of truth.
3. **When uncertain, report — never resolve.** If it's not provably Codex's own
   recoverable junk, flag it for the user; don't act.
4. **Stay low-write and lightweight.** Do NOT use SQLite or any database for
   CodexSSD's own storage — use a simple JSONL file. A tool that guards against
   aggressive SQLite writes must not do the same. Keep monitoring samples in
   memory; write only a tiny session receipt at the end.
5. **Check before touching.** Don't act on a file that may be in active use
   (e.g. Codex mid-write).
6. **Notifications are fire-and-forget.** Desktop notifications from `watch`
   (`internal/notify`) are best-effort only — a failure or delay there must
   never block, slow, or fail the watch loop; terminal output is always the
   source of truth.
7. **`internal/mcpserver` is read-only by definition.** It exposes exactly
   five read-only tools (`codex_status`, `clean_plan`, `list_backups`,
   `self_report`, `disk_report`) and may never gain a mutating tool — an agent
   using it can see everything and touch nothing.

## Current state

`codexssd` with no arguments launches the interactive dashboard (`internal/tui`).

Phase 1 and Phase 2 are implemented in full. Phase 3 (the shallow connection
map) is shipped for Claude Code. Phase 4 is partially shipped: multi-tool
profiles (Codex + Claude Code, end to end) and behavioral detection are in;
deep mapping, Cursor/Gemini, cost/token awareness, summaries, and extra
interfaces are not — see `docs/roadmap.md` for the authoritative phase-by-phase
status. All commands:

- **`status`** — 100% read-only. For Codex, `os.Stat` on the known log files;
  for other profiles, lists what is currently cleanable (stale own files only —
  fresh ones are left off deliberately). Supports `--json`, `--tool
  codex|claude` (default `codex`).
- **`watch`** — foreground, read-only monitor over Codex's logs and memory
  (Codex only — no `--tool` flag); prints on risk-level change, fires
  best-effort desktop notifications (`internal/notify`) on escalation, records
  one session receipt on exit. Also does best-effort behavioral tracking via
  `internal/behavior`: entries appearing in `~/.codex` while Codex is running
  are appended, one JSONL line per appearance, to
  `~/.codexssd/provenance.jsonl` — never blocks the loop on failure. Supports
  `--interval`, `--no-notify`, `--json`.
- **`clean`** — dry-run by default; with `--yes` moves a tool's own files aside
  to a recoverable recycling bin via `internal/cleaner`. Refuses if the tool is
  running. Supports `--json`, `--tool codex|claude`.
- **`restore`** — lists recoverable backups; with a backup id moves files back
  to their original location. Refuses if the tool is running. Supports
  `--json`, `--tool codex|claude`.
- **`prune`** — releases recycling-bin backups past their hold to the OS Trash
  (`internal/trash`). Supports `--dry-run`, `--json`, `--tool codex|claude`.
- **`report`** — read-only disk-usage breakdown of a tool's directory via
  `internal/visibility`, with stale-entry flags. `--connections` adds
  `internal/shallowmap`'s shallow probe: for Claude Code, whether a project
  transcript folder's decoded source path still exists on disk. The golden
  rule (verbatim): "finding a connection is trustworthy; finding nothing is
  not." Only two verdicts exist — `connected` (evidence-backed, hands off) and
  `unknown` (still report-only) — there is deliberately no "safe" verdict.
  Codex entries have no probe yet and `report` says so outright rather than
  silently omitting the section. Supports `--json`, `--tool codex|claude`,
  `--connections`.
- **`install-agent`** — writes a disk/token-safe `AGENTS.md` via
  `internal/agent`. Supports `--profile`, `--force`, `--print`.
- **`self`** — reports CodexSSD's own footprint via `internal/self`. Supports
  `--json`.
- **`mcp`** — serves the five read-only MCP tools over stdio via
  `internal/mcpserver` (Codex only, unchanged by multi-tool work).
- bare `codexssd` (no subcommand) — the interactive TUI dashboard.

When implementing a new command, keep read-only behavior read-only, and keep
file-mutating behavior confined to `internal/cleaner` acting only on a
`tool.Profile`'s `Allows` gate (re-checked on every move and restore).

## Layout

```
cmd/codexssd/         CLI entry point + command dispatch (main.go, watch.go)
internal/codex/       Codex paths, known log files, size reporting
internal/tool/        per-tool Profiles (allow-lists, process detection): Codex + Claude Code
internal/monitor/     watcher + risk engine
internal/cleaner/     move-aside recycling-bin tidier
internal/agent/       AGENTS.md "please behave" installer
internal/recorder/    JSONL session history, NO database
internal/self/        CodexSSD's own-footprint self-report
internal/config/      ~/.codexssd/config.json loader (never bricks the tool)
internal/visibility/  `report`'s ~/.codex disk-usage scan
internal/shallowmap/  `report --connections`' shallow probe (Phase 3)
internal/behavior/    `watch`'s best-effort behavioral provenance recorder (Phase 4)
internal/notify/      best-effort desktop notifications for `watch`
internal/mcpserver/   read-only MCP server (stdio) for `mcp`
internal/tui/         interactive dashboard (bare `codexssd`)
internal/trash/       OS Trash integration for `prune`
docs/                 full design spec (mirrored from Notion)
```

The display name is **CodexSSD**; the module, binary, and command are the
lowercase `codexssd` (Go module paths are conventionally lowercase). Module path:
`github.com/0xdefence/codexssd`.

## Conventions

- No third-party deps in the engine packages (single small binary is a product
  promise — see `docs/stack.md`). The ONE exception: the interactive app in
  `internal/tui` uses the charmbracelet libraries (Bubble Tea, Lip Gloss).
- Pure, testable functions take inputs explicitly (e.g. `ScanLogs(dir)` rather
  than reading `$HOME` internally) so tests can use `t.TempDir()`.
- Human-readable sizes use binary units (KiB/MiB/GiB) via `codex.HumanBytes`.
- Friendly, plain-language user output — this tool is for non-technical users too.
- Comment the *why* (especially safety intent), match surrounding style.
- **Config can never brick the tool.** `internal/config` unmarshals onto
  `Default()`, so a missing file yields defaults and a malformed file yields
  defaults plus a warning — never a fatal error. Any code that reads config
  must preserve this: warn and carry on, never fail the command.

## Commands

```bash
go build ./...                      # build everything
go test ./...                       # run tests
go vet ./...                        # vet
gofmt -l .                          # list unformatted files (should be empty)
go run ./cmd/codexssd status        # run the status command
go run ./cmd/codexssd status --json # JSON output
```

Always run `go build ./... && go vet ./... && go test ./...` before claiming work
is done.
