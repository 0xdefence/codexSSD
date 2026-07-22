# Claude Code Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Claude Code the same end-to-end treatment as Codex — `watch --tool claude`, tool-aware MCP, `install-agent --tool claude` → CLAUDE.md, and a tab-bar dashboard — per the approved spec at `docs/superpowers/specs/2026-07-21-claude-code-parity-design.md`.

**Architecture:** Everything routes through the existing `internal/tool` profile system and the already-generic `internal/cleaner` engine. Two small pure additions to `internal/tool` (`ScanDirSize`, `ProcessMemory`) feed watch and the TUI. The TUI collapses its four `stateClaude*` states into the generic states keyed by a new `activeTab` field.

**Tech Stack:** Go stdlib only in engine packages; charmbracelet (Bubble Tea, Lip Gloss) confined to `internal/tui`.

## Global Constraints

- Engine packages (`internal/*` except `tui`) use ONLY the standard library. `go.mod` must not change.
- The tool may only MOVE files aside, never delete. No task here adds any file-mutating logic — all mutation stays in `internal/cleaner` behind profile allow-lists.
- Codex default behavior is pinned: `watch` with no `--tool` and every MCP tool with no `tool` argument must produce byte-for-byte identical human output. (One allowed additive change: `watch --json` lines gain `"tool":"codex"`.)
- The MCP server keeps exactly five tools, all read-only, with their existing names.
- Receipts follow the documented `recorder.Receipt.Action` convention — base command plus optional `" --tool <name>"` (e.g. `"watch --tool claude"`). **Note: this supersedes the spec's "new `tool` field" sentence** — the codebase already has this convention (see `internal/recorder/jsonl.go:32`); follow it.
- Config keys are shared between tools (no per-tool thresholds — out of scope).
- Friendly plain-language output; comment the *why*; `gofmt` clean.
- After every task: `go build ./... && go vet ./... && go test ./...` must pass before committing.

---

### Task 1: `tool.ScanDirSize` — whole-directory footprint

**Files:**
- Modify: `internal/tool/scan.go` (append)
- Test: `internal/tool/scan_test.go` (append)

**Interfaces:**
- Consumes: `tool.BackupDirName` (existing const, `"codexssd-backups"`)
- Produces: `func ScanDirSize(dir string) int64` — total bytes of regular files under dir, excluding the recycling bin; 0 for a missing dir. Tasks 3 and 6 call it.

- [ ] **Step 1: Write the failing test** (append to `internal/tool/scan_test.go`)

```go
func TestScanDirSize(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(rel string, size int) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("projects/a/sess.jsonl", 100)
	writeFile("shell-snapshots/snap1", 40)
	writeFile("settings.json", 10)
	// The recycling bin must NOT count: our own tidies are not agent writes.
	writeFile(BackupDirName+"/20260101-000000/big.jsonl", 5000)

	if got := ScanDirSize(dir); got != 150 {
		t.Fatalf("ScanDirSize = %d, want 150 (backups excluded)", got)
	}
	if got := ScanDirSize(filepath.Join(dir, "no-such-dir")); got != 0 {
		t.Fatalf("ScanDirSize(missing) = %d, want 0", got)
	}
}
```

Add `"bytes"` to the test file's imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestScanDirSize -v`
Expected: FAIL — `undefined: ScanDirSize`

- [ ] **Step 3: Write minimal implementation** (append to `internal/tool/scan.go`; add `"io/fs"` to imports)

```go
// ScanDirSize returns the total size in bytes of all regular files under dir,
// excluding the codexssd-backups recycling bin — so the tool's own tidies are
// never mistaken for agent write activity. A missing or unreadable dir (or any
// unreadable entry) contributes 0 rather than an error: this feeds a live
// monitor, which must degrade gracefully, never fail.
//
// SAFETY: read-only (WalkDir/Stat only).
func ScanDirSize(dir string) int64 {
	var total int64
	root := filepath.Clean(dir)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries; never fatal
		}
		if d.IsDir() {
			if d.Name() == BackupDirName && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -run TestScanDirSize -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tool/scan.go internal/tool/scan_test.go
git commit -m "feat(tool): ScanDirSize measures a profile dir's footprint, excluding the bin"
```

---

### Task 2: `tool.ProcessMemory` — generic per-profile RSS

**Files:**
- Create: `internal/tool/memory.go`
- Test: `internal/tool/memory_test.go`

**Interfaces:**
- Consumes: `tool.DetectProcesses(p Profile)`, `tool.ErrUnsupportedPlatform` (existing)
- Produces: `func ProcessMemory(p Profile) (int64, error)` — total RSS bytes of processes matching the profile; `(0, nil)` when none running. Tasks 3 and 6 call it.

Mirrors `internal/codex/memory.go`, which stays untouched (regression safety; `codex` imports `tool`, never the reverse, so the small duplication is deliberate — note it in a comment).

- [ ] **Step 1: Write the failing test** (`internal/tool/memory_test.go`)

```go
package tool

import "testing"

func TestParseRSSKiB(t *testing.T) {
	// ps -o rss= output: one KiB value per line; junk lines are skipped.
	got := parseRSSKiB(" 1024\n2048\n\nnot-a-number\n")
	if got != 3072 {
		t.Fatalf("parseRSSKiB = %d, want 3072", got)
	}
}

func TestProcessMemoryNoProcesses(t *testing.T) {
	// A profile no real process matches: absence is (0, nil), not an error.
	p := Profile{Name: "definitely-not-running-xyz", ProcessNames: []string{"definitely-not-running-xyz"}}
	mem, err := ProcessMemory(p)
	if err != nil {
		t.Fatalf("ProcessMemory error: %v", err)
	}
	if mem != 0 {
		t.Fatalf("ProcessMemory = %d, want 0", mem)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run 'TestParseRSSKiB|TestProcessMemoryNoProcesses' -v`
Expected: FAIL — `undefined: parseRSSKiB` / `undefined: ProcessMemory`

- [ ] **Step 3: Write minimal implementation** (`internal/tool/memory.go`)

```go
package tool

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
// processes matching the profile. When none are running it returns (0, nil) —
// absence is not an error. Windows returns ErrUnsupportedPlatform; callers
// must omit memory rather than fail.
//
// Mirrors internal/codex.ProcessMemory, which is left untouched on purpose:
// codex imports tool (never the reverse), and the founding Codex path must
// stay byte-for-byte stable.
//
// SAFETY: observation only — it never signals or alters a process.
func ProcessMemory(p Profile) (int64, error) {
	if runtime.GOOS == "windows" {
		return 0, ErrUnsupportedPlatform
	}
	procs, err := DetectProcesses(p)
	if err != nil {
		return 0, err
	}
	if len(procs) == 0 {
		return 0, nil
	}
	pids := make([]string, 0, len(procs))
	for _, proc := range procs {
		pids = append(pids, strconv.Itoa(proc.PID))
	}
	out, err := execRSS(pids)
	if err != nil {
		return 0, err
	}
	// ps reports RSS in KiB on both darwin and linux.
	return parseRSSKiB(string(out)) * 1024, nil
}

// parseRSSKiB sums the per-line RSS values (KiB) in `ps -o rss=` output.
// Non-numeric lines are skipped rather than treated as errors.
func parseRSSKiB(psOut string) int64 {
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -v`
Expected: PASS (all, including existing tests)

- [ ] **Step 5: Commit**

```bash
git add internal/tool/memory.go internal/tool/memory_test.go
git commit -m "feat(tool): per-profile ProcessMemory for the watch/TUI monitors"
```

---

### Task 3: `watch --tool claude`

**Files:**
- Modify: `cmd/codexssd/watch.go`
- Modify: `cmd/codexssd/main.go` (usage const: watch line)
- Test: `cmd/codexssd/watch_test.go`

**Interfaces:**
- Consumes: `tool.ByName`, `Profile.Dir()`, `tool.ScanDirSize` (Task 1), `tool.ProcessMemory` (Task 2), `tool.IsRunning`, existing `resolveTool(name)` helper in main.go.
- Produces: `watchDeps.scan` changes signature to `func() (totalBytes, walBytes int64)`; `runWatch(w io.Writer, jsonOut bool, label, toolName string, th monitor.Thresholds, deps watchDeps) recorder.Receipt`. Every existing call site (tests) must be updated.

**Pinned behavior:** default (`codex`) human output identical; `--json` lines gain only `"tool":"codex"`; receipt `Action` stays `"watch"` for codex and becomes `"watch --tool claude"` for claude.

- [ ] **Step 1: Write the failing test** (append to `cmd/codexssd/watch_test.go`)

