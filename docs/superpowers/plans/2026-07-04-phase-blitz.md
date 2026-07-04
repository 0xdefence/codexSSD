# Phase 1–2 Completion Blitz + MCP Server — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every Phase 1 + Phase 2 gap (memory monitoring, config, disk-visibility report, recorder wiring, `watch`) and add a read-only stdio MCP server, per the approved spec at `docs/superpowers/specs/2026-07-04-phase-blitz-design.md`.

**Architecture:** Pure, testable engine functions in `internal/*` (inputs injected, no hidden I/O), thin command wiring in `cmd/codexssd/main.go`, seams as package-level function variables so tests can stub process/exec/notification calls. New packages: `internal/config`, `internal/visibility`, `internal/notify`, `internal/mcpserver`.

**Tech Stack:** Go stdlib only (the TUI's charmbracelet import remains the sole exception). JSON for config and all machine output. JSONL for CodexSSD's own state.

## Global Constraints

Every task implicitly includes these. They come from `CLAUDE.md` and the spec; **any violation is an automatic review reject.**

- **Move aside, never hard-delete.** No `os.Remove`/`os.RemoveAll`/`os.Rename` outside `internal/cleaner` and `internal/trash`.
- **Only Codex's own known log files** (`codex.LogFileNames`) may ever be acted on. Do not widen the allow-list.
- **When uncertain, report — never resolve.** All new commands in this plan are 100% read-only except receipt-writing to CodexSSD's own state dir.
- **No database.** CodexSSD's own storage stays append-only JSONL.
- **Check before touching:** mutating paths re-check `IsCodexRunning` immediately before acting.
- **No new third-party dependencies.** `go.mod` must not gain a require line in any task.
- **Receipt/notification failures never fail the user's action** — warn (CLI) or ignore (TUI/notify), continue.
- **JSON output:** empty collections emit `[]`, never `null` (precedent: commit d1465f0).
- Human sizes via `codex.HumanBytes` (binary units). Friendly plain-language output. Comment the *why*.
- Gate before claiming any task done: `go build ./... && go vet ./... && go test ./... && gofmt -l .` (gofmt output must be empty).
- Commit messages follow the repo's `type(scope): summary` convention.

**Execution order:** Tasks run in plan order, 1 → 7. Dependencies: Task 2 needs Task 1 (both touch `monitor.Thresholds`); Task 3 needs Task 2 (`loadConfig` + `StaleAfter`); Task 5 needs Tasks 1, 2, 4; Task 6 needs Tasks 2, 3; Task 7 needs everything. Only Task 4 is fully independent — it may run in parallel with Tasks 1–3, but note that Tasks 2–6 all edit `cmd/codexssd/main.go`, so parallel worktrees will conflict there; sequential execution is the safe default.

---

### Task 1: Memory monitoring (Wave 1 — merge first)

**Files:**
- Create: `internal/codex/memory.go`
- Create: `internal/codex/memory_test.go`
- Modify: `internal/monitor/samples.go` (add `MemBytes` to `Sample`)
- Modify: `internal/monitor/risk.go` (memory thresholds + escalation)
- Modify: `internal/monitor/risk_test.go` (new cases)
- Modify: `internal/tui/commands.go` (sample memory in `loadCmd`)
- Modify: `internal/tui/update.go:27` (put memory into the `monitor.Sample`)
- Modify: `internal/tui/view.go` (show memory line when available)

**Interfaces:**
- Consumes: `codex.DetectProcesses() ([]Process, error)`, `codex.ErrUnsupportedPlatform` (both exist in `internal/codex/process.go`).
- Produces: `codex.ProcessMemory() (int64, error)` (total RSS in **bytes** of Codex-like processes; `0, nil` when none run; `ErrUnsupportedPlatform` on Windows), `codex.ParseRSSKiB(string) int64`, `monitor.Sample.MemBytes int64`, `monitor.Thresholds.HighMemMB int64` / `.CriticalMemMB int64` (defaults **2048** / **6144**).

- [ ] **Step 1: Write failing parser tests**

`internal/codex/memory_test.go`:

```go
package codex

import "testing"

func TestParseRSSKiB(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"empty", "", 0},
		{"single", "1024\n", 1024},
		{"multiple with padding", "  512\n 1536\n\n", 2048},
		{"garbage lines skipped", "abc\n100\n", 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseRSSKiB(c.in); got != c.want {
				t.Errorf("ParseRSSKiB(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/codex/ -run TestParseRSSKiB` → FAIL (undefined: ParseRSSKiB)

- [ ] **Step 3: Implement `internal/codex/memory.go`**

```go
package codex

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// execRSS asks ps for the RSS (in KiB) of the given pids. A package-level seam
// so tests can substitute canned output without a real process table.
var execRSS = func(pids []string) ([]byte, error) {
	return exec.Command("ps", "-o", "rss=", "-p", strings.Join(pids, ",")).Output()
}

// ProcessMemory returns the total resident memory, in BYTES, of running
// Codex-like processes. When no Codex process is running it returns (0, nil) —
// absence of Codex is not an error. Windows returns ErrUnsupportedPlatform;
// callers must omit memory rather than fail.
//
// SAFETY: observation only — it never signals or alters a process.
func ProcessMemory() (int64, error) {
	if runtime.GOOS == "windows" {
		return 0, ErrUnsupportedPlatform
	}
	procs, err := DetectProcesses()
	if err != nil {
		return 0, err
	}
	if len(procs) == 0 {
		return 0, nil
	}
	pids := make([]string, 0, len(procs))
	for _, p := range procs {
		pids = append(pids, strconv.Itoa(p.PID))
	}
	out, err := execRSS(pids)
	if err != nil {
		return 0, err
	}
	// ps reports RSS in KiB on both darwin and linux.
	return ParseRSSKiB(string(out)) * 1024, nil
}

// ParseRSSKiB sums the per-line RSS values (KiB) in `ps -o rss=` output.
// Non-numeric lines are skipped rather than treated as errors.
func ParseRSSKiB(psOut string) int64 {
	var total int64
	for _, line := range strings.Split(psOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kib, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		total += kib
	}
	return total
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/codex/` → PASS

- [ ] **Step 5: Write failing risk-engine tests** — append to `internal/monitor/risk_test.go`:

```go
func TestEvaluateMemoryEscalation(t *testing.T) {
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	th := DefaultThresholds()

	t.Run("high memory escalates to HIGH", func(t *testing.T) {
		s := []Sample{{At: base, TotalBytes: 0, MemBytes: 3 * 1024 * 1024 * 1024}} // 3 GiB
		a := Evaluate(s, true, th)
		if a.Level != RiskHigh {
			t.Errorf("Level = %v, want HIGH", a.Level)
		}
	})

	t.Run("critical memory escalates to CRITICAL", func(t *testing.T) {
		s := []Sample{{At: base, MemBytes: 7 * 1024 * 1024 * 1024}} // 7 GiB
		a := Evaluate(s, true, th)
		if a.Level != RiskCritical {
			t.Errorf("Level = %v, want CRITICAL", a.Level)
		}
	})

	t.Run("modest memory stays LOW", func(t *testing.T) {
		s := []Sample{{At: base, MemBytes: 512 * 1024 * 1024}}
		if a := Evaluate(s, true, th); a.Level != RiskLow {
			t.Errorf("Level = %v, want LOW", a.Level)
		}
	})

	t.Run("zero mem thresholds disable the check", func(t *testing.T) {
		off := th
		off.HighMemMB, off.CriticalMemMB = 0, 0
		s := []Sample{{At: base, MemBytes: 64 * 1024 * 1024 * 1024}}
		if a := Evaluate(s, true, off); a.Level != RiskLow {
			t.Errorf("Level = %v, want LOW when disabled", a.Level)
		}
	})
}
```

- [ ] **Step 6: Run to verify failure** — `go test ./internal/monitor/ -run TestEvaluateMemoryEscalation` → FAIL (unknown field MemBytes)

- [ ] **Step 7: Implement monitor changes**

`internal/monitor/samples.go` — extend `Sample`:

```go
// Sample is a point-in-time reading of Codex's log sizes and memory use.
type Sample struct {
	At         time.Time
	TotalBytes int64 // total size of Codex's known log files
	WALBytes   int64 // size of logs_2.sqlite-wal
	MemBytes   int64 // total RSS of Codex processes (0 when unknown/not running)
}
```

`internal/monitor/risk.go` — extend `Thresholds` and `DefaultThresholds`:

```go
type Thresholds struct {
	MediumMBPerMin    float64
	HighMBPerMin      float64
	CriticalMBPerMin  float64
	HighWALSizeMB     int64
	CriticalWALSizeMB int64
	HighMemMB         int64 // Codex RSS at/above this is HIGH (0 disables)
	CriticalMemMB     int64 // Codex RSS at/above this is CRITICAL (0 disables)
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		MediumMBPerMin:    25,
		HighMBPerMin:      100,
		CriticalMBPerMin:  500,
		HighWALSizeMB:     1024,
		CriticalWALSizeMB: 8192,
		HighMemMB:         2048,
		CriticalMemMB:     6144,
	}
}
```

In `Evaluate`, after the WAL block (before the idle-writer block), add:

```go
	// Memory can escalate too: a Codex eating RAM is the same "quietly hurting
	// your machine" problem as a bloating WAL. Zero thresholds disable the check.
	memMB := newest.MemBytes / (1024 * 1024)
	if t.CriticalMemMB > 0 && memMB >= t.CriticalMemMB {
		a.Level = maxRisk(a.Level, RiskCritical)
		a.Reasons = append(a.Reasons, fmt.Sprintf("Codex is using %d MiB of memory", memMB))
	} else if t.HighMemMB > 0 && memMB >= t.HighMemMB {
		a.Level = maxRisk(a.Level, RiskHigh)
		a.Reasons = append(a.Reasons, fmt.Sprintf("Codex is using %d MiB of memory", memMB))
	}
```

- [ ] **Step 8: Run to verify pass** — `go test ./internal/monitor/` → PASS (including all pre-existing tests, unchanged)

- [ ] **Step 9: Wire into the TUI**

`internal/tui/commands.go` — add a seam next to the others and carry memory through `loadedMsg`:

```go
// in the var ( ... ) seam block:
	codexMemory = codex.ProcessMemory

// loadedMsg gains one field:
	memBytes  int64 // total Codex RSS (0 when unknown)

// in loadCmd, after the isCodexRunning call:
	mem, _ := codexMemory() // best-effort; 0 on any error — never blocks the dashboard
```
…and set `memBytes: mem` in the returned `loadedMsg`.

`internal/tui/update.go:27` — include memory in the sample:

```go
		s := monitor.Sample{At: msg.at, TotalBytes: msg.report.TotalBytes, WALBytes: walBytes(msg.report), MemBytes: msg.memBytes}
```

`internal/tui/model.go` — add `memBytes int64` to `Model`'s status fields; set it from `msg.memBytes` where `update.go` copies the other `loadedMsg` fields.

`internal/tui/view.go` — in the dashboard status area (next to where `m.running` is rendered), show memory only when we actually have a reading:

```go
	if m.running && m.memBytes > 0 {
		// e.g. "Codex memory:   1.5 GiB"
		b.WriteString(fmt.Sprintf("Codex memory:  %s\n", codex.HumanBytes(m.memBytes)))
	}
```
(Adapt to `view.go`'s actual string-building style — match surrounding code.)

- [ ] **Step 10: Add a TUI test** — in `internal/tui/update_test.go`, extend an existing `loadedMsg`-handling test (follow its pattern) to assert that a `loadedMsg{memBytes: 3 << 30, ...}` produces `m.samples[len-1].MemBytes == 3<<30`.

- [ ] **Step 11: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → all green, empty gofmt output.

- [ ] **Step 12: Commit**

```bash
git add internal/codex internal/monitor internal/tui
git commit -m "feat(monitor): Codex memory sampling + RSS risk escalation"
```

---

### Task 2: Config file (`internal/config`) (Wave 1 — after Task 1 merges)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `internal/cleaner/apply.go` (parameterized hold: `ApplyWithHold`)
- Modify: `internal/cleaner/apply_test.go` (hold test)
- Modify: `cmd/codexssd/main.go` (`cmdClean` uses config hold; warn-and-proceed on bad config)
- Modify: `internal/tui/model.go`, `internal/tui/commands.go`, `internal/tui/update.go` (config-driven thresholds, poll interval, hold)

**Interfaces:**
- Consumes: `monitor.Thresholds` incl. `HighMemMB`/`CriticalMemMB` (Task 1), `recorder.Dir() (string, error)`, `cleaner.RetentionDays`, `cleaner.Plan.Apply(now time.Time) (string, error)`.
- Produces:
  - `config.Config` struct (fields below), `config.Default() Config`
  - `config.Load(path string) (Config, error)` — missing file → `(Default(), nil)`; malformed → `(Default(), err)` (callers warn and proceed)
  - `config.DefaultPath() (string, error)` → `<recorder.Dir()>/config.json`
  - `config.LoadDefault() (Config, error)`
  - Methods: `MonitorThresholds() monitor.Thresholds`, `PollInterval() time.Duration`, `BinHold() time.Duration`, `StaleAfter() time.Duration`
  - `cleaner.(Plan) ApplyWithHold(now time.Time, hold time.Duration) (string, error)`; existing `Apply(now)` delegates with the 14-day default.

- [ ] **Step 1: Write failing config tests**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("missing file must not error, got %v", err)
	}
	if cfg != Default() {
		t.Errorf("got %+v, want defaults", cfg)
	}
}

func TestLoadPartialFileOverlaysDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"high_mb_per_min": 50, "notifications": false}`), 0o600)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HighMBPerMin != 50 {
		t.Errorf("HighMBPerMin = %v, want 50", cfg.HighMBPerMin)
	}
	if cfg.Notifications {
		t.Error("Notifications should be false when explicitly set")
	}
	if cfg.CriticalMBPerMin != Default().CriticalMBPerMin {
		t.Error("unset fields must keep defaults")
	}
}

func TestLoadMalformedReturnsDefaultsAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{not json`), 0o600)
	cfg, err := Load(path)
	if err == nil {
		t.Fatal("want an error for malformed config")
	}
	if cfg != Default() {
		t.Error("malformed config must fall back to full defaults, not a partial parse")
	}
}

func TestDurationHelpers(t *testing.T) {
	cfg := Default()
	if cfg.PollInterval() != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", cfg.PollInterval())
	}
	if cfg.BinHold() != 14*24*time.Hour {
		t.Errorf("BinHold = %v, want 336h", cfg.BinHold())
	}
	if cfg.StaleAfter() != 30*24*time.Hour {
		t.Errorf("StaleAfter = %v, want 720h", cfg.StaleAfter())
	}
	th := cfg.MonitorThresholds()
	if th.HighMemMB != 2048 || th.MediumMBPerMin != 25 {
		t.Errorf("MonitorThresholds mismatch: %+v", th)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/config/` → FAIL (no such package yet)

- [ ] **Step 3: Implement `internal/config/config.go`**

```go
// Package config loads CodexSSD's optional user configuration. The contract:
// config can NEVER brick the tool. A missing file means defaults; a malformed
// file means defaults plus an error the caller should surface as a warning and
// then carry on.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/recorder"
)

// FileName is the config file inside CodexSSD's state dir (~/.codexssd).
const FileName = "config.json"

// Config is the user-editable configuration. All fields are optional in the
// file; absent fields keep their defaults (we unmarshal onto Default()).
type Config struct {
	MediumMBPerMin      float64 `json:"medium_mb_per_min"`
	HighMBPerMin        float64 `json:"high_mb_per_min"`
	CriticalMBPerMin    float64 `json:"critical_mb_per_min"`
	HighWALSizeMB       int64   `json:"high_wal_size_mb"`
	CriticalWALSizeMB   int64   `json:"critical_wal_size_mb"`
	HighMemMB           int64   `json:"high_mem_mb"`
	CriticalMemMB       int64   `json:"critical_mem_mb"`
	PollIntervalSeconds int     `json:"poll_interval_seconds"`
	BinHoldDays         int     `json:"bin_hold_days"`
	Notifications       bool    `json:"notifications"`
	StaleAfterDays      int     `json:"stale_after_days"`
}

