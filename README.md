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
- **The only files it may touch on its own are a tool's own known files** —
  Codex's log files (`~/.codex/logs_2.sqlite*`), or, when you opt into it with
  `--tool claude`, Claude Code's own stale session files. Never your project
  files.
- **When it's uncertain whether something is junk, it reports — it never
  resolves.**
- **It stays low-write and lightweight.** It deliberately uses a plain JSONL file
  for its own storage, never a database — it would be hypocritical for a tool
  that guards against aggressive SQLite writes to do the same.

## Install / build

**Prebuilt binaries:** download the archive for your OS/arch from the
[GitHub Releases](https://github.com/0xdefence/codexSSD/releases) page, unpack it,
and run `./codexssd`. Releases are cut from version tags via GoReleaser
(macOS and Linux, amd64 + arm64); each release includes `checksums.txt`.

### Build from source

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

### `status` — read-only report (touches nothing)

```bash
codexssd status                  # human-readable report of Codex's log files + total
codexssd status --json           # the same report as JSON, for scripts/tooling
codexssd status --tool claude    # the same idea, for Claude Code
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

Every command that acts on or reports a tool's own files — `status`, `report`,
`clean`, `restore`, `prune` — accepts `--tool codex|claude` (default `codex`).
`watch` stays Codex-only for now. See
[Beyond Codex: Claude Code](#beyond-codex-claude-code) below.

### `clean` — move logs aside (dry-run by default, never deletes)

```bash
codexssd clean                  # dry run — shows what would be moved, touches nothing
codexssd clean --yes            # actually moves Codex's own logs to a recoverable bin
codexssd clean --json           # dry-run output as JSON
codexssd clean --tool claude    # dry run for Claude Code's stale session files instead
```

`clean` is **dry-run by default** — no files are touched until you pass `--yes`.
With `--yes`, Codex's logs are moved to a timestamped recycling bin inside
`~/.codex/codexssd-backups/`. Nothing is ever hard-deleted; you can restore any
backup at any time.

`clean --yes` refuses to act if Codex appears to be running (it checks your
running processes first), so it will never race with an active agent session.

### `restore` — move cleaned logs back

```bash
codexssd restore                    # list recoverable backups
codexssd restore <backup-id>        # restore a specific backup to its original location
codexssd restore --json             # list backups as JSON
codexssd restore --tool claude      # list Claude Code's recoverable backups instead
```

`restore` is the undo for `clean`. It moves the files back from the recycling
bin to their original `~/.codex/` locations. Like `clean --yes`, it refuses to
act if Codex is running.

### `report` — what's using disk inside `~/.codex` (read-only)

```bash
codexssd report                           # plain-language breakdown of everything in ~/.codex
codexssd report --json                    # the same report as JSON
codexssd report --tool claude             # the same idea, for Claude Code's directory
codexssd report --tool claude --connections  # also probe whether each project folder is still connected
```

Unlike `status` (which only looks at a tool's own known files), `report` walks
the whole tool directory (`~/.codex` by default) and shows every entry's size,
file count, and whether it looks stale (untouched for a while — configurable
via `stale_after_days`). It flags CodexSSD's own recycling bin so you can tell
it apart from the tool's data. It only ever reports — it never acts on
anything outside a tool's own known files.

`--connections` adds a shallow connection probe (currently real for Claude
Code only): for each project's session folder, it checks whether the source
project it belongs to still exists on disk. Example:

```
Connections (shallow map — read-only):
  -Users-you-code-my-app       74.6 MiB   connected — its project folder still exists on disk (/Users/you/code/my-app)
  -Users-you-old-experiment    20.6 MiB   unknown — nothing obvious points here, but that is NOT proof it's safe; your call
```

The rule behind this section: **finding a connection is trustworthy; finding
nothing is not.** A `connected` entry has real evidence behind it — leave it
alone. An `unknown` entry just means CodexSSD didn't find anything obvious; it
is not proof the entry is safe to remove, so it stays report-only, same as
everything else. Running `report --connections` for Codex says outright that
no probe exists for Codex yet, rather than silently showing nothing.

### `watch` — foreground monitor with warnings (read-only)

```bash
codexssd watch                 # watch in the foreground; Ctrl-C to stop
codexssd watch --interval 10s  # check more often than the default (config, 30s)
codexssd watch --no-notify     # suppress desktop notifications
codexssd watch --json          # emit one JSON line per risk-level change
```

`watch` samples Codex's log sizes and memory use on a timer and prints a line
whenever the risk level changes (calm sessions stay quiet). If things escalate
to a high or critical level, it fires a best-effort desktop notification —
notifications are fire-and-forget and never block or fail the watch loop.
On exit it writes one small session receipt to CodexSSD's own history; it
never touches Codex's files.

While watching, it also quietly notices any new entries that appear in
`~/.codex` during the session ("I watched this get created while Codex was
running" is a much stronger signal than guessing from a name) and remembers
that — this is purely an observation, best-effort, and never touches those
entries. `report` later mentions if something you're looking at showed up
during a watched session. `watch` is Codex-only for now; it doesn't take
`--tool`.

### `prune` — release expired recycling-bin backups to the Trash

```bash
codexssd prune                  # move backups past their ~2-week hold to the OS Trash
codexssd prune --dry-run        # just list what would be released, touches nothing
codexssd prune --json           # output as JSON
codexssd prune --tool claude    # prune Claude Code's expired backups instead
```

Backups moved aside by `clean --yes` are held for `bin_hold_days` (14 by
default) in case you want them back. `prune` releases only the ones that have
passed their hold — into the OS Trash, not permanent deletion, so the normal
Trash-recovery flow still applies.

### `install-agent` — write a disk/token-safe `AGENTS.md`

```bash
codexssd install-agent                        # write AGENTS.md into the current directory
codexssd install-agent --profile strict path/  # choose a rule profile, target a specific repo
codexssd install-agent --print                 # preview the rules without writing anything
codexssd install-agent --force                 # overwrite an existing AGENTS.md
```

Installs a "please behave" `AGENTS.md` with disk/token-safe rules for the AI
agent working in a repo, to reduce mess at the source. It refuses to overwrite
an existing `AGENTS.md` unless you pass `--force`; `--print` lets you preview
the rules first.

### `self` — CodexSSD's own footprint

```bash
codexssd self          # how much disk CodexSSD itself is using, plus its history
codexssd self --json   # the same report as JSON
```

CodexSSD keeps itself honest: `self` reports its own storage footprint (a
plain JSONL history file, never a database) and a summary of its recorded
actions, so you can see it isn't the problem.

### `mcp` — read-only tools for AI agents (MCP over stdio)

```bash
codexssd mcp   # serve five read-only tools over stdio (MCP)
```

See the [MCP](#mcp) section below for what this exposes and how to wire it up.

### Bare `codexssd` — the interactive dashboard

```bash
codexssd   # opens an interactive terminal dashboard
```

Running `codexssd` with no command at all launches a small interactive
terminal app: a live view of Codex's log sizes with guided (confirm-first)
`clean` and `restore` actions. It adds no file-mutating logic of its own — it's
a thin layer over the same safety-tested engine used by the CLI commands
above.

## Beyond Codex: Claude Code

CodexSSD started as a Codex-only tool, but the same safety-first approach now
also covers Claude Code — just add `--tool claude` to `status`, `report`,
`clean`, `restore`, or `prune`.

Claude Code is different from Codex in one important way: Codex's log files
are safe to move aside at any moment, but Claude Code's own recoverable data —
session transcripts (`~/.claude/projects/<project>/<session>.jsonl`) and shell
snapshots — is still doing a job while it's fresh: transcripts power `claude
--resume`. To respect that, CodexSSD will only ever offer to move aside these
files once they've gone stale (untouched for a while, same `stale_after_days`
setting `report` uses). Fresh files are never even shown as candidates for
cleaning, on purpose.

A short list of Claude Code files are never touched, full stop, no matter how
old they get — because they aren't clutter, they're your setup: your saved
memory, `settings.json` / `settings.local.json`, `CLAUDE.md`, and anything
under `plugins`, `agents`, `commands`, `skills`, `hooks`, or `todos`, plus
`keybindings.json`. CodexSSD's per-tool rules always check this "never touch"
list first, and it always wins.

## Configuration

CodexSSD reads an optional config file at `~/.codexssd/config.json`. Every key
is optional — anything you omit keeps its default. **A missing or broken
config never stops the tool — it warns and uses defaults.**

Full example showing every key and its default:

```json
{
  "medium_mb_per_min": 25,
  "high_mb_per_min": 100,
  "critical_mb_per_min": 500,
  "high_wal_size_mb": 1024,
  "critical_wal_size_mb": 8192,
  "high_mem_mb": 2048,
  "critical_mem_mb": 6144,
  "poll_interval_seconds": 30,
  "bin_hold_days": 14,
  "notifications": true,
  "stale_after_days": 30
}
```

- `medium_mb_per_min` / `high_mb_per_min` / `critical_mb_per_min` — log write
  rate thresholds (MB/min) that raise `watch`'s risk level.
- `high_wal_size_mb` / `critical_wal_size_mb` — WAL file size thresholds.
- `high_mem_mb` / `critical_mem_mb` — Codex process memory thresholds.
- Setting any of the above thresholds to `0` (or a negative number) disables
  that specific check rather than making it fire on everything.
- `poll_interval_seconds` — how often `watch` re-checks `~/.codex` (clamped to
  a minimum of 5 seconds).
- `bin_hold_days` — how long `clean --yes` backups are held before `prune` can
  release them (clamped to a minimum of 1 day).
- `notifications` — whether `watch` may fire desktop notifications.
- `stale_after_days` — how old an entry must be before `report` flags it as
  stale.

## MCP

CodexSSD can serve its read-only tools to AI agents over stdio using the
[Model Context Protocol](https://modelcontextprotocol.io). The design goal is
simple: **an agent can see everything and touch nothing.** There is no
mutating tool, and there never will be — `mcp` exposes exactly five read-only
tools:

- `codex_status` — sizes of Codex's own log files
- `clean_plan` — the dry-run plan of what `clean` *would* move aside (this
  server can never execute it)
- `list_backups` — recoverable recycling-bin backups
- `self_report` — CodexSSD's own footprint and action history
- `disk_report` — what's using disk inside `~/.codex`, with stale flags

Setup with Claude Code:

```bash
claude mcp add codexssd -- codexssd mcp
```

## Roadmap status

Phase 1 (the safe core: watch, warn, tidy Codex's own logs) is **complete**.
Phase 2 (recycling-bin lifecycle + disk visibility) is **complete**, with the
deliberate Phase-2-era narrowing that `report` covers `~/.codex` only, per the
spec. Phase 3 (the shallow connection map) has **shipped** for Claude Code
project folders — Codex entries have no probe yet and `report` says so
outright. Phase 4 (multi-tool support + behavioural detection) has **partially
shipped**: Claude Code now has full `status`/`report`/`clean`/`restore`/`prune`
support, and `watch` records best-effort behavioural provenance for Codex.
Deep relationship-mapping, Cursor/Gemini support, cost/token awareness, and
daily/weekly summaries remain future work. See
[`docs/roadmap.md`](docs/roadmap.md) for the full phase plan and
[`docs/scope.md`](docs/scope.md) for the in/out line in the sand.

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
cmd/codexssd/         CLI entry point + command dispatch
internal/codex/       Codex paths, known log files, size reporting
internal/tool/        per-tool Profiles (allow-lists, process detection): Codex + Claude Code
internal/monitor/     watcher + risk engine
internal/cleaner/     move-aside recycling-bin tidier
internal/agent/       AGENTS.md "please behave" installer
internal/recorder/    JSONL session history, no database
internal/self/        CodexSSD's own-footprint self-report
internal/config/      ~/.codexssd/config.json loader (never bricks the tool)
internal/visibility/  `report`'s ~/.codex disk-usage scan
internal/shallowmap/  `report --connections`' shallow probe (Phase 3)
internal/behavior/    `watch`'s best-effort behavioral provenance recorder (Phase 4)
internal/notify/      best-effort desktop notifications for `watch`
internal/mcpserver/   read-only MCP server (stdio) for `mcp`
internal/tui/         interactive dashboard (bare `codexssd`)
internal/trash/       OS Trash integration for `prune`
docs/                 the full design spec
```
