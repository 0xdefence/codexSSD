# Phase 1 Completion — Plan of Record

**Date:** 2026-06-26
**Purpose:** Sequence and scope the remaining work to turn CodexSSD into a complete,
testable Phase 1 "Watchdog" (watch → warn → safely tidy → behave-rules → self-honesty),
then cut a release testers can install.

This is the **plan of record** (the backlog + decisions + sequencing). Each part
still gets its own design spec + granular TDD plan at execution time, built and run
the same way as `clean`/`restore` and the interactive shell (brainstorm → spec →
writing-plans → subagent-driven-development → PR into `staging`).

## Where we are

Done & merged to `staging`:
- `internal/codex` (paths, log sizes, `IsCodexRunning`), `internal/cleaner`
  (move-aside/restore + manifest, mutation-tested).
- CLI `status`/`clean`/`restore` (gated, `--json`).
- Interactive dashboard (`codexssd` no-args) with in-app Tidy/Restore.

Planned & PARKED (ready to execute):
- **Watch loop** — live 30s polling + passive deadweight banner.
  Spec: `docs/superpowers/specs/2026-06-26-background-watch-design.md`;
  Plan: `docs/superpowers/plans/2026-06-26-background-watch.md`.

Stubbed (the "other parts" planned below): `internal/monitor`, `internal/agent`,
`internal/self`, `internal/recorder`.

## Sequence to a testable release

1. **Watch loop** (parked plan) — makes the dashboard live. *small*
2. **Monitor / warn engine** (`internal/monitor`) — the wedge: detect alarming
   write behavior → risk. *large* (decision settled below)
3. **`install-agent`** (`internal/agent`) — the "prevent" leg. *small–medium*
4. **`self`** (`internal/self`) — footprint honesty. *small*
5. **`recorder`** (`internal/recorder`) — session receipts. *small* (optional for
   the first test cut)
6. **Release** — tagged prebuilt macOS/Linux binaries. *small, mostly CI*

Dependencies: the monitor's risk output feeds the dashboard banner (supersedes the
size-only banner from the watch loop, so do the watch loop first, then the monitor).
`self` reads the history size produced by `recorder`, so if both are built, do
`recorder` before `self`'s footprint shows history.

---

## Part 1 — Monitor / warn engine (`internal/monitor`)

**Goal:** turn raw Codex activity into a plain-language risk level
(LOW/MEDIUM/HIGH/CRITICAL) the dashboard can surface, catching "Codex is hammering
the disk" — not just "logs are big."

**DECISION (measurement method):** v1 uses the **WAL-growth-rate proxy** — sample the
total Codex log bytes (and the `-wal` size) over time and compute the growth rate in
MB/min from the deltas. Rationale: standard-library only, cross-platform, no cgo,
and it keeps the "lightweight, no-deps engine" promise. Real per-process write
counters (`/proc/<pid>/io` on Linux; `proc_pid_rusage` via cgo on macOS) catch
write-amplification that doesn't grow the file, but are platform-specific and
heavier — **deferred to Phase 4**. (Override here if you want process counters now.)

**Design (low-write, samples in RAM only):**
- `Sample{At time.Time; TotalBytes int64; WALBytes int64; DiskFreeBytes int64}` —
  one reading. The watch-loop tick produces these from the existing read-only scan
  (plus a `disk free` stat); they are held in a small in-memory ring, never written
  to disk.
- `Risk` level + `Thresholds` already scaffolded in `internal/monitor/risk.go`
  (`DefaultThresholds`: medium 25, high 100, critical 500 MB/min; WAL high 1024 MiB,
  critical 8192 MiB).
- `Evaluate(samples []Sample, t Thresholds) Assessment` — **pure function**:
  computes write-rate MB/min from the newest deltas, peak rate, current WAL size,
  and an idle-writer signal (growth while Codex idle), and returns
  `{Level Risk; RateMBMin float64; WALBytes int64; Reasons []string}`.
- Stale/idle-writer detection: growth continuing while `IsCodexRunning` is false →
  escalate (a deleted/stale WAL held open is the dangerous case from the spec).

**Integration:** the dashboard banner becomes risk-driven (HIGH/CRITICAL shown
prominently with the rate and the top reason), replacing the size-only deadweight
line. The watch-loop tick appends a `Sample` and re-evaluates.

**Slices (each its own TDD plan at execution):**
1. `Sample` + in-memory ring buffer (cap N), pure, injected timestamps. Tests:
   ring eviction, ordering.
2. `Evaluate` risk function (rate from deltas, WAL thresholds, idle-writer,
   reasons). Tests: table-driven per level; mutation-test the thresholds.