// Default returns the documented defaults (mirrors monitor.DefaultThresholds).
func Default() Config {
	t := monitor.DefaultThresholds()
	return Config{
		MediumMBPerMin:      t.MediumMBPerMin,
		HighMBPerMin:        t.HighMBPerMin,
		CriticalMBPerMin:    t.CriticalMBPerMin,
		HighWALSizeMB:       t.HighWALSizeMB,
		CriticalWALSizeMB:   t.CriticalWALSizeMB,
		HighMemMB:           t.HighMemMB,
		CriticalMemMB:       t.CriticalMemMB,
		PollIntervalSeconds: 30,
		BinHoldDays:         cleaner.RetentionDays,
		Notifications:       true,
		StaleAfterDays:      30,
	}
}

// Load reads the config at path. Missing file → (Default(), nil). Malformed
// file → (Default(), error): the caller warns and proceeds with defaults —
// a broken config file must never make the tool unusable.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Default(), err
	}
	// Unmarshal onto the defaults: absent keys keep their default values,
	// including bools that default to true.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("config file %s is not valid JSON: %w", path, err)
	}
	return cfg, nil
}

// DefaultPath is <state-dir>/config.json — one home for everything CodexSSD owns.
func DefaultPath() (string, error) {
	dir, err := recorder.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// LoadDefault loads the config from its default path.
func LoadDefault() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Default(), err
	}
	return Load(path)
}

// MonitorThresholds maps the config onto the risk engine's thresholds.
func (c Config) MonitorThresholds() monitor.Thresholds {
	return monitor.Thresholds{
		MediumMBPerMin:    c.MediumMBPerMin,
		HighMBPerMin:      c.HighMBPerMin,
		CriticalMBPerMin:  c.CriticalMBPerMin,
		HighWALSizeMB:     c.HighWALSizeMB,
		CriticalWALSizeMB: c.CriticalWALSizeMB,
		HighMemMB:         c.HighMemMB,
		CriticalMemMB:     c.CriticalMemMB,
	}
}

// PollInterval is how often watchers re-check ~/.codex (minimum 5s).
func (c Config) PollInterval() time.Duration {
	if c.PollIntervalSeconds < 5 {
		return 5 * time.Second
	}
	return time.Duration(c.PollIntervalSeconds) * time.Second
}

// BinHold is how long moved-aside backups are held before release (minimum 1 day).
func (c Config) BinHold() time.Duration {
	if c.BinHoldDays < 1 {
		return 24 * time.Hour
	}
	return time.Duration(c.BinHoldDays) * 24 * time.Hour
}

// StaleAfter is the age past which the disk report flags an entry as stale.
func (c Config) StaleAfter() time.Duration {
	if c.StaleAfterDays < 1 {
		return 24 * time.Hour
	}
	return time.Duration(c.StaleAfterDays) * 24 * time.Hour
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/config/` → PASS. Add clamp tests for `PollIntervalSeconds: 0` → 5s and `BinHoldDays: 0` → 24h to `TestDurationHelpers` and confirm they pass.

- [ ] **Step 5: Write failing hold test** — append to `internal/cleaner/apply_test.go` (follow its existing setup pattern for creating fake logs in a `t.TempDir()`):

```go
func TestApplyWithHoldSetsManifestHoldUntil(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "logs_2.sqlite"), []byte("x"), 0o600)
	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	dest, err := plan.ApplyWithHold(now, 3*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(dest)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(3 * 24 * time.Hour); !m.HoldUntil.Equal(want) {
		t.Errorf("HoldUntil = %v, want %v", m.HoldUntil, want)
	}
}
```

- [ ] **Step 6: Run to verify failure** — `go test ./internal/cleaner/ -run TestApplyWithHold` → FAIL (undefined ApplyWithHold)

- [ ] **Step 7: Implement `ApplyWithHold`** — in `internal/cleaner/apply.go`, rename the body of `Apply` to `ApplyWithHold(now time.Time, hold time.Duration)`, replace `HoldUntil: now.AddDate(0, 0, RetentionDays)` with `HoldUntil: now.Add(hold)`, and keep `Apply` as the compatible wrapper:

```go
// Apply moves the planned files aside with the default 14-day hold.
func (p Plan) Apply(now time.Time) (string, error) {
	return p.ApplyWithHold(now, RetentionDays*24*time.Hour)
}
```

- [ ] **Step 8: Run to verify pass** — `go test ./internal/cleaner/` → PASS (all pre-existing Apply tests still green).

- [ ] **Step 9: Wire config into `cmdClean` and the TUI**

`cmd/codexssd/main.go` — add a helper and use it in `cmdClean` (and later tasks):

```go
// loadConfig returns the user config, warning (not failing) on a malformed
// file — a broken config must never block a command.
func loadConfig() config.Config {
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: note: %v — using default settings.\n", err)
	}
	return cfg
}
```
In `cmdClean`, replace `plan.Apply(time.Now())` with:

```go
	cfg := loadConfig()
	dest, err := plan.ApplyWithHold(time.Now(), cfg.BinHold())
```

`internal/tui` — thread config through the model instead of package constants:
- `model.go`: add `cfg config.Config` to `Model`; `New()` becomes `New(cfg config.Config)`; `Run()` calls `config.LoadDefault()` (ignore the error — defaults returned — the TUI has nowhere good to warn) and passes it in. Keep `pollInterval` const as fallback but read `m.cfg.PollInterval()` where ticks are scheduled: change `tickCmd()` in `commands.go` to `tickCmd(interval time.Duration) tea.Cmd` and update both call sites (`Init`, tick handling in `update.go`).
- `update.go:29`: replace `monitor.DefaultThresholds()` with `m.cfg.MonitorThresholds()`.
- `commands.go`: in the `applyPlan` seam, use `p.ApplyWithHold(time.Now(), hold)` — add a `binHold` package var default `RetentionDays*24*time.Hour`… simplest correct wiring: change the seam to `applyPlan = func(p cleaner.Plan, hold time.Duration) (string, int64, error)` and pass `m.cfg.BinHold()` from the caller in `update.go`.
- `update_test.go`: construct models with `New(config.Default())`; adjust seam signatures in tests.

- [ ] **Step 10: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 11: Commit**

```bash
git add internal/config internal/cleaner internal/tui cmd/codexssd
git commit -m "feat(config): optional ~/.codexssd/config.json for thresholds, intervals, hold, notifications"
```

---

### Task 3: Disk-visibility report (`codexssd report`) (Wave 1)

**Files:**
- Create: `internal/visibility/scan.go`
- Create: `internal/visibility/scan_test.go`
- Modify: `cmd/codexssd/main.go` (new `report` command + usage line)
- Modify: `cmd/codexssd/main_test.go` (render test)

**Interfaces:**
- Consumes: `codex.Dir()`, `codex.HumanBytes(int64) string`, `cleaner.BackupDirName` (= `"codexssd-backups"`), and from Task 2: `loadConfig()` in `cmd/codexssd/main.go` and `config.Config.StaleAfter() time.Duration`. Task 2 must be merged before this task starts.
- Produces:

```go
package visibility

type Entry struct {
	Name       string    `json:"name"`
	IsDir      bool      `json:"is_dir"`
	TotalBytes int64     `json:"total_bytes"`
	FileCount  int       `json:"file_count"`
	NewestMod  time.Time `json:"newest_mod"`
	Stale      bool      `json:"stale"`
	IsOurs     bool      `json:"is_ours"` // true for the codexssd-backups bin
	ReadError  string    `json:"read_error,omitempty"`
}

type Report struct {
	Dir        string  `json:"dir"`
	DirExists  bool    `json:"dir_exists"`
	Entries    []Entry `json:"entries"` // sorted by TotalBytes descending; [] never null
	TotalBytes int64   `json:"total_bytes"`
}

func Scan(dir string, now time.Time, staleAfter time.Duration) Report
```

- [ ] **Step 1: Write failing scan tests**

`internal/visibility/scan_test.go`:

```go
package visibility

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