```go
// TestRunWatchClaudeToolLabels pins the tool-aware surfaces: the JSON "tool"
// field, the display name in notification bodies, and the receipt Action.
func TestRunWatchClaudeToolLabels(t *testing.T) {
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	tick := make(chan time.Time, 1)
	stop := make(chan struct{})
	var notified []string
	sizes := []int64{0, 600 * 1024 * 1024} // 600 MiB in one 30s tick → alarming rate
	step := 0
	now := base
	deps := watchDeps{
		scan:    func() (int64, int64) { s := sizes[step]; return s, 0 },
		memory:  func() (int64, error) { return 0, nil },
		running: func() (bool, error) { return true, nil },
		notify:  func(title, body string) error { notified = append(notified, body); return nil },
		now:     func() time.Time { return now },
		tick:    tick,
		stop:    stop,
	}
	var out strings.Builder
	done := make(chan recorder.Receipt, 1)
	go func() { done <- runWatch(&out, true, "Claude Code", "claude", testThresholds(), deps) }()
	// baseline sampled; advance one tick with a huge write burst
	step = 1
	now = base.Add(30 * time.Second)
	tick <- now
	// let the observe goroutine drain, then stop
	time.Sleep(50 * time.Millisecond)
	close(stop)
	rec := <-done

	if !strings.Contains(out.String(), `"tool":"claude"`) {
		t.Fatalf("json output missing tool field:\n%s", out.String())
	}
	if len(notified) == 0 || !strings.Contains(notified[0], "Claude Code") {
		t.Fatalf("notification body should name Claude Code, got %q", notified)
	}
	if rec.Action != "watch --tool claude" {
		t.Fatalf("receipt Action = %q, want %q", rec.Action, "watch --tool claude")
	}
}
```

Reuse the file's existing threshold helper if one exists under a different name (e.g. a `monitor.Thresholds` literal used by other tests) — the test needs a threshold set whose high rate is below 600 MiB per 30s. If no helper exists, add:

```go
func testThresholds() monitor.Thresholds {
	return monitor.Thresholds{MediumMBPerMin: 25, HighMBPerMin: 100, CriticalMBPerMin: 500,
		HighWALMB: 1024, CriticalWALMB: 8192, HighMemMB: 2048, CriticalMemMB: 6144}
}
```

(Check the real field names in `internal/monitor/risk.go` first and match them exactly.)

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./cmd/codexssd/ -run TestRunWatchClaudeToolLabels -v`
Expected: FAIL — `runWatch` has wrong arity / `scan` field type mismatch

- [ ] **Step 3: Change `watchDeps.scan` and `runWatch`** in `cmd/codexssd/watch.go`

In `watchDeps`, replace:

```go
	scan    func() codex.LogReport
```

with:

```go
	// scan returns the tool's current disk footprint: total bytes plus the
	// WAL size for tools that have one (0 otherwise — the WAL risk checks
	// then simply never fire, no special-casing needed).
	scan    func() (totalBytes, walBytes int64)
```

Change `runWatch`'s signature to:

```go
func runWatch(w io.Writer, jsonOut bool, label, toolName string, th monitor.Thresholds, deps watchDeps) recorder.Receipt {
```

Inside `observe()`, replace the report/WAL extraction:

```go
		report := deps.scan()
		...
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
```

with:

```go
		total, wal := deps.scan()
		...
		if len(samples) == 0 {
			firstTotal = total
		}
		lastTotal = total
		samples = monitor.AppendSample(samples, monitor.Sample{
			At: now, TotalBytes: total, WALBytes: wal, MemBytes: mem,
		}, 20)
```

Also inside `observe()`: the behavioral-tracking print stays as-is (it is only wired for codex, see Step 5), and the JSON event marshalling becomes:

```go
			line, _ := json.Marshal(watchEvent{At: now, Tool: toolName, Level: a.Level.String(), Reasons: reasons, TotalMB: total / (1024 * 1024)})
```

with `watchEvent` gaining the field (keep existing field order, add Tool second):

```go
type watchEvent struct {
	At      time.Time `json:"at"`
	Tool    string    `json:"tool"`
	Level   string    `json:"level"`
	Reasons []string  `json:"reasons"`
	TotalMB int64     `json:"total_log_mb"`
}
```

The notification body becomes label-driven (for codex, `label == "Codex"` reproduces today's string exactly):

```go
			body := label + " disk/memory activity looks alarming."
```

The returned receipt's Action follows the recorder convention:

```go
			action := "watch"
			if toolName != "codex" {
				action += " --tool " + toolName
			}
			return recorder.Receipt{
				At: end, Action: action,
				...
```

- [ ] **Step 4: Wire the flag in `cmdWatch`**

Add the flag beside the others:

```go
	toolName := fs.String("tool", "codex", "which AI tool to watch (codex, claude)")
```

Update usage:

```go
		fmt.Fprintf(os.Stderr, "Usage: codexssd watch [--interval 30s] [--no-notify] [--json] [--tool codex|claude]\n\n")
		fmt.Fprintf(os.Stderr, "Watch a tool's disk and memory in the foreground; Ctrl-C to stop.\n\n")
```

Replace the `dir, err := codex.Dir()` block with the shared resolver:

```go
	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}
```

- [ ] **Step 5: Build tool-aware deps**

Behavioral tracking stays codex-only this round — guard the existing provenance block:

```go
	var trackBehavior func(agentRunning bool, now time.Time) []behavior.Event
	if p.Name == "codex" {
		if provPath, err := behavior.ProvenancePath(); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: note: couldn't determine provenance path: %v — behavioral tracking disabled for this session.\n", err)
		} else {
			tracker := behavior.NewTracker("codex", provPath, readDirNames(dir))
			trackBehavior = func(agentRunning bool, now time.Time) []behavior.Event {
				return tracker.Observe(readDirNames(dir), agentRunning, now)
			}
		}
	}
```

Replace the `deps := watchDeps{...}` literal with a per-tool branch (import `"github.com/0xdefence/codexssd/internal/tool"`):

```go
	deps := watchDeps{
		notify:          notifier,
		now:             time.Now,
		tick:            ticker.C,
		stop:            stop,
		observeBehavior: trackBehavior,
	}
	if p.Name == "codex" {
		// Founding path, byte-for-byte: fixed-file scan with WAL extraction.
		deps.scan = func() (int64, int64) {
			r := codex.ScanLogs(dir)
			var wal int64
			for _, f := range r.Files {
				if f.Name == "logs_2.sqlite-wal" && f.Exists {
					wal = f.Size
				}
			}
			return r.TotalBytes, wal
		}
		deps.memory = codex.ProcessMemory
		deps.running = codex.IsCodexRunning
	} else {
		// Glob-profile tools: whole-dir footprint (no WAL → 0, so WAL risk
		// checks never fire), generic process matching and memory.
		deps.scan = func() (int64, int64) { return tool.ScanDirSize(dir), 0 }
		deps.memory = func() (int64, error) { return tool.ProcessMemory(p) }
		deps.running = func() (bool, error) { return tool.IsRunning(p) }
	}
	rec := runWatch(os.Stdout, *jsonOut, p.DisplayName, p.Name, cfg.MonitorThresholds(), deps)
```

- [ ] **Step 6: Update every existing `runWatch`/`watchDeps` call site in `cmd/codexssd/watch_test.go`**

Mechanical transformation, applied to each existing test:

- `runWatch(w, jsonOut, th, deps)` → `runWatch(w, jsonOut, "Codex", "codex", th, deps)`
- every `scan: func() codex.LogReport { return <report> }` → `scan: func() (int64, int64) { return <total>, <wal> }`, where `<total>`/`<wal>` are the same numbers the old fake report carried (`TotalBytes`, and the size of any `logs_2.sqlite-wal` entry, else 0).
- any assertion on JSON output that pins a full line must add `"tool":"codex"` after `"at":...`.

Do not weaken any other assertion — expected human output strings must remain identical.

- [ ] **Step 7: Update the usage const in `cmd/codexssd/main.go`**

Replace:

```go
  watch          Watch a running Codex agent and warn on risky activity
```

with:

```go
  watch          Watch a running agent and warn on risky activity
```

(The existing "Most commands accept --tool codex|claude (default codex)." line now covers watch too — leave it.)

- [ ] **Step 8: Run the full package tests**

Run: `go test ./cmd/codexssd/ -v -count=1`
Expected: PASS (new test + all updated regressions)

- [ ] **Step 9: Manual smoke check**

Run: `go run ./cmd/codexssd watch --tool claude --interval 5s` for ~10s, Ctrl-C.
Expected: baseline `[...] risk low` line, clean exit summary; no errors. Then `go run ./cmd/codexssd watch -h` shows the new flag.

- [ ] **Step 10: Commit**

```bash
git add cmd/codexssd/watch.go cmd/codexssd/watch_test.go cmd/codexssd/main.go
git commit -m "feat(watch): --tool claude — generic footprint scan, memory, and receipts"
```

---

### Task 4: Tool-aware MCP server

**Files:**
- Modify: `internal/mcpserver/tools.go`
- Modify: `internal/mcpserver/server.go:106` (pass arguments through)
- Test: `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `tool.ByName`, `tool.Codex()`, `Profile.Dir()`, `Profile.CleanablePaths`, `tool.IsRunning`, `cleaner.PlanTool`.
- Produces: `Server` field signatures change to take `tool.Profile`: `status func(p tool.Profile) (any, error)`, `cleanPlan func(p tool.Profile) (any, error)`, `backups func(p tool.Profile) (any, error)`, `diskReport func(p tool.Profile) (visibility.Report, error)`; `selfReport` unchanged. `callTool(name string, rawArgs json.RawMessage)`.