3. Wire into the TUI: tick → append sample → `Evaluate` → risk banner + rate line.
4. (Optional) disk-free sampling + "disk filling" CRITICAL.

**Testing:** all decision logic is pure functions over injected samples — deterministic,
no real timers/processes. Mutation-test the risk thresholds the way we did the
cleaner safety gates.

---

## Part 2 — `install-agent` (`internal/agent`)

**Goal:** write a disk/token-safe `AGENTS.md` into a repo so the agent makes less
mess at the source. (Design already brainstormed earlier; carried here.)

**Settled design:**
- CLI: `codexssd install-agent [--profile <name>] [--force] [--print] [dir]`.
  Default profile `balanced`; dir defaults to `.`.
- Profiles (all four, just distinct rule text): `balanced` (default), `strict`,
  `repo-only`, `disk-token-safe`.
- Existing-file safety: refuse if `AGENTS.md` exists unless `--force`; every
  generated file carries a hidden marker (`<!-- codexssd:generated profile=… -->`)
  so `--force` can tell our file from a hand-written one and warn harder on the
  latter. `--print` writes to stdout (preview), touches nothing. No process gate
  (doesn't touch Codex logs).

**Slices:**
1. `profiles.go`: `Parse(name)`, `Profiles`, `Content(p)` (marker + intro + rules);
   tests per profile + invalid name.
2. `install.go`: `Install(dir, p, force) (path, replacedForeign, err)` + `ErrExists`
   + `isGenerated`; temp-dir tests (write/refuse/force/foreign).
3. CLI wiring (`cmdInstallAgent`, flags, `--print`); tests.
4. (Later) surface as an in-app action in the dashboard.

---

## Part 3 — `self` (`internal/self`)

**Goal:** honestly report CodexSSD's own footprint so it proves it isn't the problem.

**Design:**
- `Measure() (Report, error)` where `Report{OwnWriteBytes int64; Mode string;
  HistoryBytes int64}`. v1: `Mode = "low-write"`; `HistoryBytes` = size of
  `~/.codexssd` (the recorder's dir); `OwnWriteBytes` = bytes written this session
  if cheaply known (else 0/omit). Pure-ish: take the dir as an argument for tests.
- CLI: `codexssd self` (+ `--json`). Plain-language output.
- (Later) a footprint line on the dashboard.

**Slices:**
1. `Measure(dir)` + dir-size helper; temp-dir tests.
2. CLI wiring (`cmdSelf`, `--json`); tests.

---

## Part 4 — `recorder` (`internal/recorder`)

**Goal:** append one JSONL line per session to `~/.codexssd/sessions.jsonl` (no DB),
so we have a history of what happened and `self` has a history size to report.

**Design (already scaffolded):**
- `Receipt{At, DurationSec, DiskWritten, PeakMBPerMin, FilesChanged, Risk}`.
- `Append(r Receipt) error` — append-only JSONL; create dir if needed.
- History cap (from the spec config: `max_mb: 10`, `max_days: 30`): trim oldest
  lines on append when exceeded.
- `Path()` already returns `~/.codexssd/sessions.jsonl`.

**Slices:**
1. `Append` + read-back helper; temp-dir tests (append, multiple lines).
2. History cap/trim (by age and size); tests with synthetic old lines.
3. (Integration) the dashboard/watch loop writes one receipt on exit.

**Note:** a "session" only has meaning once the app runs continuously (the watch
loop) — so `recorder` integration naturally follows the watch loop. The pure
`Append`/trim logic can be built and tested independently first.

---

## Part 5 — Release (so testers can install)

**Goal:** a tagged GitHub release with prebuilt binaries for macOS (arm64/amd64) and
Linux (amd64/arm64), so non-developers can download and run one file.

**Approach:** a release workflow (GoReleaser, or a hand-rolled `actions/setup-go`
matrix build) triggered on a `v*` tag; attach binaries to the GitHub Release.
Homebrew tap is a later nicety. README already has build-from-source instructions.

---

## Decisions (settled 2026-06-26)

1. **Monitor measurement method = WAL-growth-rate proxy (Option A).** Stdlib-only,
   cross-platform, lightweight; catches the original Codex log-ballooning case. Real
   per-process write counters (cgo/`/proc`) are deferred to Phase 4.
2. **Build order = follow the sequence:** watch loop → monitor → install-agent →
   self → recorder → release. (Make it live first, then build the warn "brain.")

## Out of scope for Phase 1 (later phases, unchanged)

Deep relationship-map, behavioural detection, other AI tools (Claude Code/Cursor/
Gemini), cost/token tracking, daemon, menu-bar app, team settings.