const staleAfter = 30 * 24 * time.Hour

func write(t *testing.T, path string, size int, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestScanMissingDir(t *testing.T) {
	r := Scan(filepath.Join(t.TempDir(), "nope"), now, staleAfter)
	if r.DirExists {
		t.Error("DirExists should be false")
	}
	if r.Entries == nil {
		t.Error("Entries must be an empty slice, not nil (JSON [] not null)")
	}
}

func TestScanAggregatesAndFlagsStale(t *testing.T) {
	dir := t.TempDir()
	fresh := now.Add(-time.Hour)
	old := now.Add(-90 * 24 * time.Hour)
	write(t, filepath.Join(dir, "logs_2.sqlite"), 100, fresh)
	write(t, filepath.Join(dir, "sessions", "a", "one.jsonl"), 300, old)
	write(t, filepath.Join(dir, "sessions", "b", "two.jsonl"), 200, old)
	write(t, filepath.Join(dir, "codexssd-backups", "20260101-000000", "logs_2.sqlite"), 50, old)

	r := Scan(dir, now, staleAfter)
	if !r.DirExists {
		t.Fatal("DirExists should be true")
	}
	if r.TotalBytes != 650 {
		t.Errorf("TotalBytes = %d, want 650", r.TotalBytes)
	}
	// Sorted by size desc: sessions (500) first.
	if r.Entries[0].Name != "sessions" || r.Entries[0].TotalBytes != 500 || r.Entries[0].FileCount != 2 {
		t.Errorf("entries[0] = %+v", r.Entries[0])
	}
	if !r.Entries[0].Stale {
		t.Error("sessions should be stale (90 days old)")
	}
	byName := map[string]Entry{}
	for _, e := range r.Entries {
		byName[e.Name] = e
	}
	if byName["logs_2.sqlite"].Stale {
		t.Error("fresh file must not be stale")
	}
	if !byName["codexssd-backups"].IsOurs {
		t.Error("the recycling bin must be marked IsOurs")
	}
}

func TestScanJSONShape(t *testing.T) {
	r := Scan(filepath.Join(t.TempDir(), "nope"), now, staleAfter)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data)[0] != '{' {
		t.Fatal("unexpected JSON")
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/visibility/` → FAIL (no package)

- [ ] **Step 3: Implement `internal/visibility/scan.go`**

```go
// Package visibility produces the read-only "what's eating disk in ~/.codex"
// report — Phase 2's noticing-for-you promise.
//
// SAFETY: Scan is 100% read-only, and it only ever looks inside the directory
// it is given (in production, ~/.codex). It never reads user project trees.
// Finding something stale is only ever REPORTED — never acted on.
package visibility

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
)

type Entry struct {
	Name       string    `json:"name"`
	IsDir      bool      `json:"is_dir"`
	TotalBytes int64     `json:"total_bytes"`
	FileCount  int       `json:"file_count"`
	NewestMod  time.Time `json:"newest_mod"`
	Stale      bool      `json:"stale"`
	IsOurs     bool      `json:"is_ours"`
	ReadError  string    `json:"read_error,omitempty"`
}

type Report struct {
	Dir        string  `json:"dir"`
	DirExists  bool    `json:"dir_exists"`
	Entries    []Entry `json:"entries"`
	TotalBytes int64   `json:"total_bytes"`
}

// Scan walks dir (read-only) and aggregates disk usage by top-level entry.
// Subtree read errors degrade to Entry.ReadError — the report never fails
// outright. `now` and `staleAfter` are injected for testability.
func Scan(dir string, now time.Time, staleAfter time.Duration) Report {
	r := Report{Dir: dir, Entries: []Entry{}} // [] not null in JSON
	tops, err := os.ReadDir(dir)
	if err != nil {
		return r // missing/unreadable dir: DirExists stays false
	}
	r.DirExists = true

	for _, t := range tops {
		e := Entry{Name: t.Name(), IsDir: t.IsDir(), IsOurs: t.Name() == cleaner.BackupDirName}
		walkErr := filepath.WalkDir(filepath.Join(dir, t.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			e.TotalBytes += info.Size()
			e.FileCount++
			if info.ModTime().After(e.NewestMod) {
				e.NewestMod = info.ModTime()
			}
			return nil
		})
		if walkErr != nil {
			// Keep whatever we managed to count; report the problem in place.
			e.ReadError = walkErr.Error()
		}
		e.Stale = e.FileCount > 0 && now.Sub(e.NewestMod) >= staleAfter
		r.TotalBytes += e.TotalBytes
		r.Entries = append(r.Entries, e)
	}

	sort.Slice(r.Entries, func(i, j int) bool { return r.Entries[i].TotalBytes > r.Entries[j].TotalBytes })
	return r
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/visibility/` → PASS

- [ ] **Step 5: Write failing render test** — append to `cmd/codexssd/main_test.go` (follow its existing output-capture pattern):

```go
func TestRenderVisibilityReport(t *testing.T) {
	r := visibility.Report{
		Dir: "/home/x/.codex", DirExists: true, TotalBytes: 500,
		Entries: []visibility.Entry{
			{Name: "sessions", IsDir: true, TotalBytes: 500, FileCount: 2,
				NewestMod: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Stale: true},
		},
	}
	var buf bytes.Buffer
	renderVisibility(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "sessions") || !strings.Contains(out, "March 2026") {
		t.Errorf("output missing pieces:\n%s", out)
	}
	if !strings.Contains(out, "yours to decide") {
		t.Errorf("report must end with the report-only pointer:\n%s", out)
	}
}
```

- [ ] **Step 6: Run to verify failure** — `go test ./cmd/codexssd/ -run TestRenderVisibility` → FAIL

- [ ] **Step 7: Implement the `report` command** — in `cmd/codexssd/main.go`: add `report` to the `usage` string (`  report         Show what's using disk inside ~/.codex (read-only)`), add `case "report": return cmdReport(rest)`, and:

```go
// cmdReport implements `codexssd report`.
//
// SAFETY: 100% read-only, and scoped to ~/.codex ONLY. It reports and points;
// it never acts and never suggests CodexSSD act on anything beyond its own
// known log files.
func cmdReport(args []string) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd report [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Show what's using disk inside ~/.codex (read-only).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := codex.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return 1
	}
	cfg := loadConfig()
	rep := visibility.Scan(dir, time.Now(), cfg.StaleAfter())
	if *jsonOut {
		return emitJSON(rep)
	}
	renderVisibility(os.Stdout, rep)
	return 0
}

// renderVisibility prints the disk report in plain language.
func renderVisibility(w io.Writer, r visibility.Report) {
	if !r.DirExists {
		fmt.Fprintf(w, "No Codex directory found at %s — nothing is using disk here.\n", r.Dir)
		return
	}
	fmt.Fprintf(w, "Disk use inside %s (%s total):\n\n", r.Dir, codex.HumanBytes(r.TotalBytes))
	for _, e := range r.Entries {
		line := fmt.Sprintf("  %-24s %10s  (%d files)", e.Name, codex.HumanBytes(e.TotalBytes), e.FileCount)
		if e.Stale {
			line += fmt.Sprintf(" — untouched since %s", e.NewestMod.Format("January 2006"))
		}
		if e.IsOurs {
			line += "  [CodexSSD's own recycling bin]"
		}
		if e.ReadError != "" {
			line += "  (couldn't read everything here)"
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "CodexSSD only ever tidies its known Codex log files; the rest is")
	fmt.Fprintln(w, "yours to decide on. Nothing above has been touched.")
}
```

- [ ] **Step 8: Run to verify pass** — `go test ./cmd/codexssd/` → PASS. Manual smoke: `go run ./cmd/codexssd report` and `go run ./cmd/codexssd report --json`.

- [ ] **Step 9: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 10: Commit**

```bash
git add internal/visibility cmd/codexssd
git commit -m "feat(report): read-only disk-visibility report for ~/.codex with stale flags"
```

---

### Task 4: Recorder wiring — receipts for every mutation (Wave 1)

**Files:**
- Modify: `internal/recorder/jsonl.go` (extend `Receipt`; add `SummarizeFile`)
- Modify: `internal/recorder/jsonl_test.go`
- Modify: `internal/self/report.go` (+ history summary), `internal/self/report_test.go`
- Modify: `cmd/codexssd/main.go` (`cmdClean`/`cmdRestore`/`cmdPrune` append receipts; `cmdSelf` renders summary)
- Modify: `internal/tui/commands.go` (receipts on TUI clean/restore/auto-release)

**Interfaces:**
- Consumes: `recorder.Append(Receipt) error`, `recorder.Path()`, `self.Measure(stateDir)`.
- Produces:

```go
// Receipt gains (all existing fields keep their names; new/optional ones):
type Receipt struct {
	At           time.Time `json:"at"`
	Action       string    `json:"action"` // "clean" | "restore" | "prune" | "watch"
	DurationSec  float64   `json:"duration_sec,omitempty"`
	DiskWritten  int64     `json:"disk_written_bytes,omitempty"`
	PeakMBPerMin float64   `json:"peak_mb_per_min,omitempty"`
	FilesChanged int       `json:"files_changed,omitempty"`
	Risk         string    `json:"risk,omitempty"`
	BytesMoved   int64     `json:"bytes_moved,omitempty"`
	BackupID     string    `json:"backup_id,omitempty"`
	BackupIDs    []string  `json:"backup_ids,omitempty"`
}

type Summary struct {
	Records    int       `json:"records"`
	LastAction string    `json:"last_action,omitempty"`
	LastAt     time.Time `json:"last_at"`
}

func SummarizeFile(path string) (Summary, error) // missing file → zero Summary, nil error
```
  and `self.Report` gains `Records int` + `LastAction string` (populated by `Measure` via `recorder.SummarizeFile(filepath.Join(stateDir, recorder.FileName))`).

- [ ] **Step 1: Write failing recorder tests** — append to `internal/recorder/jsonl_test.go`:

```go
func TestSummarizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")

	s, err := SummarizeFile(path)
	if err != nil || s.Records != 0 {
		t.Fatalf("missing file: got %+v, %v; want zero summary, nil", s, err)
	}

	at := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	appendTo(path, Receipt{At: at, Action: "clean", BytesMoved: 100, BackupID: "20260704-120000"}, 10)
	appendTo(path, Receipt{At: at.Add(time.Hour), Action: "restore", BackupID: "20260704-120000"}, 10)

	s, err = SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 2 || s.LastAction != "restore" || !s.LastAt.Equal(at.Add(time.Hour)) {
		t.Errorf("got %+v", s)
	}
}

func TestSummarizeFileSkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	os.WriteFile(path, []byte("{not json}\n{\"at\":\"2026-07-04T12:00:00Z\",\"action\":\"clean\"}\n"), 0o600)
	s, err := SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 1 || s.LastAction != "clean" {
		t.Errorf("got %+v", s)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/recorder/` → FAIL

- [ ] **Step 3: Implement** — update the `Receipt` struct exactly as in Interfaces above (add `Action`, `BytesMoved`, `BackupID`, `BackupIDs`; add `omitempty` to the optional fields), then add:

```go
// Summary condenses the session history for the `self` report.
type Summary struct {
	Records    int       `json:"records"`
	LastAction string    `json:"last_action,omitempty"`
	LastAt     time.Time `json:"last_at"`
}

// SummarizeFile reads the JSONL history at path. A missing file is an empty
// (not error) summary. Corrupt lines are skipped — history is bookkeeping,
// and a damaged line must not take down the report.
func SummarizeFile(path string) (Summary, error) {
	lines, err := readLines(path)
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	for _, l := range lines {
		var r Receipt
		if json.Unmarshal([]byte(l), &r) != nil {
			continue
		}
		s.Records++
		if !r.At.Before(s.LastAt) {
			s.LastAt = r.At
			s.LastAction = r.Action
		}
	}
	return s, nil
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/recorder/` → PASS

- [ ] **Step 5: Extend `self`** — failing test first, appended to `internal/self/report_test.go`:

```go
func TestMeasureIncludesHistorySummary(t *testing.T) {
	dir := t.TempDir()
	line := `{"at":"2026-07-04T12:00:00Z","action":"clean","bytes_moved":42}` + "\n"
	os.WriteFile(filepath.Join(dir, "sessions.jsonl"), []byte(line), 0o600)
	r, err := Measure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Records != 1 || r.LastAction != "clean" {
		t.Errorf("got %+v", r)
	}
}
```
Run (FAIL), then in `internal/self/report.go`: add `Records int \`json:"records"\`` and `LastAction string \`json:"last_action,omitempty"\`` to `Report`, and in `Measure` after `r.HistoryBytes = size`:

```go
	sum, err := recorder.SummarizeFile(filepath.Join(stateDir, recorder.FileName))
	if err != nil {
		return r, err
	}
	r.Records = sum.Records
	r.LastAction = sum.LastAction
```
(import `"github.com/0xdefence/codexssd/internal/recorder"` and `"path/filepath"`). Run → PASS.

- [ ] **Step 6: Wire receipts into the CLI** — in `cmd/codexssd/main.go` add one seam + helper:

```go
// appendReceipt records a session receipt. A failed receipt is a note, never
// an error — bookkeeping must not fail the user's action.
var appendReceipt = recorder.Append

func recordReceipt(r recorder.Receipt) {
	if err := appendReceipt(r); err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: note: couldn't record session receipt: %v\n", err)
	}
}
```
Call it at each mutation success point:
- `cmdClean`, right after the success `fmt.Printf` for `--yes`:
  `recordReceipt(recorder.Receipt{At: time.Now(), Action: "clean", BytesMoved: plan.TotalBytes, FilesChanged: len(plan.Items), BackupID: filepath.Base(dest)})`
- `cmdRestore`, after a successful `cleaner.Restore`:
  `recordReceipt(recorder.Receipt{At: time.Now(), Action: "restore", BackupID: id})`
- `cmdPrune`, after a successful `ReleaseExpired` **when `len(released) > 0`**:
  `recordReceipt(recorder.Receipt{At: time.Now(), Action: "prune", BackupIDs: released})`
- `cmdSelf` human output — after the storage line add:

```go
	if rep.Records > 0 {
		fmt.Printf("  history:  %d recorded action(s), last: %s\n", rep.Records, rep.LastAction)
	} else {
		fmt.Println("  history:  no recorded actions yet")
	}
```

- [ ] **Step 7: Wire receipts into the TUI** — in `internal/tui/commands.go`, add seam `appendReceipt = recorder.Append` to the var block. In `cleanCmd` after a successful `applyPlan`, in `restoreCmd` after a successful `restoreBackup`, and in `releaseCmd` when `len(released) > 0`, append the same receipts as Step 6 but **ignore the error entirely** (the TUI has no stderr channel worth interrupting for). Add an `update_test.go` case asserting the seam was called after a successful clean (stub `appendReceipt` to capture the receipt, assert `Action == "clean"`).

- [ ] **Step 8: Integration check** — extend `cmd/codexssd/main_test.go` or `integration_test.go` (follow existing pattern with stubbed `isCodexRunning`): run a `clean --yes` against a temp codex dir, then assert `~/.codexssd/sessions.jsonl`… **stop** — tests must not touch the real home dir. Instead: stub `appendReceipt` in the test to capture, assert one receipt with `Action == "clean"` and correct `BackupID`.

- [ ] **Step 9: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 10: Commit**

```bash
git add internal/recorder internal/self internal/tui cmd/codexssd
git commit -m "feat(recorder): write action receipts on clean/restore/prune; real self history"
```

---

### Task 5: `codexssd watch` — foreground monitor + notifications (Wave 2)

**Files:**
- Create: `internal/notify/notify.go`
- Create: `internal/notify/notify_test.go`
- Create: `cmd/codexssd/watch.go`
- Create: `cmd/codexssd/watch_test.go`
- Modify: `cmd/codexssd/main.go` (replace `cmdNotImplemented("watch")` with `cmdWatch(rest)`; delete `cmdNotImplemented` if now unused)

**Interfaces:**
- Consumes: `codex.ScanLogs`, `codex.ProcessMemory` (Task 1), `codex.IsCodexRunning`, `monitor.AppendSample/Evaluate` (Task 1 fields), `config.LoadDefault` + duration helpers (Task 2), `recorder.Receipt` with `Action`/`DurationSec`/`PeakMBPerMin`/`Risk` (Task 4), `recordReceipt` (Task 4).
- Produces: `notify.Notify(title, body string) error`, `notify.ErrUnsupported`; `runWatch(w io.Writer, jsonOut bool, th monitor.Thresholds, deps watchDeps) recorder.Receipt` (pure-ish loop, fully injectable).

- [ ] **Step 1: Write failing notify test**

`internal/notify/notify_test.go`:

```go
package notify

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestNotifyInvokesPlatformTool(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("notification platforms only")
	}
	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return exec.Command("true") // run something harmless
	}
	t.Cleanup(func() { execCommand = exec.Command })

	if err := Notify("CodexSSD", "WAL is 2 GiB"); err != nil {
		t.Fatal(err)
	}
	switch runtime.GOOS {
	case "darwin":
		if gotName != "osascript" || len(gotArgs) != 2 || gotArgs[0] != "-e" {
			t.Errorf("got %s %v", gotName, gotArgs)
		}
	case "linux":
		if gotName != "notify-send" {
			t.Errorf("got %s %v", gotName, gotArgs)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/notify/` → FAIL

- [ ] **Step 3: Implement `internal/notify/notify.go`**

```go
// Package notify fires a native desktop notification. It is strictly
// fire-and-forget: callers treat any failure as ignorable, because the
// terminal output is always the source of truth.
package notify

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// ErrUnsupported means this platform has no notification pathway.
var ErrUnsupported = errors.New("desktop notifications not supported on this platform")

// execCommand is a seam so tests never spawn real notification UIs.
var execCommand = exec.Command

// Notify shows a desktop notification with the given title and body.
func Notify(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		return execCommand("osascript", "-e", script).Run()
	case "linux":
		return execCommand("notify-send", title, body).Run()
	default:
		return ErrUnsupported
	}
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/notify/` → PASS

- [ ] **Step 5: Write failing watch-loop tests**

`cmd/codexssd/watch_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
)

// scriptedDeps feeds a fixed sequence of readings, then closes stop.
// scan and now use SEPARATE counters: runWatch calls now() for the session
// start/end too, so sharing one index would misalign (and overrun) totals.
func scriptedDeps(t *testing.T, totals []int64) (watchDeps, *[]string) {
	t.Helper()
	tick := make(chan time.Time)
	stop := make(chan struct{})
	scanIdx, timeIdx := 0, 0
	var notified []string
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	deps := watchDeps{
		scan: func() codex.LogReport {
			r := codex.LogReport{DirExists: true, TotalBytes: totals[scanIdx]}
			if scanIdx < len(totals)-1 {
				scanIdx++
			}
			return r
		},
		memory:  func() (int64, error) { return 0, nil },
		running: func() (bool, error) { return true, nil },
		notify: func(title, body string) error {
			notified = append(notified, title+": "+body)
			return nil
		},
		// Each now() call advances one minute, so consecutive observations sit
		// exactly one minute apart and MB-per-minute math is trivial to reason about.
		now: func() time.Time {
			ts := base.Add(time.Duration(timeIdx) * time.Minute)
			timeIdx++
			return ts
		},
		tick: tick,
		stop: stop,
	}
	go func() {
		for range totals[1:] {
			tick <- time.Time{}
		}
		close(stop)
	}()
	return deps, &notified
}

func TestRunWatchPrintsOnLevelChangeOnly(t *testing.T) {
	// 0 → +200MB/min for two ticks: LOW baseline, then HIGH, then stays HIGH.
	deps, notified := scriptedDeps(t, []int64{0, 200 << 20, 400 << 20})
	var buf bytes.Buffer
	rec := runWatch(&buf, false, monitor.DefaultThresholds(), deps)

	out := buf.String()
	// Count the event-line form "risk HIGH" — the session summary says
	// "Peak risk: HIGH", which deliberately doesn't match this substring.
	if strings.Count(out, "risk HIGH") != 1 {
		t.Errorf("'risk HIGH' should be printed exactly once (level change), got:\n%s", out)
	}
	if len(*notified) != 1 {
		t.Errorf("want exactly 1 notification on escalation to HIGH, got %v", *notified)
	}
	if rec.Action != "watch" || rec.Risk != "HIGH" {
		t.Errorf("receipt = %+v", rec)
	}
}

func TestRunWatchNoNotifyBelowHigh(t *testing.T) {
	deps, notified := scriptedDeps(t, []int64{0, 30 << 20}) // ~30MB/min → MEDIUM
	var buf bytes.Buffer
	runWatch(&buf, false, monitor.DefaultThresholds(), deps)
	if len(*notified) != 0 {
		t.Errorf("MEDIUM must not notify, got %v", *notified)
	}
}
```

- [ ] **Step 6: Run to verify failure** — `go test ./cmd/codexssd/ -run TestRunWatch` → FAIL

- [ ] **Step 7: Implement `cmd/codexssd/watch.go`**

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/notify"
	"github.com/0xdefence/codexssd/internal/recorder"
)

// watchDeps injects every effect the watch loop has, so tests can script a
// whole session deterministically.
type watchDeps struct {
	scan    func() codex.LogReport
	memory  func() (int64, error)
	running func() (bool, error)
	notify  func(title, body string) error
	now     func() time.Time
	tick    <-chan time.Time
	stop    <-chan struct{}
}

// cmdWatch implements `codexssd watch`: a foreground, read-only monitor.
//
// SAFETY: 100% read-only on Codex's files. Its only writes are one session
// receipt to CodexSSD's own state dir on exit, and (optional) desktop
// notifications — both best-effort.
func cmdWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Duration("interval", 0, "poll interval (default: from config, 30s)")
	noNotify := fs.Bool("no-notify", false, "disable desktop notifications")
	jsonOut := fs.Bool("json", false, "emit one JSON line per risk-level change")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd watch [--interval 30s] [--no-notify] [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Watch Codex's logs and memory in the foreground; Ctrl-C to stop.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dir, err := codex.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return 1
	}
	cfg := loadConfig()
	if *interval == 0 {
		*interval = cfg.PollInterval()
	}

	notifier := notify.Notify
	if *noNotify || !cfg.Notifications {
		notifier = func(string, string) error { return nil }
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	stop := make(chan struct{})
	go func() { <-sigCh; close(stop) }()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	deps := watchDeps{
		scan:    func() codex.LogReport { return codex.ScanLogs(dir) },
		memory:  func() (int64, error) { return codex.ProcessMemory() },
		running: codex.IsCodexRunning,
		notify:  notifier,
		now:     time.Now,
		tick:    ticker.C,
		stop:    stop,
	}
	rec := runWatch(os.Stdout, *jsonOut, cfg.MonitorThresholds(), deps)
	recordReceipt(rec)
	return 0
}

// watchEvent is the JSON line emitted per risk-level change with --json.
type watchEvent struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Reasons []string  `json:"reasons"`
	TotalMB int64     `json:"total_log_mb"`
}