**Pinned behavior:** with no `tool` argument every tool's output is unchanged (codex paths call the exact same functions as today, including `PlanCodexLogs` and the `"codex_running"` key).

- [ ] **Step 1: Write the failing tests** (append to `internal/mcpserver/server_test.go`; follow the file's existing request/response helpers — it stubs `Server` fields directly, so update stub signatures in the same edit)

```go
func TestCallToolClaudeArgument(t *testing.T) {
	var gotTool string
	s := &Server{
		status: func(p tool.Profile) (any, error) { gotTool = p.Name; return map[string]string{"ok": "yes"}, nil },
	}
	if _, err := s.callTool("codex_status", json.RawMessage(`{"tool":"claude"}`)); err != nil {
		t.Fatalf("callTool error: %v", err)
	}
	if gotTool != "claude" {
		t.Fatalf("dispatched tool = %q, want claude", gotTool)
	}
}

func TestCallToolDefaultsToCodex(t *testing.T) {
	var gotTool string
	s := &Server{
		status: func(p tool.Profile) (any, error) { gotTool = p.Name; return "ok", nil },
	}
	// nil, empty object, and empty raw message all mean codex.
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		if _, err := s.callTool("codex_status", raw); err != nil {
			t.Fatalf("callTool(%s) error: %v", raw, err)
		}
		if gotTool != "codex" {
			t.Fatalf("dispatched tool = %q, want codex", gotTool)
		}
	}
}

func TestCallToolUnknownToolErrors(t *testing.T) {
	s := &Server{status: func(tool.Profile) (any, error) { return "ok", nil }}
	if _, err := s.callTool("codex_status", json.RawMessage(`{"tool":"copilot"}`)); err == nil {
		t.Fatal("want error for unknown tool, got nil")
	}
}

func TestToolDescriptorsAdvertiseToolArg(t *testing.T) {
	for _, d := range toolDescriptors() {
		schema := d["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		if _, ok := props["tool"]; !ok {
			t.Fatalf("descriptor %v missing tool property", d["name"])
		}
	}
}
```

Add imports `"encoding/json"` and `"github.com/0xdefence/codexssd/internal/tool"` to the test file as needed.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/mcpserver/ -v`
Expected: FAIL to compile (field signatures / callTool arity)

- [ ] **Step 3: Implement in `internal/mcpserver/tools.go`**

Add import `"github.com/0xdefence/codexssd/internal/tool"` and `"time"` stays. New `Server` and `New()`:

```go
// Server holds the tool data sources as fields so tests can stub them.
// Every source is READ-ONLY by construction. Profile-taking sources receive
// the profile resolved from the request's optional {"tool": ...} argument.
type Server struct {
	status     func(p tool.Profile) (any, error)
	cleanPlan  func(p tool.Profile) (any, error)
	backups    func(p tool.Profile) (any, error)
	selfReport func() (any, error)
	diskReport func(p tool.Profile) (visibility.Report, error)
}

// New returns a Server wired to the real read-only engine functions.
func New() *Server {
	return &Server{
		status: func(p tool.Profile) (any, error) {
			dir, err := p.Dir()
			if err != nil {
				return nil, err
			}
			if p.Name == "codex" {
				// Founding path, unchanged shape.
				return codex.ScanLogs(dir), nil
			}
			// Glob-profile tools: what's cleanable (stale) right now — same
			// deliberate framing as `status --tool claude` on the CLI.
			cfg, _ := config.LoadDefault()
			cleanable := p.CleanablePaths(dir, time.Now(), cfg.StaleAfter())
			var total int64
			files := make([]map[string]any, 0, len(cleanable))
			for _, f := range cleanable {
				files = append(files, map[string]any{"name": f.Rel, "size_bytes": f.Size})
				total += f.Size
			}
			return map[string]any{
				"tool": p.Name, "dir": dir,
				"cleanable_stale_files": files, "cleanable_total_bytes": total,
				"note": "fresh session files are excluded on purpose — they may still be in use",
			}, nil
		},
		cleanPlan: func(p tool.Profile) (any, error) {
			dir, err := p.Dir()
			if err != nil {
				return nil, err
			}
			if p.Name == "codex" {
				// Unchanged codex path, including the codex_running key.
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
			}
			cfg, _ := config.LoadDefault()
			plan, err := cleaner.PlanTool(p, dir, time.Now(), cfg.StaleAfter())
			if err != nil {
				return nil, err
			}
			running, runErr := tool.IsRunning(p)
			out := map[string]any{
				"plan":         plan,
				"tool_running": running,
				"note":         "dry run only — this server cannot move files; cleaning is human-only",
			}
			if runErr != nil && runErr != tool.ErrUnsupportedPlatform {
				out["check_error"] = runErr.Error()
			}
			return out, nil
		},
		backups: func(p tool.Profile) (any, error) {
			dir, err := p.Dir()
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
		diskReport: func(p tool.Profile) (visibility.Report, error) {
			dir, err := p.Dir()
			if err != nil {
				return visibility.Report{}, err
			}
			cfg, _ := config.LoadDefault() // malformed config → defaults; never fails
			return visibility.Scan(dir, time.Now(), cfg.StaleAfter()), nil
		},
	}
}
```

New descriptors and dispatch:

```go
// toolDescriptors lists the five read-only tools. Each accepts one OPTIONAL
// argument — {"tool": "codex"|"claude"} — defaulting to codex; nothing else,
// which keeps the surface unabusable. Names are historical (codex_status is
// named for the founding profile) and stable: renaming would break existing
// client configs.
func toolDescriptors() []map[string]any {
	toolSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tool": map[string]any{
				"type": "string", "enum": []string{"codex", "claude"},
				"description": "Which AI tool to inspect (default codex).",
			},
		},
	}
	mk := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": toolSchema}
	}
	return []map[string]any{
		mk("codex_status", "Sizes of the selected tool's own files (read-only). Named for the founding profile; pass {\"tool\":\"claude\"} for Claude Code."),
		mk("clean_plan", "Dry-run plan of what `codexssd clean` WOULD move aside for the selected tool. This server cannot execute it."),
		mk("list_backups", "The selected tool's recoverable recycling-bin backups with hold information (read-only)."),
		mk("self_report", "CodexSSD's own footprint and action history (read-only; the tool argument is accepted and ignored)."),
		mk("disk_report", "What's using disk inside the selected tool's directory, with stale flags (read-only)."),
	}
}

// parseToolArg resolves the optional {"tool": ...} argument. Absent/empty
// means codex (the founding default); an unknown value is an explicit error,
// never a silent fallback.
func parseToolArg(raw json.RawMessage) (tool.Profile, error) {
	if len(raw) == 0 {
		return tool.Codex(), nil
	}
	var args struct {
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tool.Profile{}, fmt.Errorf("invalid arguments: %v", err)
	}
	if args.Tool == "" {
		return tool.Codex(), nil
	}
	return tool.ByName(args.Tool)
}

