# CodexSSD — remaining tickets (Claude Code parity round)

Drafted 2026-07-22 for import into Linear (or any tracker). Source of truth:
spec `docs/superpowers/specs/2026-07-21-claude-code-parity-design.md`, plan
`docs/superpowers/plans/2026-07-21-claude-code-parity.md`, progress ledger
`.superpowers/sdd/progress.md`.

**Shipped on `feat/claude-code-parity` (commits 5d75c31..13695a9, all
review-approved):** `tool.ScanDirSize`, `tool.ProcessMemory`,
`watch --tool claude`, MCP `tool` argument on all five read-only tools.

---

## Ticket 1 — Ship the CLI-complete MVP: docs + verification + PR

**Priority:** High · **Estimate:** S · **Labels:** docs, release

Finish and merge `feat/claude-code-parity` at its current (CLI-complete) scope.

- README: "Works with Codex and Claude Code" callout at the top of Usage with a
  command × tool support matrix (watch row now ✓/✓; dashboard row = "✓ via the
  `l` Claude Code screen" until the tab bar ships); update the watch section
  (drop "Codex-only for now", add `--tool claude` example + JSON `tool` field);
  document the MCP `{"tool":"codex"|"claude"}` argument; slim "Beyond Codex" to
  the safety rationale.
- CLAUDE.md: sync watch/mcp bullets; note each MCP tool's optional argument.
- Run plan Task 10's gate: full build/vet/test/gofmt, codex regression sweep,
  claude smoke sweep.
- PR `feat/claude-code-parity` → `main`.

**Acceptance:** README/CLAUDE.md contain no stale "Codex-only" claims for
watch/MCP; gate green; PR open with test-plan evidence.

---

## Ticket 2 — TUI: per-tool risk sampling + peak-tool session receipts

**Priority:** Medium · **Estimate:** M · **Labels:** tui, phase-4

Plan Task 6, deferred from the MVP. Dashboard samples BOTH tools every poll so
the (future) tab switch never shows an empty rate history.

- `loadedClaudeMsg` gains `at`/`totalBytes` (via `tool.ScanDirSize`)/`memBytes`
  (via `tool.ProcessMemory`); seams `scanClaudeSize`, `claudeMemory`.
- Model gains `claudeSamples`/`claudeAssessment`/`claudeMemBytes`/
  `claudeTotalBytes` + claude session peaks; `notifyCmd(a, label)` so Claude
  escalations notify with the right display name.
- `sessionReceipt` reports the harder-peaking tool, Action `"session"` (codex,
  historical) or `"session --tool claude"` per the recorder convention.

**Acceptance:** plan Task 6's two tests pass (`TestClaudeLoadFeedsRiskSamples`,
`TestSessionReceiptReportsPeakTool` + codex-default receipt pin); codex
assessment untouched by claude loads; full gate green.

---

## Ticket 3 — TUI: tab-scoped dashboard — state collapse + key routing

**Priority:** Medium · **Estimate:** M · **Labels:** tui, phase-4 · **Blocked by:** Ticket 2

Plan Task 7. Fold the four `stateClaude*` states into the generic
confirm/restore states keyed by a new `activeTab`; `tab` cycles tools, `1`/`2`
jump, `l` stays as the Claude alias. Per-tool running-check gates preserved
exactly (each tool's clean/restore command re-checks its own `IsRunning`).

**Acceptance:** plan Task 7 tests pass (tab switching; `c` on the Claude tab
hits Claude's gates, never Codex's); `stateClaude*` and `returnState` deleted;
all rewritten claude_test.go expectations preserved behaviorally; gate green.

---

## Ticket 4 — TUI: tab-bar view + per-tab panels

**Priority:** Medium · **Estimate:** M · **Labels:** tui, phase-4 · **Blocked by:** Ticket 3

Plan Task 8. Persistent `[ Codex ] Claude Code` tab bar under the logo; the
active tab drives folder/Risk/Recycling-bin panels, banner, footer
(`tab switch tool · …`), and help; the old dedicated Claude screen's itemized
listing becomes the Claude tab's folder panel; delete the dead renderers.
Includes a manual TUI drive at wide + narrow widths, and the README/CLAUDE.md
dashboard-section update to describe tabs (superseding Ticket 1's "l screen"
wording).

**Acceptance:** plan Task 8's three view tests pass; no `renderClaude*`
leftovers; manual drive checklist done; gate green.

---

## Ticket 5 — Hardening backlog from this round's reviews (all Minor)

**Priority:** Low · **Estimate:** S · **Labels:** cleanup

Review-approved-with-minors, deliberately deferred:

- `tool.ScanDirSize` counts non-regular entries (symlink own-size); add a
  `d.Type().IsRegular()` guard or document the behavior.
- `tool.ScanDirSize` doesn't exclude a root dir itself named
  `codexssd-backups` (no current caller passes one; guard or document).
- `tool.ProcessMemory`: process exiting between `DetectProcesses` and the
  `ps -o rss=` call surfaces as `(0, err)` instead of `(0, nil)` (mirrors the
  codex original; decide once for both).

**Acceptance:** each item either fixed with a test or explicitly documented as
intended; gate green.