// runWatch is the loop, fully injected for tests. It samples once immediately
// (the baseline), then once per tick, printing only when the risk LEVEL
// changes — never per tick, so a quiet session stays quiet.
func runWatch(w io.Writer, jsonOut bool, th monitor.Thresholds, deps watchDeps) recorder.Receipt {
	var samples []monitor.Sample
	var last monitor.Risk = -1 // sentinel: baseline always prints
	peak := monitor.RiskLow
	var peakRate float64
	start := deps.now()
	var firstTotal, lastTotal int64

	observe := func() {
		report := deps.scan()
		mem, _ := deps.memory() // best-effort; 0 when unknown
		running, _ := deps.running()
		now := deps.now()
		if len(samples) == 0 {
			firstTotal = report.TotalBytes
		}
		lastTotal = report.TotalBytes
		var wal int64
		for _, f := range report.Files {
			if f.Name == "logs_2.sqlite-wal" && f.Exists {
				wal = f.Size
			}
		}
		samples = monitor.AppendSample(samples, monitor.Sample{
			At: now, TotalBytes: report.TotalBytes, WALBytes: wal, MemBytes: mem,
		}, 20)
		a := monitor.Evaluate(samples, running, th)
		if a.RateMBPerMin > peakRate {
			peakRate = a.RateMBPerMin
		}
		if a.Level > peak {
			peak = a.Level
		}
		if a.Level == last {
			return
		}
		escalatedToAlarming := a.Level >= monitor.RiskHigh && a.Level > last
		last = a.Level
		if jsonOut {
			reasons := a.Reasons
			if reasons == nil {
				reasons = []string{} // [] not null
			}
			line, _ := json.Marshal(watchEvent{At: now, Level: a.Level.String(), Reasons: reasons, TotalMB: report.TotalBytes / (1024 * 1024)})
			fmt.Fprintln(w, string(line))
		} else {
			msg := fmt.Sprintf("[%s] risk %s", now.Format("15:04:05"), a.Level)
			for _, r := range a.Reasons {
				msg += " — " + r
			}
			fmt.Fprintln(w, msg)
		}
		if escalatedToAlarming {
			body := "Codex disk/memory activity looks alarming."
			if len(a.Reasons) > 0 {
				body = a.Reasons[0]
			}
			_ = deps.notify("CodexSSD: "+a.Level.String(), body) // fire-and-forget
		}
	}

	observe() // baseline
	for {
		select {
		case <-deps.tick:
			observe()
		case <-deps.stop:
			end := deps.now()
			growth := lastTotal - firstTotal
			if growth < 0 {
				growth = 0
			}
			if !jsonOut {
				fmt.Fprintf(w, "\nWatched for %s. Peak risk: %s. Log growth observed: %s.\n",
					end.Sub(start).Round(time.Second), peak, codex.HumanBytes(growth))
			}
			return recorder.Receipt{
				At: end, Action: "watch",
				DurationSec:  end.Sub(start).Seconds(),
				DiskWritten:  growth,
				PeakMBPerMin: peakRate,
				Risk:         peak.String(),
			}
		}
	}
}
```
In `main.go`, change `case "watch": return cmdNotImplemented("watch")` to `case "watch": return cmdWatch(rest)`. If `cmdNotImplemented` has no remaining callers, delete it (and its test if one exists).

- [ ] **Step 8: Run to verify pass** — `go test ./cmd/codexssd/` → PASS. Note the scripted `now` advances one minute per observation, so 200 MiB growth in 1 minute evaluates to HIGH (≥100 MB/min) but below CRITICAL (500).

- [ ] **Step 9: Manual smoke** — `go run ./cmd/codexssd watch --interval 5s` in one terminal; confirm a baseline line appears, quiet after; Ctrl-C prints the session summary. On macOS also confirm a notification fires if you can provoke HIGH (or temporarily lower thresholds in `~/.codexssd/config.json` — e.g. `{"high_wal_size_mb": 1}` — and delete after).

- [ ] **Step 10: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 11: Commit**

```bash
git add internal/notify cmd/codexssd
git commit -m "feat(watch): foreground monitor with level-change lines + desktop notifications"
```

---

### Task 6: Read-only MCP server (`codexssd mcp`) (Wave 2)

**Files:**
- Create: `internal/mcpserver/server.go`
- Create: `internal/mcpserver/tools.go`
- Create: `internal/mcpserver/server_test.go`
- Modify: `cmd/codexssd/main.go` (new `mcp` command + usage line)

**Interfaces:**
- Consumes: `codex.Dir/ScanLogs/IsCodexRunning/ErrUnsupportedPlatform`, `cleaner.PlanCodexLogs/ListBackups`, `self.Measure`, `recorder.Dir`, `visibility.Scan` (Task 3), `config.LoadDefault` (Task 2).
- Produces: `mcpserver.New() *Server`, `(*Server) Serve(r io.Reader, w io.Writer) error`. Protocol: newline-delimited JSON-RPC 2.0 over stdio; protocol version `"2025-06-18"`; capabilities `{"tools":{}}`; five read-only tools: `codex_status`, `clean_plan`, `list_backups`, `self_report`, `disk_report`.

- [ ] **Step 1: Write failing protocol tests**

`internal/mcpserver/server_test.go`:

```go
package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/visibility"
)

