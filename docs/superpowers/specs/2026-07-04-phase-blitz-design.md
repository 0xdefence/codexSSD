# Design: Phase 1–2 Completion Blitz + MCP Server

**Date:** 2026-07-04
**Status:** Approved by product owner (pre-implementation)
**Execution model:** Subagent-driven — one developer agent and one staff-engineer
reviewer agent per work item, in three dependency-ordered waves.

## Goal

Close every gap between the codebase and the Phase 1 + Phase 2 promises in
`docs/roadmap.md`, and add one deliberate off-roadmap capability: a read-only
MCP server so AI agents can *see* what CodexSSD sees without being able to
*touch* anything.

Out of scope (unchanged from `docs/scope.md`): daemons/login services, other AI
tools' directories, user project scanning, any mutating capability exposed to
machines, Windows support for mutating commands.

## Decisions made

| Decision | Choice |
|---|---|
| Sprint scope | All seven items below |
| `watch` shape | Foreground command + native desktop notifications; **no daemon** |
| MCP server | Read-only tools only; hand-rolled stdio JSON-RPC, **stdlib only** |
| Visibility scan | Deep read-only scan of `~/.codex` **only** |

## Non-negotiable constraints

The five CRITICAL SAFETY RULES in `CLAUDE.md` bind every item. Additionally:

- No new third-party dependencies anywhere (the TUI's charmbracelet exception
  stays the only exception).
- No `os.Remove`/`os.RemoveAll`/`os.Rename` outside `internal/cleaner` and
  `internal/trash`.
- CodexSSD's own storage stays JSONL — never a database.
- Every item lands with tests; `go build ./... && go vet ./... && go test ./...
  && gofmt -l .` must be clean before an item is called done.

---

## Wave 1 — foundations (four independent items, parallel)

### Item 1: Config file (`internal/config`)

Plain JSON at `<state-dir>/config.json`, where `<state-dir>` is the directory
`recorder.Dir()` already defines — one home for everything CodexSSD owns.

Fields (all optional; zero value → documented default):

- `thresholds`: the five values in `monitor.DefaultThresholds`
- `poll_interval_seconds` (default: current TUI interval)
- `bin_hold_days` (default 14 — feeds `cleaner.Expired`)
- `notifications` (bool, default true — feeds `watch`)
- `stale_after_days` (default 30 — feeds the disk report)

Behavior: missing file → defaults silently. Malformed file → plain-language
error naming the file and the problem; commands then *proceed with defaults*
(config can never brick the tool). Unknown keys ignored. Pure
`config.Load(path) (Config, error)`; callers use `config.LoadDefault()` which
resolves the state dir. No config-writing command this sprint — users edit the
file by hand; `docs` item documents the format.

### Item 2: Recorder wiring

Every mutation appends one `recorder.Receipt` via the existing (currently
orphaned) `recorder.Append`:

- `clean --yes` and TUI tidy → `{action: "clean", bytes, backup_id, at}`
- `restore <id>` and TUI restore → `{action: "restore", backup_id, at}`
- `prune` and TUI auto-release → `{action: "prune", backup_ids, at}`
- `watch` (Item 5) → one receipt on exit: `{action: "watch", duration, peak_risk, at}`

Receipt write failures are reported as a one-line warning but NEVER fail or
roll back the user's action (the receipt is bookkeeping, not a ledger).
`self` gains: record count, last action, history size (already measured).
The existing record cap in `recorder` is kept as-is.

### Item 3: Disk-visibility report (`codexssd report`)

New read-only command + `internal/visibility` package.

- Walks `~/.codex` recursively (and ONLY `~/.codex`), read-only.
- Aggregates by top-level entry (`sessions/`, `logs_2.sqlite`, `history.jsonl`,
  `codexssd-backups/`, …): total size, file count, newest mtime.
- Flags entries older than `stale_after_days` in plain language:
  `"sessions/  2.1 GiB, 312 files — untouched since March"`.