// callTool runs one named tool and returns its result as pretty JSON text.
func (s *Server) callTool(name string, rawArgs json.RawMessage) (string, error) {
	p, err := parseToolArg(rawArgs)
	if err != nil {
		return "", err
	}
	var v any
	switch name {
	case "codex_status":
		v, err = s.status(p)
	case "clean_plan":
		v, err = s.cleanPlan(p)
	case "list_backups":
		v, err = s.backups(p)
	case "self_report":
		v, err = s.selfReport() // tool-agnostic; argument accepted and ignored
	case "disk_report":
		v, err = s.diskReport(p)
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

In `server.go:106`, change:

```go
			text, err := s.callTool(params.Name)
```

to:

```go
			text, err := s.callTool(params.Name, params.Arguments)
```

(The existing `fail(-32602, err.Error())` path already turns an unknown-tool error into the invalid-params JSON-RPC error the spec requires.)

- [ ] **Step 4: Update existing stubs in `server_test.go`**

Mechanical: every existing stub field `status: func() (codex.LogReport, error) {...}` and friends becomes profile-taking, e.g. `status: func(tool.Profile) (any, error) {...}`; every existing `s.callTool(name)` call gains a `nil` second argument. Assertions stay identical.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/mcpserver/ -v -count=1`
Expected: PASS

- [ ] **Step 6: Manual smoke check**

```bash
printf '%s\n%s\n' \
 '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
 '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"codex_status","arguments":{"tool":"claude"}}}' \
 | go run ./cmd/codexssd mcp
```

Expected: descriptor list showing the `tool` property; a claude status result with `"tool": "claude"`.

- [ ] **Step 7: Commit**

```bash
git add internal/mcpserver/tools.go internal/mcpserver/server.go internal/mcpserver/server_test.go
git commit -m "feat(mcp): optional tool argument on all five read-only tools"
```

---

### Task 5: `install-agent --tool claude` → CLAUDE.md

**Files:**
- Modify: `internal/agent/profiles.go` (Target type, ContentFor)
- Modify: `internal/agent/install.go` (InstallTarget)
- Modify: `cmd/codexssd/main.go` (cmdInstallAgent)
- Test: `internal/agent/install_test.go`, `internal/agent/profiles_test.go`

**Interfaces:**
- Produces: `type Target struct { FileName, Audience string }`; `var TargetAgents, TargetClaude Target`; `func TargetFor(toolName string) Target`; `func ContentFor(p Profile, t Target) string`; `func InstallTarget(dir string, t Target, p Profile, force bool) (path string, replacedForeign bool, err error)`. Existing `Content(p)` and `Install(dir, p, force)` become thin wrappers over the new functions (AGENTS.md target) so all existing call sites and tests pass unchanged.

- [ ] **Step 1: Write the failing tests** (append to `internal/agent/profiles_test.go` and `install_test.go`)

```go
// profiles_test.go
func TestContentForClaudeTarget(t *testing.T) {
	c := ContentFor(ProfileBalanced, TargetClaude)
	if !strings.Contains(c, "# CLAUDE.md") {
		t.Fatalf("claude content missing heading:\n%s", c)
	}
	if !strings.Contains(c, "House rules for Claude Code in this repo") {
		t.Fatalf("claude content missing audience line:\n%s", c)
	}
	if !strings.HasPrefix(c, "<!-- codexssd:generated") {
		t.Fatal("claude content missing generated marker")
	}
}

func TestContentUnchangedForAgents(t *testing.T) {
	// The AGENTS.md rendering must be byte-for-byte what Content always produced.
	if ContentFor(ProfileBalanced, TargetAgents) != Content(ProfileBalanced) {
		t.Fatal("ContentFor(TargetAgents) diverged from Content")
	}
}

func TestTargetFor(t *testing.T) {
	if TargetFor("claude").FileName != "CLAUDE.md" {
		t.Fatal("claude → CLAUDE.md")
	}
	if TargetFor("codex").FileName != "AGENTS.md" {
		t.Fatal("codex → AGENTS.md")
	}
}
```

```go
// install_test.go
func TestInstallTargetWritesClaudeMd(t *testing.T) {
	dir := t.TempDir()
	path, replaced, err := InstallTarget(dir, TargetClaude, ProfileBalanced, false)
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Fatal("nothing existed to replace")
	}
	if filepath.Base(path) != "CLAUDE.md" {
		t.Fatalf("wrote %s, want CLAUDE.md", path)
	}
	// Refuses to overwrite without force — same contract as AGENTS.md.
	if _, _, err := InstallTarget(dir, TargetClaude, ProfileBalanced, false); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -v`
Expected: FAIL — undefined `ContentFor`, `TargetClaude`, `InstallTarget`, `TargetFor`

- [ ] **Step 3: Implement**

In `profiles.go`, add above `Content`:

```go
// Target selects which agent-instruction file to write: Codex and most agents
// read AGENTS.md, Claude Code reads CLAUDE.md. One shared template with the
// audience substituted keeps the two from ever drifting apart.
type Target struct {
	FileName string // file written into the repo, e.g. "AGENTS.md"
	Audience string // who the intro line addresses
}

var (
	TargetAgents = Target{FileName: "AGENTS.md", Audience: "AI coding agents"}
	TargetClaude = Target{FileName: "CLAUDE.md", Audience: "Claude Code"}
)

// TargetFor maps a --tool name to its instruction-file target.
func TargetFor(toolName string) Target {
	if toolName == "claude" {
		return TargetClaude
	}
	return TargetAgents
}

// ContentFor renders the rules file for a profile and target, including the
// generated marker.
func ContentFor(p Profile, t Target) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s profile=%s -->\n", marker, p)
	fmt.Fprintf(&b, "# %s\n\n", t.FileName)
	fmt.Fprintf(&b, "House rules for %s in this repo, installed by CodexSSD\n", t.Audience)
	b.WriteString("to keep disk and token use sane. Safe to edit.\n\n")
	b.WriteString("## Rules\n\n")
	for _, r := range coreRules {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	for _, r := range profileExtra[p] {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	return b.String()
}
```

Replace the body of `Content` with a wrapper:

```go
// Content renders the AGENTS.md rules for a profile — the founding target,
// kept as a wrapper so existing callers and pinned tests never move.
func Content(p Profile) string { return ContentFor(p, TargetAgents) }
```

In `install.go`, generalize (keep `FileName` const and `Install` as compatibility wrappers):

```go
// InstallTarget writes the target's rules file for the given profile into dir.
// Same contract as Install: refuses to overwrite unless forced, and reports
// whether a forced overwrite replaced a file CodexSSD didn't generate.
func InstallTarget(dir string, t Target, p Profile, force bool) (path string, replacedForeign bool, err error) {
	path = filepath.Join(dir, t.FileName)
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		if !force {
			return "", false, ErrExists
		}
		replacedForeign = !isGenerated(path)
	}
	if err := os.WriteFile(path, []byte(ContentFor(p, t)), 0o644); err != nil {
		return "", false, err
	}
	return path, replacedForeign, nil
}

// Install writes an AGENTS.md for the given profile into dir (the founding
// target, kept for existing callers).
func Install(dir string, p Profile, force bool) (string, bool, error) {
	return InstallTarget(dir, TargetAgents, p, force)
}
```

- [ ] **Step 4: Wire `--tool` into `cmdInstallAgent`** in `cmd/codexssd/main.go`

Add the flag and target resolution (validate the name through `tool.ByName` so junk values get the standard error), and substitute the target everywhere the code currently hardcodes AGENTS.md:

```go
	toolName := fs.String("tool", "codex", "which AI tool the rules file is for (codex → AGENTS.md, claude → CLAUDE.md)")
```

Usage lines become:

```go
		fmt.Fprintf(os.Stderr, "Usage: codexssd install-agent [--profile <name>] [--tool codex|claude] [--force] [--print] [dir]\n\n")
		fmt.Fprintf(os.Stderr, "Write disk/token-safe agent rules into dir (default \".\"):\nAGENTS.md for codex, CLAUDE.md for claude.\n\n")
```

After profile parsing:

```go
	tp, err := tool.ByName(*toolName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
		return 2
	}
	target := agent.TargetFor(tp.Name)
```

Then replace the print/install/error paths:

```go
	if *printOnly {
		fmt.Print(agent.ContentFor(p, target))
		return 0
	}

	path, replacedForeign, err := agent.InstallTarget(dir, target, p, *force)
	if err != nil {
		if errors.Is(err, agent.ErrExists) {
			fmt.Fprintf(os.Stderr, "codexssd: %s already exists — leaving it untouched.\n", filepath.Join(dir, target.FileName))
			fmt.Fprintln(os.Stderr, "Re-run with --force to overwrite, or --print to preview.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "codexssd: could not write %s: %v\n", target.FileName, err)
		return 1
	}
	if replacedForeign {
		fmt.Printf("Note: replaced an existing %s that CodexSSD didn't create.\n", target.FileName)
	}
```

Also update the usage const's install-agent line:

```go
  install-agent  Write disk/token-safe agent rules into a repo (--tool: AGENTS.md or CLAUDE.md)
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/agent/ ./cmd/codexssd/ -count=1`
Expected: PASS. Then manually: `go run ./cmd/codexssd install-agent --tool claude --print | head -4` shows the CLAUDE.md heading.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/ cmd/codexssd/main.go
git commit -m "feat(install-agent): --tool claude writes CLAUDE.md from the shared template"
```

---

### Task 6: TUI plumbing — active tab, Claude risk sampling

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/update.go` (loadedClaudeMsg case only)
- Test: `internal/tui/session_test.go` (receipt), `internal/tui/update_test.go` (sampling)

**Interfaces:**
- Produces: `type toolTab int` with `tabCodex`/`tabClaude`; Model fields `activeTab toolTab`, `claudeSamples []monitor.Sample`, `claudeAssessment monitor.Assessment`, `claudeMemBytes int64`, `claudeTotalBytes int64`, `claudeStartedAt time.Time`, `claudeStartBytes int64`, `claudePeakRate float64`, `claudePeakRisk monitor.Risk`; `loadedClaudeMsg` gains `at time.Time`, `totalBytes int64`, `memBytes int64`; seams `scanClaudeSize = tool.ScanDirSize` and `claudeMemory = func() (int64, error) { return tool.ProcessMemory(tool.Claude()) }`; `notifyCmd(a monitor.Assessment, label string)`. Tasks 7–8 rely on all of these names.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/update_test.go`:

```go
// TestClaudeLoadFeedsRiskSamples pins that Claude snapshots feed their own
// monitor window: two loads 60s apart with a 300 MiB jump must register a
// non-zero claude rate without touching the codex assessment.
func TestClaudeLoadFeedsRiskSamples(t *testing.T) {
	m := New(config.Default())
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	step := func(at time.Time, total int64) Model {
		next, _ := m.Update(loadedClaudeMsg{at: at, dir: "/home/u/.claude", totalBytes: total, supported: true})
		return next.(Model)
	}
	m = step(base, 0)
	m = step(base.Add(time.Minute), 300*1024*1024)
	if m.claudeAssessment.RateMBPerMin <= 0 {
		t.Fatalf("claude rate = %f, want > 0", m.claudeAssessment.RateMBPerMin)
	}
	if m.assessment.RateMBPerMin != 0 {
		t.Fatalf("codex assessment must be untouched, got rate %f", m.assessment.RateMBPerMin)
	}
	if m.claudeStartBytes != 0 || m.claudeStartedAt != base {
		t.Fatalf("claude session start not captured: startBytes=%d startedAt=%v", m.claudeStartBytes, m.claudeStartedAt)
	}
}
```

Append to `internal/tui/session_test.go`:

```go
// TestSessionReceiptReportsPeakTool pins the receipt convention: when Claude
// peaked harder than Codex this session, the receipt says so via the Action
// suffix and carries Claude's growth and peaks.
func TestSessionReceiptReportsPeakTool(t *testing.T) {
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC)
	m := Model{
		startedAt: now.Add(-10 * time.Minute), startBytes: 0,
		peakRate: 5, peakRisk: monitor.RiskLow,
		claudeStartedAt: now.Add(-10 * time.Minute), claudeStartBytes: 0,
		claudeTotalBytes: 200 * 1024 * 1024,
		claudePeakRate:   50, claudePeakRisk: monitor.RiskHigh,
	}
	rec := m.sessionReceipt(now)
	if rec.Action != "session --tool claude" {
		t.Fatalf("Action = %q, want %q", rec.Action, "session --tool claude")
	}
	if rec.PeakMBPerMin != 50 || rec.Risk != monitor.RiskHigh.String() {
		t.Fatalf("receipt should carry claude peaks, got %+v", rec)
	}
	if rec.DiskWritten != 200*1024*1024 {
		t.Fatalf("DiskWritten = %d, want claude growth", rec.DiskWritten)
	}
}

// TestSessionReceiptCodexDefault pins that a codex-peaked (or all-quiet)
// session keeps the historical Action "session" — old receipt lines and their
// readers never change meaning.
func TestSessionReceiptCodexDefault(t *testing.T) {
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC)
	m := Model{startedAt: now.Add(-time.Minute), peakRate: 5, peakRisk: monitor.RiskLow}
	if rec := m.sessionReceipt(now); rec.Action != "session" {
		t.Fatalf("Action = %q, want %q", rec.Action, "session")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestClaudeLoadFeedsRiskSamples|TestSessionReceipt' -v`
Expected: FAIL — unknown fields `at`/`totalBytes` on `loadedClaudeMsg`, etc.

- [ ] **Step 3: Implement model fields** (`internal/tui/model.go`)

Add below the `state` consts:

```go
// toolTab identifies which tool the dashboard is currently scoped to.
type toolTab int

const (
	tabCodex toolTab = iota
	tabClaude
)
```

Add to `Model` (after the existing claude* block):

```go
	// activeTab scopes the dashboard: every panel, banner, and c/r action
	// operates on this tool. tab/1/2/l switch it (Task 7).
	activeTab toolTab

	// Claude monitor state, parallel to samples/assessment above. Sampled on
	// every poll for BOTH tools so switching tabs never shows an empty history.
	claudeSamples    []monitor.Sample
	claudeAssessment monitor.Assessment
	claudeMemBytes   int64
	claudeTotalBytes int64

	// Claude session tracking, parallel to startedAt/startBytes/peak* above.
	claudeStartedAt  time.Time
	claudeStartBytes int64
	claudePeakRate   float64
	claudePeakRisk   monitor.Risk
```

Replace `sessionReceipt` with the peak-tool version:

```go
// sessionReceipt summarizes this dashboard session for the JSONL history,
// reporting whichever tool peaked harder. Action follows the recorder
// convention: "session" (codex, the historical default) or
// "session --tool claude". Growth is measured from each tool's own first
// load, clamped so a mid-session tidy never reports negative.
func (m Model) sessionReceipt(now time.Time) recorder.Receipt {
	started := m.startedAt
	if started.IsZero() || (!m.claudeStartedAt.IsZero() && m.claudeStartedAt.Before(started)) {
		started = m.claudeStartedAt
	}
	dur := 0.0
	if !started.IsZero() {
		dur = now.Sub(started).Seconds()
	}
	action := "session"
	grew := m.report.TotalBytes - m.startBytes
	rate, risk := m.peakRate, m.peakRisk
	if m.claudePeakRate > m.peakRate {
		action = "session --tool claude"
		grew = m.claudeTotalBytes - m.claudeStartBytes
		rate, risk = m.claudePeakRate, m.claudePeakRisk
	}
	if grew < 0 {
		grew = 0
	}
	return recorder.Receipt{
		At:           now,
		Action:       action,
		DurationSec:  dur,
		DiskWritten:  grew,
		PeakMBPerMin: rate,
		Risk:         risk.String(),
	}
}
```

- [ ] **Step 4: Implement commands plumbing** (`internal/tui/commands.go`)

Add seams beside the existing Claude seams:

```go
	// scanClaudeSize measures ~/.claude's whole footprint (backups excluded)
	// for the risk monitor — Claude has no fixed log set to scan.
	scanClaudeSize = tool.ScanDirSize
	claudeMemory   = func() (int64, error) { return tool.ProcessMemory(tool.Claude()) }
```

Extend `loadedClaudeMsg`:

```go
type loadedClaudeMsg struct {
	at         time.Time
	dir        string
	loadErr    error
	running    bool
	supported  bool
	runErr     error
	cleanable  []tool.FoundFile
	plan       cleaner.Plan
	backups    []cleaner.Backup
	processes  []tool.Process
	totalBytes int64 // whole-dir footprint for the risk monitor
	memBytes   int64 // total Claude RSS (0 when unknown)
}
```

In `loadClaudeCmd`, gather and return them:

```go
		totalBytes := scanClaudeSize(dir)
		mem, _ := claudeMemory() // best-effort; 0 on any error — never blocks the dashboard
		return loadedClaudeMsg{
			at: time.Now(), dir: dir, running: running, supported: supported, runErr: runErr,
			cleanable: cleanable, plan: plan, backups: backups, processes: procs,
			totalBytes: totalBytes, memBytes: mem,
		}
```

Change `notifyCmd` to take the display label (and update its one existing call site in update.go to `notifyCmd(m.assessment, "Codex")`):

```go
func notifyCmd(a monitor.Assessment, label string) tea.Cmd {
	return func() tea.Msg {
		go func() {
			body := label + " disk/memory activity looks alarming."
			if len(a.Reasons) > 0 {
				body = a.Reasons[0]
			}
			_ = notifyFn("CodexSSD: "+a.Level.String(), body)
		}()
		return nil
	}
}
```

- [ ] **Step 5: Extend the `loadedClaudeMsg` case in `update.go`**

Replace the case body with (mirrors the codex `loadedMsg` case, WAL always 0):

```go
	case loadedClaudeMsg:
		m.claudeLoaded = true
		m.claudeDir = msg.dir
		m.claudeLoadErr = msg.loadErr
		m.claudeRunning = msg.running
		m.claudeSupported = msg.supported
		m.claudeRunErr = msg.runErr
		m.claudeCleanable = msg.cleanable
		m.claudePlan = msg.plan
		m.claudeBackups = msg.backups
		m.claudeProcesses = msg.processes
		m.claudeMemBytes = msg.memBytes
		m.claudeTotalBytes = msg.totalBytes
		if msg.loadErr != nil || msg.at.IsZero() {
			return m, nil
		}
		last := m.claudeAssessment.Level
		s := monitor.Sample{At: msg.at, TotalBytes: msg.totalBytes, WALBytes: 0, MemBytes: msg.memBytes}
		m.claudeSamples = monitor.AppendSample(m.claudeSamples, s, maxSamples)
		m.claudeAssessment = monitor.Evaluate(m.claudeSamples, m.claudeRunning, m.cfg.MonitorThresholds())
		if m.claudeStartedAt.IsZero() {
			m.claudeStartedAt = msg.at
			m.claudeStartBytes = msg.totalBytes
		}
		if m.claudeAssessment.RateMBPerMin > m.claudePeakRate {
			m.claudePeakRate = m.claudeAssessment.RateMBPerMin
		}
		if m.claudeAssessment.Level > m.claudePeakRisk {
			m.claudePeakRisk = m.claudeAssessment.Level
		}
		if m.cfg.Notifications && escalatedToAlarming(last, m.claudeAssessment.Level) {
			return m, notifyCmd(m.claudeAssessment, "Claude Code")
		}
		return m, nil
```

- [ ] **Step 6: Run tests, fix compile fallout, commit**

Run: `go test ./internal/tui/ -count=1`
Expected: new tests PASS; any existing test constructing `loadedClaudeMsg` still compiles (new fields are zero-valued extras). Existing `notifyCmd(m.assessment)` call updated in Step 4.

```bash
git add internal/tui/
git commit -m "feat(tui): per-tool risk sampling, session peaks, and peak-tool receipts"
```

---

### Task 7: TUI state collapse + tab key routing

**Files:**
- Modify: `internal/tui/model.go` (remove 4 states + `returnState`)
- Modify: `internal/tui/update.go`
- Test: `internal/tui/claude_test.go`, `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `activeTab`/`tabCodex`/`tabClaude` (Task 6); existing `cleanCmd`, `claudeCleanCmd`, `restoreCmd`, `claudeRestoreCmd`, `loadCmd`, `loadClaudeCmd`.
- Produces: states `stateClaude`, `stateClaudeConfirmClean`, `stateClaudeRestoreList`, `stateClaudeConfirmRestore` and the `returnState` field are DELETED; `func (m Model) activeBackups() []cleaner.Backup`; keys `tab`, `1`, `2`, `l` switch tabs. Task 8's views key off `m.activeTab` only.

- [ ] **Step 1: Write the failing tests** (append to `internal/tui/update_test.go`)

```go
func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return next.(Model)
}

// TestTabSwitching pins the mode-switch keys: tab cycles, 1/2 jump, l is the
// compatibility alias for the Claude tab.
func TestTabSwitching(t *testing.T) {
	m := New(config.Default())
	m = pressKey(t, m, "\t")
	if m.activeTab != tabClaude {
		t.Fatal("tab should switch to Claude")
	}
	m = pressKey(t, m, "\t")
	if m.activeTab != tabCodex {
		t.Fatal("tab should cycle back to Codex")
	}
	if m = pressKey(t, m, "2"); m.activeTab != tabClaude {
		t.Fatal("2 should jump to Claude")
	}
	if m = pressKey(t, m, "1"); m.activeTab != tabCodex {
		t.Fatal("1 should jump to Codex")
	}
	if m = pressKey(t, m, "l"); m.activeTab != tabClaude {
		t.Fatal("l should remain an alias for the Claude tab")
	}
	if m.state != stateDashboard {
		t.Fatal("switching tabs must never leave the dashboard")
	}
}

// TestCleanRoutesByActiveTab pins that c on the Claude tab runs the CLAUDE
// gates (blocked while running), never the Codex ones.
func TestCleanRoutesByActiveTab(t *testing.T) {
	m := New(config.Default())
	m.activeTab = tabClaude
	m.claudeSupported = true
	m.claudeRunning = true
	m = pressKey(t, m, "c")
	if m.state != stateBlocked {
		t.Fatalf("state = %v, want stateBlocked", m.state)
	}
	if !strings.Contains(m.blockedReason, "Claude Code") {
		t.Fatalf("blocked reason should name Claude Code: %q", m.blockedReason)
	}
}
```

(Note tab key: Bubble Tea delivers tab as `tea.KeyMsg{Type: tea.KeyTab}` — in `pressKey`, special-case it: `if key == "\t" { next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}); return next.(Model) }`. Include that in the helper.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestTabSwitching|TestCleanRoutesByActiveTab' -v`
Expected: FAIL (tab does nothing; c on claude tab runs codex gates)

- [ ] **Step 3: Delete the folded states and `returnState`**

In `model.go`: remove `stateInfo`'s trailing siblings `stateClaude`, `stateClaudeConfirmClean`, `stateClaudeRestoreList`, `stateClaudeConfirmRestore` from the const block; remove the `returnState state` field (keep `workingLabel`); remove the `claudeLoaded` gating comment's reference to "pressing l dispatches" (the field itself stays — it still gates "loading…" on the Claude tab). Add the helper:

```go
// activeBackups returns the backups list for the tool the dashboard is
// currently scoped to.
func (m Model) activeBackups() []cleaner.Backup {
	if m.activeTab == tabClaude {
		return m.claudeBackups
	}
	return m.backups
}
```

- [ ] **Step 4: Rewrite key routing in `update.go`**

`handleDashboardKey` becomes tab-aware; `handleClaudeKey` is DELETED (its gate logic moves here):

```go
// handleDashboardKey handles keys on the main screen. c/r act on the tool the
// dashboard is currently scoped to (activeTab); each tool keeps its own
// independent running-check gate.
func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		if m.activeTab == tabCodex {
			m.activeTab = tabClaude
		} else {
			m.activeTab = tabCodex
		}
		return m, nil
	case "1":
		m.activeTab = tabCodex
		return m, nil
	case "2", "l": // l kept as the historical Claude-screen key
		m.activeTab = tabClaude
		return m, nil
	case "c":
		return m.startClean()
	case "r":
		return m.startRestore()
	case "i":
		m.state = stateInfo
		m.infoLoaded = false
		return m, infoCmd(m.cfg.StaleAfter())
	}
	return m, nil
}

// startClean applies the active tool's pre-flight gates, then opens the
// confirm screen. The authoritative running-check happens again inside
// cleanCmd/claudeCleanCmd right before any file moves.
func (m Model) startClean() (tea.Model, tea.Cmd) {
	if m.activeTab == tabClaude {
		if !m.claudeSupported {
			m.state = stateBlocked
			m.blockedReason = "This platform can't verify Claude Code is closed, so tidying is disabled here."
			return m, nil
		}
		if m.claudeRunning {
			m.state = stateBlocked
			m.blockedReason = "Claude Code appears to be running. Close it first, then try again."
			return m, nil
		}
		if m.claudePlan.Empty() {
			m.state = stateResult
			m.resultMsg = "Nothing to tidy — no stale Claude Code files are present."
			m.resultErr = nil
			return m, nil
		}
		m.state = stateConfirmClean
		return m, nil
	}
	if !m.supported {
		m.state = stateBlocked
		m.blockedReason = "This platform can't verify Codex is closed, so tidying is disabled here."
		return m, nil
	}
	if m.running {
		m.state = stateBlocked
		m.blockedReason = "Codex appears to be running. Close it first, then try again."
		return m, nil
	}
	if m.plan.Empty() {
		m.state = stateResult
		m.resultMsg = "Nothing to tidy — no Codex logs are present."
		m.resultErr = nil
		return m, nil
	}
	m.state = stateConfirmClean
	return m, nil
}

// startRestore opens the restore list for the active tool, or explains that
// there is nothing to restore.
func (m Model) startRestore() (tea.Model, tea.Cmd) {
	if len(m.activeBackups()) == 0 {
		m.state = stateResult
		if m.activeTab == tabClaude {
			m.resultMsg = "No Claude Code backups to restore — nothing has been tidied yet."
		} else {
			m.resultMsg = "No backups to restore — nothing has been tidied yet."
		}
		m.resultErr = nil
		return m, nil
	}
	m.selected = 0
	m.state = stateRestoreList
	return m, nil
}
```

The generic state cases collapse to per-tab dispatch (replace the `stateConfirmClean`, `stateRestoreList`, `stateConfirmRestore`, and result/blocked cases; delete the four `stateClaude*` cases entirely):

```go
	case stateConfirmClean:
		switch msg.String() {
		case "y":
			if m.activeTab == tabClaude {
				m.workingLabel = "Tidying Claude Code's stale files aside…"
				m.state = stateCleaning
				return m, claudeCleanCmd(m.cfg.BinHold(), m.cfg.StaleAfter())
			}
			m.workingLabel = "Tidying Codex logs aside…"
			m.state = stateCleaning
			return m, cleanCmd(m.cfg.BinHold())
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateRestoreList:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.activeBackups())-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			m.state = stateConfirmRestore
			return m, nil
		case "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateConfirmRestore:
		switch msg.String() {
		case "y":
			m.workingLabel = "Restoring…"
			m.state = stateRestoring
			if m.activeTab == tabClaude {
				return m, claudeRestoreCmd(m.claudeBackups[m.selected].Dir)
			}
			return m, restoreCmd(m.backups[m.selected].Dir)
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateResult, stateBlocked, stateError:
		switch msg.String() {
		case "enter", "esc":
			m.state = stateDashboard
			if m.activeTab == tabClaude {
				return m, loadClaudeCmd(m.cfg.StaleAfter())
			}
			return m, loadCmd // refresh after returning
		}
```

In the `cleanResultMsg` and `restoreResultMsg` cases, replace every `m.returnState == stateClaude` check with `m.activeTab == tabClaude` (wording logic otherwise identical). Remove all remaining `m.returnState = ...` assignments.

- [ ] **Step 5: Rewrite the tests that used the deleted states**

In `internal/tui/claude_test.go` and any other test referencing `stateClaude*` or `returnState`:

- Entry: replace "press l → `stateClaude`" setups with `m.activeTab = tabClaude` (still on `stateDashboard`).
- Flows: `stateClaudeConfirmClean` → `stateConfirmClean` with `activeTab: tabClaude` (same for restore list/confirm).
- Return-wording assertions keyed on `returnState` now set `activeTab: tabClaude` instead.
- Assertions about which command was dispatched (via the seam variables) stay identical — the same `claudeCleanCmd`/`claudeRestoreCmd` must fire, proving the safety gates moved intact.

Keep every behavioral expectation (blocked reasons, result strings, gate ordering) — only the state names change. If a test exists asserting `l` opens `stateClaude`, its replacement asserts `l` sets `activeTab == tabClaude` and stays on the dashboard.

- [ ] **Step 6: Run the full TUI tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS (view tests may still fail until Task 8 — if so, note which and proceed only if failures are exclusively in view rendering of the old Claude screen; Task 8 fixes them. Otherwise fix here.)

- [ ] **Step 7: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): tab-scoped dashboard — fold the Claude screen into generic states"
```

---

### Task 8: TUI tab-bar view

**Files:**
- Modify: `internal/tui/view.go`
- Test: `internal/tui/view_test.go`

**Interfaces:**
- Consumes: `m.activeTab`, `m.claude*` fields (Task 6), `panel`, `statusBar`, `truncateCells`, `effectiveWidth`, `renderLogo`, `renderCompactLogo`, styles (`headerStyle`, `mutedTextStyle`, `selectedRowStyle`, `riskStyle`, `riskGlyph`).
- Produces: `renderTabBar(active toolTab, width int) string`; per-tab `renderDashboard`; updated footer/help. Deletes `renderClaude`, `renderClaudeConfirmClean`, `renderClaudeRestoreList`, `renderClaudeConfirmRestore`, `claudePanelLine`, `returnDescription`.

- [ ] **Step 1: Write the failing tests** (append to `internal/tui/view_test.go`)

```go
func TestDashboardShowsTabBar(t *testing.T) {
	m := New(config.Default())
	m.width = 100
	v := m.View()
	if !strings.Contains(v, "Codex") || !strings.Contains(v, "Claude Code") {
		t.Fatalf("dashboard missing tab labels:\n%s", v)
	}
	if !strings.Contains(v, "tab switch tool") {
		t.Fatalf("footer missing tab hint:\n%s", v)
	}
}