// serve runs the given newline-delimited requests through a stubbed server and
// returns one decoded JSON object per response line.
func serve(t *testing.T, requests ...string) []map[string]any {
	t.Helper()
	s := New()
	// Stub every data source: protocol tests must not read the real ~/.codex.
	s.status = func() (codex.LogReport, error) {
		return codex.LogReport{CodexDir: "/tmp/x/.codex", DirExists: true, Files: []codex.LogFile{}, TotalBytes: 42}, nil
	}
	s.cleanPlan = func() (any, error) { return map[string]any{"total_bytes": 42}, nil }
	s.backups = func() (any, error) { return []any{}, nil }
	s.selfReport = func() (any, error) { return map[string]any{"mode": "low-write"}, nil }
	s.diskReport = func() (visibility.Report, error) {
		return visibility.Scan("/nonexistent-for-test", time.Now(), time.Hour), nil
	}

	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	if err := s.Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestInitializeHandshake(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
	)
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d", len(resps))
	}
	result := resps[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Error("must advertise tools capability")
	}
}

func TestToolsListAndCall(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"codex_status","arguments":{}}}`,
	)
	if len(resps) != 3 { // the notification gets NO response
		t.Fatalf("want 3 responses, got %d", len(resps))
	}
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("want 5 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"codex_status", "clean_plan", "list_backups", "self_report", "disk_report"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	callResult := resps[2]["result"].(map[string]any)
	content := callResult["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" || !strings.Contains(content["text"].(string), "42") {
		t.Errorf("bad tool result: %v", content)
	}
}

func TestErrors(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"no/such/method"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_everything"}}`,
		`this is not json`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)
	if len(resps) != 4 {
		t.Fatalf("want 4 responses (incl. parse error), got %d", len(resps))
	}
	if code := resps[0]["error"].(map[string]any)["code"].(float64); code != -32601 {
		t.Errorf("unknown method code = %v, want -32601", code)
	}
	if code := resps[1]["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("unknown tool code = %v, want -32602", code)
	}
	if code := resps[2]["error"].(map[string]any)["code"].(float64); code != -32700 {
		t.Errorf("parse error code = %v, want -32700", code)
	}
	if _, ok := resps[3]["result"]; !ok {
		t.Error("ping must return an empty result")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/mcpserver/` → FAIL (no package)

- [ ] **Step 3: Implement `internal/mcpserver/server.go`**

```go
// Package mcpserver is CodexSSD's read-only MCP (Model Context Protocol)
// server, spoken over stdio as newline-delimited JSON-RPC 2.0.
//
// SAFETY (hard product line): every tool is READ-ONLY. An AI agent connected
// to this server can see everything CodexSSD sees and touch NOTHING. There
// are no mutating tools, and none may ever be added — cleaning stays a human
// action. Implemented with the standard library only.
package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion is the MCP revision this server implements. Pinned
// deliberately: a read-only server would rather be honestly versioned than
// silently wrong.
const protocolVersion = "2025-06-18"

const serverName = "codexssd"
const serverVersion = "0.1.0"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads newline-delimited JSON-RPC requests from r and writes responses
// to w until EOF. Notifications (no id) get no response, per JSON-RPC 2.0.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // generous line cap
	enc := json.NewEncoder(w)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Parse errors respond with a null id, per spec.
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp, respond := s.handle(req)
		if respond {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// handle dispatches one request. respond=false for notifications.
func (s *Server) handle(req rpcRequest) (rpcResponse, bool) {
	isNotification := len(req.ID) == 0
	ok := func(result any) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, !isNotification
	}
	fail := func(code int, msg string) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}, !isNotification
	}

	switch req.Method {
	case "initialize":
		return ok(map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
			"instructions": "CodexSSD watches OpenAI Codex's disk and memory footprint. " +
				"All tools are read-only: you can inspect, never modify. " +
				"Cleaning is a human-only action via the codexssd CLI.",
		})
	case "notifications/initialized":
		return rpcResponse{}, false
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": toolDescriptors()})
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(-32602, "invalid params")
		}
		text, err := s.callTool(params.Name)
		if err != nil {
			return fail(-32602, err.Error())
		}
		return ok(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": false,
		})
	default:
		return fail(-32601, fmt.Sprintf("method %q not found", req.Method))
	}
}
```

- [ ] **Step 4: Implement `internal/mcpserver/tools.go`**

```go
package mcpserver

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/visibility"
)

