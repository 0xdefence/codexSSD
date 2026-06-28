# Phase 2 — Recycling-Bin Auto-Release + Visibility — Design

**Date:** 2026-06-28
**Status:** Approved (brainstorming) — ready for implementation plan
**Slice:** First Phase 2 slice. Auto-release expired backups to the OS Trash, plus
recycling-bin visibility (ages + release dates). Per-project "old logs" visibility
is deferred to Phase 3.

## Why

Phase 1 moves Codex's logs into a recoverable recycling bin
(`~/.codex/codexssd-backups/<timestamp>/`) and records `hold_until` (moved_at + 14
days) in each manifest — but nothing ever releases them, so the bin grows forever.
Phase 2's promise: *"held ~2 weeks, then sent to the trash on their own if not
missed."* This slice delivers that, and makes the bin's contents visible.

## The safety reconciliation (the crux)

The roadmap says "auto-trash after 2 weeks." CLAUDE.md rule #1 says: *"Move aside,
never hard-delete. Permanent deletion must require a separate, explicit user
action — never automatic."* These collide.

**Resolution: release = MOVE the expired backup into the OS Trash — never `rm`.**
This keeps rule #1 fully intact: CodexSSD only ever *moves* (logs → bin → Trash);
the user emptying their Trash is the explicit, user-initiated permanent delete.
It is also literally "sent to the trash." CodexSSD never calls `os.Remove`/
`os.RemoveAll` on a backup.

## Components

### New: `internal/trash` (stdlib-only)

One job: move a path into the OS Trash. No third-party deps.

- **macOS:** move into `~/.Trash/` with a collision-safe name.
- **Linux:** XDG trash — move into `$XDG_DATA_HOME/Trash/files/` (default
  `~/.local/share/Trash/files/`) and write a matching `…/info/<name>.trashinfo`
  (`[Trash Info]` with `Path=` original absolute path and `DeletionDate=`), so the
  desktop Trash can list/restore it.
- **Windows / unknown:** return `trash.ErrUnsupported`.
- API: `func Move(path string) error`; unexported `moveInto(dir, path string)
  (string, error)` does the collision-safe move and is unit-tested with a temp dir;
  unexported per-OS `trashDir() (string, error)`.

Consequence that closes the Windows gap: `clean --yes` already refuses on Windows
(unsupported busy-check), so Windows never creates backups — there is nothing to
auto-release there, so `ErrUnsupported` is moot in practice.

### `internal/cleaner` — release logic

- `func Expired(backups []Backup, now time.Time) []Backup` — **pure** filter:
  keeps backups where `now` is at or after `Manifest.HoldUntil`.
- `func Release(backupDir string) error` — moves the whole timestamped backup
  directory into the Trash via `trash.Move`. SAFETY: only ever a move; never a
  delete. Refuses a path that is not under a `codexssd-backups/` directory.
- `func ReleaseExpired(codexDir string, now time.Time) ([]string, error)` —
  `ListBackups` → `Expired` → `Release` each; returns the released backup ids. If
  the platform's trash is unsupported, it releases nothing and the backups stay
  safely held (never hard-deleted).

`trash` is imported only by `cleaner` (both stdlib-only engine packages).

### Triggers

- **Auto on app start:** the TUI `Init` fires a release-expired `tea.Cmd` (using
  `time.Now()`); a `releasedMsg` updates the dashboard with "Released N old
  backup(s) to Trash." Best-effort — a release error never blocks the app.
- **`codexssd prune`** — runs `ReleaseExpired`. `--dry-run` lists what *would* be
  released (read-only, via `Expired`), `--json` for scripts. Exit 0 ok / 1 error /
  2 bad flags.

### Visibility

- The in-app **restore list** and **`prune --dry-run`** show each backup's **age**
  and **"releases `<hold_until date>`"** (data already in the manifest).
- Dashboard: a line like "Recycling bin: N backup(s)" and, if any exist, the
  soonest upcoming release date (the minimum `hold_until`) — no fuzzy "soon"
  threshold; show the concrete date.
- **Scoping note:** the roadmap's "9 GB of old logs from a project untouched since
  March" is *per-project* awareness. Codex's logs are a single global SQLite DB, so
  true per-project staleness needs the connection map — that is **Phase 3**. Phase
  2's visibility is the recycling bin's own contents and ages.

## Safety summary

- Only ever moves (logs → bin → Trash); CodexSSD never `rm`s a backup.
- `Release`/`ReleaseExpired` act only on directories under `codexssd-backups/`.
- Unsupported platform → keep backups held; never hard-delete.
- Reuses the existing manifest `hold_until`; the clean/restore gates are unchanged.

## Testing

- `cleaner.Expired` — pure filter, boundary at exactly `hold_until` (released when
  `now == hold_until`).
- `trash.moveInto` — collision-safe move into a temp dir (second file with same
  name gets a distinct name).
- `cleaner.ReleaseExpired` — against `t.TempDir()` with a **stubbed trash seam**
  (assert which ids are released; assert an un-expired backup is NOT released; the
  real Trash is never touched).
- `prune` CLI — `--dry-run` releases nothing; exit codes; (stubbed trash seam so
  tests don't touch the real Trash).

## Files (anticipated)

- `internal/trash/trash.go` (+ test) — new package.
- `internal/cleaner/release.go` (+ test) — `Expired`, `Release`, `ReleaseExpired`,
  a `moveToTrash` seam defaulting to `trash.Move`.
- `cmd/codexssd/main.go` — `prune` command; `main_test.go`.
- `internal/tui/` — release-on-start `tea.Cmd` + `releasedMsg` + restore-list/
  dashboard age + release-date display.

## Out of scope (later)

Per-project / "old projects" staleness (Phase 3, needs the map); emptying the OS
Trash for the user (that is the user's explicit action, by design); configurable
retention (the 14-day `RetentionDays` constant stays for now).