func TestClaudeTabShowsClaudePanels(t *testing.T) {
	m := New(config.Default())
	m.width = 100
	m.activeTab = tabClaude
	m.claudeLoaded = true
	m.claudeDir = "/home/u/.claude"
	m.claudeCleanable = []tool.FoundFile{{Rel: "projects/a/old.jsonl", Size: 2048}}
	v := m.View()
	if !strings.Contains(v, "/home/u/.claude") || !strings.Contains(v, "projects/a/old.jsonl") {
		t.Fatalf("claude tab missing claude folder content:\n%s", v)
	}
	if strings.Contains(v, "logs_2.sqlite") {
		t.Fatalf("claude tab must not show codex files:\n%s", v)
	}
}

func TestConfirmCleanWordingPerTab(t *testing.T) {
	m := New(config.Default())
	m.width = 100
	m.state = stateConfirmClean
	if v := m.View(); !strings.Contains(v, "Codex") {
		t.Fatalf("codex confirm wording missing:\n%s", v)
	}
	m.activeTab = tabClaude
	if v := m.View(); !strings.Contains(v, "Claude Code") {
		t.Fatalf("claude confirm wording missing:\n%s", v)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestDashboardShowsTabBar|TestClaudeTabShowsClaudePanels|TestConfirmCleanWordingPerTab' -v`
Expected: FAIL

- [ ] **Step 3: Implement the tab bar**

```go
// renderTabBar renders the persistent tool switcher shown under the logo.
// The active tool is bracketed and highlighted so the mode is unmissable.
func renderTabBar(active toolTab, width int) string {
	codexL, claudeL := "  Codex  ", "  Claude Code  "
	if active == tabCodex {
		codexL = selectedRowStyle.Render("[ Codex ]")
		claudeL = mutedTextStyle.Render(claudeL)
	} else {
		codexL = mutedTextStyle.Render(codexL)
		claudeL = selectedRowStyle.Render("[ Claude Code ]")
	}
	return truncateCells(codexL+" "+claudeL, width)
}
```

- [ ] **Step 4: Make `renderDashboard` per-tab**

Structure (full function): after `renderLogo(w)` append `renderTabBar(m.activeTab, w)` and a blank line; then branch:

```go
	if m.activeTab == tabClaude {
		return m.renderClaudeDashboard(sections, w)
	}
```

with the codex remainder unchanged from today except: the full-width `panel("Claude Code", m.claudePanelLine(), w)` section is REMOVED (the tab replaces it), and the footer/status-bar strings change to:

```go
	sections = append(sections, "", statusBar(m.footer(), "watching ~/.codex + ~/.claude · updates every "+friendlyInterval(m.cfg.PollInterval()), w))
```

New Claude-tab body (folds the old `renderClaude` content into dashboard panels — reuse its exact strings):

```go
// renderClaudeDashboard renders the dashboard scoped to Claude Code: the
// itemized cleanable-stale listing (folded in from the old dedicated screen),
// Claude's own risk panel, and Claude's recycling bin.
func (m Model) renderClaudeDashboard(sections []string, w int) string {
	if m.claudeLoadErr != nil {
		sections = append(sections,
			panel("Claude Code folder", fmt.Sprintf("Could not read Claude Code's folder: %v", m.claudeLoadErr), w),
			"",
			statusBar(m.footer(), "watching ~/.codex + ~/.claude", w),
		)
		return strings.Join(sections, "\n")
	}
	if !m.claudeLoaded {
		sections = append(sections, panel("Claude Code folder", "loading…", w), "",
			statusBar(m.footer(), "watching ~/.codex + ~/.claude", w))
		return strings.Join(sections, "\n")
	}

	var files strings.Builder
	fmt.Fprintf(&files, "%s\n", m.claudeDir)
	if len(m.claudeCleanable) == 0 {
		fmt.Fprint(&files, "Nothing stale to report right now — no cleanable Claude Code files were found.")
	} else {
		var total int64
		for _, f := range m.claudeCleanable {
			fmt.Fprintf(&files, "%-40s %10s\n", f.Rel, codex.HumanBytes(f.Size))
			total += f.Size
		}
		fmt.Fprintf(&files, "%-40s %10s", "Total", codex.HumanBytes(total))
	}

	var risk strings.Builder
	lvl := m.claudeAssessment.Level
	fmt.Fprintf(&risk, "%s %s\n", riskStyle(lvl).Render(riskGlyph(lvl)), riskStyle(lvl).Render(lvl.String()))
	if lvl >= monitor.RiskMedium {
		reason := ""
		if len(m.claudeAssessment.Reasons) > 0 {
			reason = " · " + strings.Join(m.claudeAssessment.Reasons, " · ")
		}
		fmt.Fprintf(&risk, "%.0f MB/min%s\n", m.claudeAssessment.RateMBPerMin, reason)
	}
	switch {
	case !m.claudeSupported:
		fmt.Fprint(&risk, "Claude Code: can't check")
	case m.claudeRunning && len(m.claudeProcesses) > 0:
		fmt.Fprint(&risk, formatRunningProcesses("Claude Code", m.claudeProcesses))
	case m.claudeRunning:
		fmt.Fprint(&risk, "Claude Code: running")
	default:
		fmt.Fprint(&risk, "Claude Code: not running")
	}
	if m.claudeRunning && m.claudeMemBytes > 0 {
		fmt.Fprintf(&risk, "\nmemory: %s", codex.HumanBytes(m.claudeMemBytes))
	}

	const twoColMin = 72
	if w >= twoColMin {
		leftW := (w - 2) / 2
		rightW := w - 2 - leftW
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Claude Code folder", files.String(), leftW), "  ", panel("Risk", risk.String(), rightW)))
	} else {
		sections = append(sections, panel("Claude Code folder", files.String(), w), panel("Risk", risk.String(), w))
	}

	bin := "empty"
	if t, ok := m.claudeLastTidy(); ok {
		bin = fmt.Sprintf("%d backup(s) · last tidy %s", len(m.claudeBackups), t.Format("2006-01-02 15:04"))
		if s, ok := m.claudeSoonestRelease(); ok {
			bin += fmt.Sprintf(" · next release %s", s.Format("2006-01-02"))
		}
	}
	sections = append(sections, panel("Recycling bin", bin, w))
	sections = append(sections, mutedTextStyle.Render("Fresh Claude Code session files aren't listed on purpose — they're still in use."))
	if m.releaseNote != "" {
		sections = append(sections, mutedTextStyle.Render(m.releaseNote))
	}
	sections = append(sections, "", statusBar(m.footer(), "watching ~/.codex + ~/.claude · updates every "+friendlyInterval(m.cfg.PollInterval()), w))
	return strings.Join(sections, "\n")
}
```

- [ ] **Step 5: Per-tab wording for the shared screens; delete dead renderers**

Footer:

```go
func (m Model) footer() string {
	return "tab switch tool · c tidy · r restore · i info · ? help · q quit"
}
```

`renderConfirmClean` branches by tab (claude body reuses the old `renderClaudeConfirmClean` strings verbatim):

```go
func (m Model) renderConfirmClean() string {
	if m.activeTab == tabClaude {
		body := fmt.Sprintf("Move %s of Claude Code's stale session files into a recoverable bin?\nNothing is deleted — you can restore them any time.",
			codex.HumanBytes(m.claudePlan.TotalBytes))
		return m.screen("Tidy Claude Code files", body, "y yes · n no")
	}
	body := fmt.Sprintf("Move %s of Codex's own logs into a recoverable bin?\nNothing is deleted — you can restore them any time.",
		codex.HumanBytes(m.report.TotalBytes))
	return m.screen("Tidy Codex logs", body, "y yes · n no")
}
```

`renderRestoreList`/`renderConfirmRestore`: swap `m.backups` for `m.activeBackups()`, and in the confirm body use the tool name (`"Codex"` / `"Claude Code"` by `m.activeTab`) in "back to your %s folder?". Titles: restore list becomes `"Restore a backup"` for codex and `"Restore a Claude Code backup"` for claude (preserving both today's strings).

`renderResult`/`renderBlocked`: replace `"enter "+m.returnDescription()` with the constant `"enter return to dashboard"` (the dashboard is now always the return target; the tab preserves tool context). Delete `returnDescription`.

Delete: `renderClaude`, `renderClaudeConfirmClean`, `renderClaudeRestoreList`, `renderClaudeConfirmRestore`, `claudePanelLine`, and their `View()` dispatch cases (removed with the states in Task 7).

Help:

```go
func (m Model) renderHelp() string {
	body := strings.Join([]string{
		"tab  switch between Codex and Claude Code (1/2 jump, l = Claude Code)",
		"c    tidy the current tool's files aside (recoverable)",
		"r    restore previously tidied files",
		"i    open the info screen (settings, self-footprint, disk report)",
		"?    toggle this help",
		"q    quit",
	}, "\n")
	return m.screen("CodexSSD — help", body, "? or esc to close")
}
```

Banner (codex tab only — the claude tab has its own fixed note line): unchanged logic. On the claude tab add an actionable nudge before the release note when there is something to tidy and Claude is closed:

```go
	// (inside renderClaudeDashboard, before the release-note append)
	var staleTotal int64
	for _, f := range m.claudeCleanable {
		staleTotal += f.Size
	}
	if staleTotal >= deadweightThreshold && m.claudeSupported && !m.claudeRunning {
		sections = append(sections, headerStyle.Render(fmt.Sprintf("⚠  %s of stale Claude Code files piled up — press c to tidy.", codex.HumanBytes(staleTotal))))
	}
```

- [ ] **Step 6: Update existing view tests that asserted the old footer/help/Claude-screen strings**

Mechanical: footer assertions expect the new `tab switch tool · …` string; help assertions expect the new lines; any test rendering `stateClaude` now sets `activeTab = tabClaude` on the dashboard instead. Behavioral assertions (which sizes/files/warnings appear) stay identical.

- [ ] **Step 7: Run all TUI tests, then the full suite**

Run: `go test ./internal/tui/ -count=1 && go build ./... && go vet ./... && go test ./... -count=1`
Expected: all PASS

- [ ] **Step 8: Manual drive**

Run `go run ./cmd/codexssd` in a real terminal: verify tab bar renders, `tab`/`1`/`2`/`l` switch, claude tab shows real `~/.claude` data, `c` on each tab shows the right confirm wording, `?` lists the new keys, narrow-terminal (< 72 cols) stacking works.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): persistent Codex/Claude tab bar with per-tab panels and actions"
```

---

### Task 9: README + CLAUDE.md visibility

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

No code; keep wording plain-language. Verify every claim against the shipped behavior of Tasks 1–8 before writing it.

- [ ] **Step 1: README — add the callout at the top of Usage**

Immediately after the `## Usage` heading (before the dashboard section), insert:

```markdown
### Works with Codex and Claude Code

Every data command takes `--tool codex|claude` (default `codex`), and the
dashboard has a tab for each tool (`tab` to switch):

| Command | Codex | Claude Code |
| --- | --- | --- |
| `status` / `report` / `clean` / `restore` / `prune` | ✓ | ✓ (`--tool claude`) |
| `watch` | ✓ | ✓ (`--tool claude`) |
| `install-agent` | ✓ AGENTS.md | ✓ CLAUDE.md (`--tool claude`) |
| `mcp` (five read-only tools) | ✓ | ✓ (`{"tool":"claude"}` argument) |
| dashboard (bare `codexssd`) | ✓ Codex tab | ✓ Claude Code tab |

Claude Code is handled more conservatively than Codex on purpose — see
[Beyond Codex: Claude Code](#beyond-codex-claude-code) for what is (and
deliberately isn't) cleanable.
```

- [ ] **Step 2: README — update the affected sections**

- `watch` section: remove "`watch` stays Codex-only for now." sentence wherever it appears; add `codexssd watch --tool claude` to the example block with a comment; mention the JSON `tool` field.
- Dashboard section: document the tab bar (`tab` cycles, `1`/`2` jump, `l` = Claude Code tab), that panels/actions follow the active tab, and that both tools are sampled continuously.
- `install-agent` section: document `--tool claude` → CLAUDE.md.
- MCP section: document the optional `{"tool": "codex"|"claude"}` argument, the unchanged five names, and that `self_report` ignores it.
- "Beyond Codex: Claude Code": trim to the safety rationale (stale-only cleaning, NeverTouch list, why `--resume` makes fresh transcripts off-limits); remove the per-command flag walkthrough now covered by the matrix.

- [ ] **Step 3: CLAUDE.md — sync the repo instructions**

- `watch` bullet: add `--tool`.
- `install-agent` bullet: add `--tool` (AGENTS.md/CLAUDE.md).
- `mcp` bullet + safety rule 7: still exactly five read-only tools; note each accepts an optional `tool` argument.
- TUI bullet: bare `codexssd` is a tabbed dashboard covering both tools.

- [ ] **Step 4: Verify and commit**

Re-read both files start to finish checking no stale "Codex-only" claims remain. Run `go build ./... && go vet ./... && go test ./... -count=1` (docs-only, but per project convention).

```bash
git add README.md CLAUDE.md
git commit -m "docs: surface codex/claude mode switching — support matrix, tab bar, MCP arg"
```

---

### Task 10: Full verification gate

**Files:** none new.

- [ ] **Step 1: Full gate**

Run: `go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l .`
Expected: builds, vets, all tests pass, `gofmt -l` prints nothing.

- [ ] **Step 2: Codex regression sweep**

Run each and eyeball that output is unchanged from main's behavior:

```bash
go run ./cmd/codexssd status
go run ./cmd/codexssd clean          # dry run
go run ./cmd/codexssd watch -h
go run ./cmd/codexssd install-agent --print | head -4
printf '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"codex_status"}}\n' | go run ./cmd/codexssd mcp
```

- [ ] **Step 3: Claude smoke sweep**

```bash
go run ./cmd/codexssd watch --tool claude --interval 5s   # Ctrl-C after ~10s
go run ./cmd/codexssd install-agent --tool claude --print | head -4
printf '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"clean_plan","arguments":{"tool":"claude"}}}\n' | go run ./cmd/codexssd mcp
```

- [ ] **Step 4: Commit anything outstanding, then hand off**

Use superpowers:finishing-a-development-branch (PR against `main` from `feat/claude-code-parity`).