// Server holds the tool data sources as fields so tests can stub them.
// Every source is READ-ONLY by construction.
type Server struct {
	status     func() (codex.LogReport, error)
	cleanPlan  func() (any, error)
	backups    func() (any, error)
	selfReport func() (any, error)
	diskReport func() (visibility.Report, error)
}

// New returns a Server wired to the real read-only engine functions.
func New() *Server {
	return &Server{
		status: func() (codex.LogReport, error) {
			dir, err := codex.Dir()
			if err != nil {
				return codex.LogReport{}, err
			}
			return codex.ScanLogs(dir), nil
		},
		cleanPlan: func() (any, error) {
			dir, err := codex.Dir()
			if err != nil {
				return nil, err
			}
			plan, err := cleaner.PlanCodexLogs(dir)
			if err != nil {
				return nil, err
			}
			running, runErr := codex.IsCodexRunning()
			out := map[string]any{
				"plan":          plan,
				"codex_running": running,
				"note":          "dry run only — this server cannot move files; cleaning is human-only",
			}
			if runErr != nil && runErr != codex.ErrUnsupportedPlatform {
				out["check_error"] = runErr.Error()
			}
			return out, nil
		},
		backups: func() (any, error) {
			dir, err := codex.Dir()
			if err != nil {
				return nil, err
			}
			backups, err := cleaner.ListBackups(dir)
			if err != nil {
				return nil, err
			}
			if backups == nil {
				backups = []cleaner.Backup{} // [] not null
			}
			return backups, nil
		},
		selfReport: func() (any, error) {
			dir, err := recorder.Dir()
			if err != nil {
				return nil, err
			}
			rep, err := self.Measure(dir)
			if err != nil {
				return nil, err
			}
			return rep, nil
		},
		diskReport: func() (visibility.Report, error) {
			dir, err := codex.Dir()
			if err != nil {
				return visibility.Report{}, err
			}
			cfg, _ := config.LoadDefault() // malformed config → defaults; never fails
			return visibility.Scan(dir, time.Now(), cfg.StaleAfter()), nil
		},
	}
}

