# CodexSSD

**A small, quiet watchdog for AI coding agents — it protects your SSD and disk
space from runaway local AI tools, starting with OpenAI's Codex.**

CodexSSD watches what an AI coding agent does to your disk and memory, warns you
in plain language when things start to look alarming, safely tidies Codex's *own*
log files into a recoverable recycling bin, and flags other clutter for you to
decide on. It never acts on your real project files on its own.

> CodexSSD is **not** an AI coding agent. It's a guardrail layer *around* them.

## The problem it solves

AI coding tools behave a bit like very fast junior assistants with full access to
your computer. While they work, they leave things behind: huge log files, caches,
and stale files. It piles up slowly and invisibly across every project — until
one day your disk is mysteriously full and your laptop feels slow.

The immediate wedge is a real Codex issue: Codex's local SQLite logs at

```
~/.codex/logs_2.sqlite
~/.codex/logs_2.sqlite-wal
~/.codex/logs_2.sqlite-shm
```

can write extremely aggressively and bloat over time — quietly eating SSD
lifespan and disk space. CodexSSD's job is to *notice this on your behalf*.

## Safety first (the product's integrity)

CodexSSD is a tool you let near your machine precisely *because* you're worried
about another tool near your machine. So trust is everything:

- **It only ever moves files aside** (into a recycling bin) — it never
  hard-deletes. Permanent deletion always requires a separate, explicit action
  from you.
- **The only files it may touch on its own are Codex's own known log files**
  (`~/.codex/logs_2.sqlite*`). Never your project files.
- **When it's uncertain whether something is junk, it reports — it never
  resolves.**
- **It stays low-write and lightweight.** It deliberately uses a plain JSONL file
  for its own storage, never a database — it would be hypocritical for a tool
  that guards against aggressive SQLite writes to do the same.

## Install / build

Requires Go (the repo is developed against the current Go toolchain; Go 1.22+).

Build a local binary:

```bash
git clone https://github.com/0xdefence/codexSSD.git
cd codexSSD
go build -o codexssd ./cmd/codexssd
./codexssd status
```

Or run without building:

```bash
go run ./cmd/codexssd status
```

Or install onto your `PATH`:

```bash
go install github.com/0xdefence/codexssd/cmd/codexssd@latest
```

> The display name is **CodexSSD**; the module, binary, and command are the
> lowercase `codexssd` (Go module paths are conventionally lowercase).

## Usage (today)

Only the read-only `status` command is implemented so far. It touches nothing,
moves nothing, and deletes nothing.

```bash
codexssd status          # human-readable report of Codex's log files + total
codexssd status --json   # the same report as JSON, for scripts/tooling
```

Example:

```
Codex directory: /Users/you/.codex

Codex log files:
  logs_2.sqlite             142.3 MiB
  logs_2.sqlite-wal           9.4 GiB
  logs_2.sqlite-shm           4.0 MiB

Total:                        9.5 GiB
```

If you don't have a `~/.codex` directory, `status` says so politely and exits
cleanly — there's nothing to report.

## Phase 1 scope

This repo is at the start of **Phase 1 — The Watchdog (the safe core)**. The
plan is to build in order of *safety earned*: the earliest versions can only
watch, warn, and tidy the AI tool's own recoverable mess.

Phase 1 covers (read-only first, safe actions later):

- Watching Codex and reporting its log-file sizes — **`status` is done**
- Plain-language warnings when disk/memory use gets alarming
- Safely clearing Codex's *own* logs into a recoverable recycling bin
- A "please behave" `AGENTS.md` rules installer
- Honest self-reporting of CodexSSD's own footprint

Deliberately **out of scope** for now: deleting anything automatically, acting on
your real project files, deep code relationship-mapping, and support for other AI
tools. See [`docs/scope.md`](docs/scope.md) for the full line in the sand.

## Documentation

The full design spec lives in [`docs/`](docs/):

- [overview.md](docs/overview.md) — what it is, the problem, who it's for
- [roadmap.md](docs/roadmap.md) — the four phases, in plain language
- [architecture.md](docs/architecture.md) — the parts, diagrams, and safety rules
- [stack.md](docs/stack.md) — why it's all Go, and the no-database choice
- [scope.md](docs/scope.md) — what this build does and defers
- [future-builds.md](docs/future-builds.md) — the ambitious later chapters
- [ideas-parking-lot.md](docs/ideas-parking-lot.md) — raw ideas, open questions, risks

> Naming note: earlier spec drafts named this "CodexGuard"; it now ships as
> **CodexSSD**.

## Repository layout

```
cmd/codexssd/        CLI entry point
internal/codex/      Codex paths, known log files, size reporting (used now)
internal/monitor/    watcher + risk engine (stub)
internal/cleaner/    move-aside recycling-bin tidier (stub)
internal/agent/      AGENTS.md "please behave" installer (stub)
internal/recorder/   JSONL session history, no database (stub)
internal/self/       CodexSSD's own-footprint self-report (stub)
docs/                the full design spec
```