- `codexssd-backups/` is reported in its own labelled section (it's ours).
- Permission/IO errors on subtrees degrade to a `"couldn't read X"` line;
  the report never fails outright.
- `--json` emits the full structure. Human output ends with a plain-language
  one-line summary and, when stale items exist, a *pointer* — never an action:
  `"CodexSSD only tidies its known log files; the rest is yours to decide on."`
- Pure core: `visibility.Scan(dir, now, staleAfter) (Report, error)`.

### Item 4: Memory monitoring

- `internal/codex`: new `ProcessMemory() (bytes int64, err error)` sampling
  total RSS of detected Codex processes via `ps -o rss=` on darwin/linux.
  Returns `ErrUnsupportedPlatform` on Windows; callers omit memory rather
  than erroring.
- `monitor.Sample` gains `MemBytes`; `monitor.Thresholds` gains
  `HighMemMB`/`CriticalMemMB` (defaults 2048/6144, config-overridable).
- `monitor.Evaluate` stays pure; memory escalates `Level` with a reason string
  like `"Codex is using 3.2 GiB of memory"`.
- TUI header shows current Codex memory when available.

---

## Wave 2 — headline features (two items, parallel, after Wave 1)

### Item 5: `codexssd watch`

Foreground, read-only, Ctrl-C to stop.

- Flags: `--interval <dur>` (default from config), `--no-notify`, `--json`
  (one JSON line per assessment change, for scripting).
- Loop: sample logs (existing `ScanLogs`) + memory (Item 4) → ring buffer of
  samples (existing `monitor.AppendSample`) → `monitor.Evaluate` with
  config thresholds.
- Prints one plain-language line only when the risk **level changes** (plus one
  baseline line at start). No per-tick spam.
- On escalation to HIGH or CRITICAL: native desktop notification —
  `osascript -e 'display notification …'` (darwin), `notify-send` (linux).
  Notification failure is silently ignored (the terminal line is the source of
  truth). `--no-notify` and the config flag both disable.
- On exit (SIGINT), writes one `watch` receipt (Item 2) and prints a short
  session summary (duration, peak risk, total growth observed).
- New `internal/notify` package: `Notify(title, body string) error`, platform
  switch inside, injectable for tests.

### Item 6: MCP server (`codexssd mcp`)

Stdio MCP server in new `internal/mcpserver`, stdlib only.

- Protocol: JSON-RPC 2.0, newline-delimited on stdin/stdout. Implements
  `initialize` (advertising `tools` capability only), `notifications/initialized`,
  `tools/list`, `tools/call`, `ping`. Unknown methods → standard JSON-RPC
  method-not-found. Pinned protocol version `2025-06-18`; if the client
  requests another version, respond with ours per spec.
- Five tools, ALL read-only, each returning the same JSON as the corresponding
  `--json` CLI path (single source of truth — the tool handlers call the same
  internal functions, never shell out):
  1. `codex_status` — `codex.ScanLogs` report
  2. `clean_plan` — `cleaner.PlanCodexLogs` dry run + codex-running flag
  3. `list_backups` — `cleaner.ListBackups` with ages/hold status
  4. `self_report` — `self.Measure` + receipt summary
  5. `disk_report` — `visibility.Scan` (Item 3)
- NO mutating tools. This is a hard product line, restated in the package
  comment: an agent may see everything and touch nothing.
- Logging to stderr only (stdout is the protocol channel).
- Tests: drive the server over in-memory pipes with recorded request/response
  fixtures for the full handshake + each tool, plus malformed-input cases
  (bad JSON, unknown tool, wrong params → JSON-RPC errors, never a crash).

---

## Wave 3 — truth (one item, last)

### Item 7: Docs sync

- README: replace "stub" claims; document `report`, `watch`, `mcp`,
  `install-agent`, `prune`, the TUI, the config file format, and an MCP setup
  snippet (`claude mcp add codexssd -- codexssd mcp`).
- CLAUDE.md "Current state" and layout table updated to reality; safety rules
  restated to cover the new surface (notify is fire-and-forget; mcpserver is
  read-only by definition).
- `docs/scope.md`: move shipped items from "in scope" prose to shipped status;
  `docs/roadmap.md`: annotate Phase 1/2 bullets with done-markers.
- No behavior changes in this item; docs only.

---

## Execution protocol (applies to every item)

1. **Dev agent** implements the item TDD-style in an isolated worktree,
   following repo conventions (pure functions with injected paths, binary-unit
   sizes, friendly output, comment the *why*).
2. **Staff-engineer agent** then reviews adversarially in the same worktree:
   - Safety-rules checklist first (any weakening = reject).
   - Hunts bugs: race conditions with a running Codex, TOCTOU on file moves,
     platform edge cases, JSON encoding of empty slices (`[]` not `null` —
     see commit d1465f0 for precedent), error-path behavior.
   - Patches what it finds, or bounces back to the dev agent with a concrete
     defect list if the design itself is wrong.
3. Full gate (`build/vet/test/gofmt`) green → merge to main. Wave N+1 starts
   only after all of Wave N is merged.

## Testing strategy

Unit tests per package (existing convention, `t.TempDir()` + injected seams).
The integration test in `cmd/codexssd` grows cases for `report`, `watch`
(one-shot mode via injected clock/sampler), and `mcp` (pipe-driven handshake).
Manual smoke: `codexssd watch` against a real bloating WAL, `claude mcp add`
against the built binary.

## Risks

- **Notification portability** — `notify-send` absent on some Linuxes; mitigated
  by silent degradation and the terminal line being authoritative.
- **MCP protocol drift** — hand-rolled implementation is pinned to one protocol
  version; acceptable for a read-only server, revisit if clients break.
- **`ps` output variance** — RSS parsing differs subtly across darwin/linux;
  mitigated by table-driven parser tests with captured fixtures.
- **Wave-1 worktree merges** — Items 1 and 4 both touch `monitor.Thresholds`;
  merge Item 4 first, then Item 1 rebases its config mapping on top.