// toolDescriptors lists the five read-only tools. Every inputSchema is an
// empty object: no tool takes arguments, which keeps the surface unabusable.
func toolDescriptors() []map[string]any {
	emptySchema := map[string]any{"type": "object", "properties": map[string]any{}}
	mk := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": emptySchema}
	}
	return []map[string]any{
		mk("codex_status", "Sizes of Codex's own log files under ~/.codex (read-only)."),
		mk("clean_plan", "Dry-run plan of what `codexssd clean` WOULD move aside. This server cannot execute it."),
		mk("list_backups", "Recoverable recycling-bin backups with hold information (read-only)."),
		mk("self_report", "CodexSSD's own footprint and action history (read-only)."),
		mk("disk_report", "What's using disk inside ~/.codex, with stale flags (read-only)."),
	}
}

// callTool runs one named tool and returns its result as pretty JSON text.
func (s *Server) callTool(name string) (string, error) {
	var v any
	var err error
	switch name {
	case "codex_status":
		v, err = s.status()
	case "clean_plan":
		v, err = s.cleanPlan()
	case "list_backups":
		v, err = s.backups()
	case "self_report":
		v, err = s.selfReport()
	case "disk_report":
		v, err = s.diskReport()
	default:
		return "", fmt.Errorf("unknown tool %q (this server is read-only; there are exactly five tools)", name)
	}
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
```

- [ ] **Step 5: Run to verify pass** — `go test ./internal/mcpserver/` → PASS

- [ ] **Step 6: Wire the command** — in `cmd/codexssd/main.go`: add to usage `  mcp            Serve read-only CodexSSD tools to AI agents over stdio (MCP)`, add:

```go
	case "mcp":
		if err := mcpserver.New().Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: mcp server error: %v\n", err)
			return 1
		}
		return 0
```
(Import `internal/mcpserver`. Anything diagnostic goes to stderr only — stdout is the protocol channel.)

- [ ] **Step 7: Manual smoke**

```bash
go build -o /tmp/codexssd ./cmd/codexssd
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | /tmp/codexssd mcp
```
Expected: two JSON lines — an initialize result with `"protocolVersion":"2025-06-18"`, then a tools list with 5 tools. Optionally: `claude mcp add codexssd -- /tmp/codexssd mcp` and call a tool from a Claude Code session.

- [ ] **Step 8: Full gate** — `go build ./... && go vet ./... && go test ./... && gofmt -l .` → green/empty.

- [ ] **Step 9: Commit**

```bash
git add internal/mcpserver cmd/codexssd
git commit -m "feat(mcp): read-only stdio MCP server with five inspection tools"
```

---

### Task 7: Docs sync (Wave 3 — after everything merges)

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/scope.md`
- Modify: `docs/roadmap.md`

**Interfaces:** none (docs only; no behavior changes permitted in this task).

- [ ] **Step 1: Verify ground truth first** — run `go run ./cmd/codexssd help` and skim `cmd/codexssd/main.go`'s dispatch. The docs below must match what actually ships; if a command listed here is missing, STOP and report rather than documenting it.

- [ ] **Step 2: Update `README.md`**
  - "Usage (today)" gains sections for `report`, `watch`, `prune`, `install-agent`, `self`, `mcp`, and the bare-`codexssd` dashboard, each with a short plain-language example in the existing style (mirror the `status` section's tone).
  - Add a **Configuration** section documenting `~/.codexssd/config.json` with a full example JSON showing every key and its default, and the sentence: "A missing or broken config never stops the tool — it warns and uses defaults."
  - Add an **MCP** section: what it is, that it is read-only by design ("an agent can see everything and touch nothing"), and the setup line `claude mcp add codexssd -- codexssd mcp`.
  - "Phase 1 scope" section: retitle to "Roadmap status", mark Phase 1 complete, Phase 2 complete except deferred items, link the spec.
  - Repository layout: remove every "(stub)" annotation that no longer applies; add `internal/config/`, `internal/visibility/`, `internal/notify/`, `internal/mcpserver/`, `internal/tui/`, `internal/trash/` lines.

- [ ] **Step 3: Update `CLAUDE.md`**
  - "Current state": list all implemented commands (`status`, `watch`, `clean`, `restore`, `prune`, `report`, `install-agent`, `self`, `mcp`, bare-command TUI); remove the stubs paragraph.
  - Safety rules: append two lines — notifications are fire-and-forget and never block; `internal/mcpserver` is read-only by definition and may never gain a mutating tool.
  - Layout table: same corrections as the README.
  - Conventions: note the config-can-never-brick-the-tool contract.

- [ ] **Step 4: Update `docs/scope.md` and `docs/roadmap.md`** — in scope.md, annotate each "In scope" bullet with **[shipped]** where true (all should be, after this sprint, except "memory" warnings only cover Codex RSS — say exactly that). In roadmap.md, add a one-line `> Status (2026-07): shipped` note under Phase 1 and Phase 2 headings, and under Phase 2 note the deliberate narrowing: visibility covers `~/.codex` only, per the spec.

- [ ] **Step 5: Cross-check** — every command named in README/CLAUDE.md exists in `codexssd help` output and vice versa; every config key in the README example exists in `internal/config/config.go`'s struct tags. Fix any mismatch found.

- [ ] **Step 6: Gate & commit**

```bash
go build ./... && go vet ./... && go test ./... && gofmt -l .
git add README.md CLAUDE.md docs/scope.md docs/roadmap.md
git commit -m "docs: sync README, CLAUDE.md, scope, roadmap to shipped Phase 1-2 + MCP state"
```
