# Background Watch Loop Implementation Plan

> **Status: PARKED.** Approved + ready, not yet executed. Resume with superpowers:subagent-driven-development when we return to the frontend.
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the interactive dashboard live — poll `~/.codex` every 30s (read-only) and show a passive, stateless deadweight banner (actionable when Codex is idle, informational while it's active).

**Architecture:** A self-rescheduling Bubble Tea `tea.Tick` re-runs the existing read-only `loadCmd`; the deadweight banner is a pure function of the loaded state. No new state machine, no snooze. All in `internal/tui`.

**Tech Stack:** Go 1.25; existing `bubbletea`/`lipgloss` (no new deps).

## Global Constraints

- **Passive banner only** — the app never auto-switches screens or rings a bell.
- **Fixed cadence:** `pollInterval = 30 * time.Second`.
- **Read-only polling:** the tick re-runs `loadCmd` (`ScanLogs`/`IsCodexRunning`/`ListBackups`) — no file mutation, no new writes.
- **`loadedMsg` must never change `m.state`** — a refresh landing mid-action is harmless; the `cleanCmd`/`restoreCmd` gates stay authoritative.
- All changes confined to `internal/tui`; the `internal/monitor` stubs are NOT touched.
- Deadweight threshold is the existing `deadweightThreshold` (100 MiB).
- Naming: CodexSSD / `codexssd`.
- Verification gate: `go build ./... && go vet ./... && go test ./...` green and `gofmt -l .` empty before each commit. The TUI program is never started from tests.

---

## File Structure

- `internal/tui/model.go` — **modify.** Add `pollInterval` const.
- `internal/tui/commands.go` — **modify.** Add `tickMsg` + `tickCmd`.
- `internal/tui/update.go` — **modify.** `Init` batches load+tick; handle `tickMsg`.
- `internal/tui/view.go` — **modify.** `bannerState` pure function + dashboard banner + watching indicator.
- `internal/tui/*_test.go` — **modify.** Banner + tick + refresh-invariant tests.

Current relevant code (already on `staging`):
- `loadCmd func() tea.Msg` returns `loadedMsg{report, running, supported, runErr, loadErr, plan, backups}` (read-only).
- `Model.deadweight() bool` returns `report.TotalBytes >= deadweightThreshold`.
- `Model.Init()` currently returns `loadCmd`.
- `renderDashboard` currently prints a `⚠ … worth tidying` / `Nothing alarming` line plus a separate running/idle line and a footer.

---

## Task 1: Polling tick (keep the dashboard live)

**Files:**
- Modify: `internal/tui/model.go` (add `pollInterval`), `internal/tui/commands.go` (`tickMsg`/`tickCmd`), `internal/tui/update.go` (`Init` batch + `tickMsg` handling)
- Test: `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `loadCmd` (existing), `tea.Tick`, `tea.Batch`.
- Produces: `type tickMsg struct{}`, `func tickCmd() tea.Cmd`, `const pollInterval = 30 * time.Second`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/update_test.go`:

```go
func TestTickKeepsWatchingWithoutChangingState(t *testing.T) {
	m, _ := step(New(), sampleLoaded()) // on dashboard
	next, cmd := step(m, tickMsg{})
	if next.state != stateDashboard {
		t.Errorf("tick changed state to %v, want stateDashboard", next.state)
	}
	if cmd == nil {
		t.Error("tick should re-dispatch a command (reload + reschedule)")
	}
}

func TestLoadedMsgDoesNotChangeState(t *testing.T) {
	m, _ := step(New(), sampleLoaded())
	m.state = stateConfirmClean // user is mid-confirm
	next, _ := step(m, sampleLoaded())
	if next.state != stateConfirmClean {
		t.Errorf("a refresh changed state to %v, want stateConfirmClean", next.state)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTick|TestLoadedMsgDoesNotChangeState' -v`
Expected: FAIL — `tickMsg` undefined.

- [ ] **Step 3: Add `pollInterval`**

In `internal/tui/model.go`, add near the other consts (ensure `time` is imported — it already is for `lastTidy`):

```go
// pollInterval is how often the open dashboard re-checks ~/.codex (read-only).
const pollInterval = 30 * time.Second
```

- [ ] **Step 4: Add `tickMsg` and `tickCmd`**

In `internal/tui/commands.go`, add (and ensure `time` is imported — it already is for `applyPlan`):

```go
// tickMsg fires on the poll interval to keep the dashboard live.
type tickMsg struct{}

// tickCmd schedules the next poll tick.
func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}
```

- [ ] **Step 5: Batch load+tick in Init, and handle tickMsg**

In `internal/tui/model.go`, change `Init`:

```go
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadCmd, tickCmd())
}
```

In `internal/tui/update.go`, add a `tickMsg` case to the `Update` type switch (alongside `loadedMsg`):

```go
	case tickMsg:
		// Re-check ~/.codex and schedule the next tick. Does not touch m.state.
		return m, tea.Batch(loadCmd, tickCmd())
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (new tests + all prior).

- [ ] **Step 7: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): poll ~/.codex every 30s to keep the dashboard live"
```

---

## Task 2: Passive deadweight banner

**Files:**
- Modify: `internal/tui/view.go` (`bannerState` + dashboard rendering + watching indicator)
- Test: `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `Model.deadweight()`, `m.running`, `m.supported`, `m.report.TotalBytes`, `codex.HumanBytes`.
- Produces: `type banner int`, `const (bannerCalm banner = iota; bannerActionable; bannerInformational)`, `func (m Model) bannerState() banner`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/update_test.go`:

```go
func TestBannerActionableWhenIdleDeadweight(t *testing.T) {
	m, _ := step(New(), sampleLoaded()) // 200 MiB, not running
	if got := m.bannerState(); got != bannerActionable {
		t.Errorf("bannerState = %v, want bannerActionable", got)
	}
	if !strings.Contains(m.View(), "press c to tidy") {
		t.Errorf("actionable banner missing 'press c to tidy':\n%s", m.View())
	}
}

func TestBannerInformationalWhenCodexActive(t *testing.T) {
	m, _ := step(New(), sampleLoaded())
	m.running = true
	if got := m.bannerState(); got != bannerInformational {
		t.Errorf("bannerState = %v, want bannerInformational", got)
	}
	view := m.View()
	if strings.Contains(view, "press c to tidy") {
		t.Errorf("informational banner should not prompt 'press c' while Codex active:\n%s", view)
	}
}

func TestBannerCalmBelowThreshold(t *testing.T) {
	msg := sampleLoaded()
	msg.report.TotalBytes = 1 * 1024 * 1024
	m, _ := step(New(), msg)
	if got := m.bannerState(); got != bannerCalm {
		t.Errorf("bannerState = %v, want bannerCalm", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run TestBanner -v`
Expected: FAIL — `bannerState`/`banner` undefined.

- [ ] **Step 3: Add the banner state function**

In `internal/tui/view.go`, add:

```go
// banner classifies what the dashboard's deadweight line should say.
type banner int

const (
	bannerCalm banner = iota // nothing worth tidying
	bannerActionable         // deadweight + Codex idle → offer to tidy now
	bannerInformational      // deadweight + Codex active → can't act yet
)

// bannerState is a pure function of the current load: it never tracks history.
func (m Model) bannerState() banner {
	if !m.deadweight() {
		return bannerCalm
	}
	if m.supported && !m.running {
		return bannerActionable
	}
	return bannerInformational
}
```

- [ ] **Step 4: Render the banner + watching indicator**

In `internal/tui/view.go` `renderDashboard`, replace the existing deadweight block (the `if m.deadweight() { … } else { … }` lines and the running/idle status switch) with a banner driven by `bannerState`:

```go
	switch m.bannerState() {
	case bannerActionable:
		fmt.Fprintf(&b, "⚠  %s of Codex logs piled up — press c to tidy.\n", codex.HumanBytes(m.report.TotalBytes))
	case bannerInformational:
		fmt.Fprintf(&b, "⚠  %s piling up — I'll offer to tidy when Codex is closed.\n", codex.HumanBytes(m.report.TotalBytes))
	default:
		fmt.Fprintln(&b, "Nothing alarming right now.")
	}

	switch {
	case !m.supported:
		fmt.Fprintln(&b, "(This platform can't check whether Codex is running.)")
	case m.running:
		fmt.Fprintln(&b, "Codex appears to be running.")
	default:
		fmt.Fprintln(&b, "Codex doesn't appear to be running.")
	}
```

Then, just before the final footer line in `renderDashboard`, add the watching indicator:

```go
	fmt.Fprintln(&b, "watching ~/.codex · updates every 30s")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (banner tests + all prior, including the Task 2 dashboard tests, which still find the size string and a `tidy` substring).

- [ ] **Step 6: Verify build/vet/format and exercise**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green. (Optional manual: `HOME=/tmp/cssd-tui go run ./cmd/codexssd` with a >100 MiB fake log shows the actionable banner and the watching line.)

- [ ] **Step 7: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): passive deadweight banner (actionable when Codex idle)"
```

---

## Self-Review notes

- **Spec coverage:** 30s read-only polling (Task 1); refresh-never-changes-state invariant (Task 1 test); passive banner with actionable/informational/calm states (Task 2); watching indicator (Task 2); no snooze state (stateless `bannerState`); confined to `internal/tui`; no new deps.
- **Type consistency:** `tickMsg`/`tickCmd`/`pollInterval` and `banner`/`bannerState` are used identically across tasks. `Init` returns `tea.Batch(loadCmd, tickCmd())`.
- **Verify before resuming:** when un-parking, confirm `sampleLoaded()` still builds a non-empty 200 MiB plan and that `renderDashboard`'s current deadweight/status lines match the block being replaced in Task 2 Step 4 (the dashboard may have shifted since this was written).
