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
2. **Only touch Codex's OWN known log files** (`~/.codex/logs_2.sqlite*`) on the
   tool's own initiative. NEVER the user's project files. The allowed file set
   lives in `internal/codex` (`LogFileNames`); do not widen it casually.
3. **When uncertain, report — never resolve.** If it's not provably Codex's own
   recoverable junk, flag it for the user; don't act.
4. **Stay low-write and lightweight.** Do NOT use SQLite or any database for
   CodexSSD's own storage — use a simple JSONL file. A tool that guards against
   aggressive SQLite writes must not do the same. Keep monitoring samples in
   memory; write only a tiny session receipt at the end.
5. **Check before touching.** Don't act on a file that may be in active use
   (e.g. Codex mid-write).

## Current state

Phase 1, three commands implemented:

- **`status`** — 100% read-only (`os.Stat` on Codex's known log files; supports
  `--json`).
- **`clean`** — dry-run by default; with `--yes` moves Codex's own logs aside to
  a recoverable recycling bin via `internal/cleaner`. Refuses if Codex is
  running. Supports `--json`.
- **`restore`** — lists recoverable backups; with a backup id moves files back to
  their original `~/.codex/` location. Refuses if Codex is running. Supports
  `--json`.

Everything else (`watch`, `install-agent`, `self`) is a documented stub with a
package comment and no logic yet.

When implementing a new command, keep read-only behavior read-only, and keep
file-mutating behavior confined to `internal/cleaner` acting only on the
`internal/codex` allow-list.

## Layout

```
cmd/codexssd/        CLI entry point + command dispatch (main.go)
internal/codex/      Codex paths, known log files, size reporting  ← implemented
internal/monitor/    watcher + risk engine (stub)
internal/cleaner/    move-aside recycling-bin tidier (stub)
internal/agent/      AGENTS.md "please behave" installer (stub)
internal/recorder/   JSONL session history, NO database (stub)
internal/self/       CodexSSD's own-footprint self-report (stub)
docs/                full design spec (mirrored from Notion)
```

The display name is **CodexSSD**; the module, binary, and command are the
lowercase `codexssd` (Go module paths are conventionally lowercase). Module path:
`github.com/0xdefence/codexssd`.

## Conventions

- Plain `flag` package for CLI; no third-party deps unless there's a strong
  reason (single small binary is a product promise — see `docs/stack.md`).
- Pure, testable functions take inputs explicitly (e.g. `ScanLogs(dir)` rather
  than reading `$HOME` internally) so tests can use `t.TempDir()`.
- Human-readable sizes use binary units (KiB/MiB/GiB) via `codex.HumanBytes`.
- Friendly, plain-language user output — this tool is for non-technical users too.
- Comment the *why* (especially safety intent), match surrounding style.

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
