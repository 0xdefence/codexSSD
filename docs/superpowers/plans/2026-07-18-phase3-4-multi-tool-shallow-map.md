# Phase 3 + 4 Core: Tool Profiles, Claude Code Support, Shallow Map, Behavioral Detection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generalize CodexSSD from a Codex-only watchdog into a multi-tool one (adding Claude Code end-to-end), ship Phase 3's shallow connection map, and ship Phase 4's behavioral detection — all read-only-first, allow-list-gated, in one worktree.

**Architecture:** A new `internal/tool` package defines per-tool Profiles (data dir, own-file allow-list, never-touch list, process signatures); `cleaner`, `status`, `report`, `clean`, `restore`, `prune` are re-pointed through profiles with `--tool` (default `codex`, so all existing behavior and tests are unchanged). Phase 3 is a new `internal/shallowmap` package that probes whether anything obvious points at a flagged entry (Connected → hands off; Unknown → still report-only). Phase 4's behavioral detection is a new `internal/behavior` package that records, during `watch`, which entries appeared while the agent was running (one JSONL line per appearance, never per-poll).

**Tech Stack:** Go stdlib only (no new dependencies). Existing packages: `internal/codex`, `internal/cleaner`, `internal/visibility`, `internal/config`, `internal/recorder`, `cmd/codexssd`.

**Worktree:** Execute this plan in an isolated worktree created via `superpowers:using-git-worktrees`, branch `feat/phase3-4-multi-tool` based on **`main`** (not `feat/tui-overhaul` — this plan does not touch `internal/tui`, so the two branches can merge independently).

## Global Constraints

Copied from CLAUDE.md — every task's requirements implicitly include these:

- **Move aside, never hard-delete.** Files may only ever be MOVED to the recycling bin; permanent deletion is a separate explicit user action.
- **Only touch a tool's OWN known files** on the tool's own initiative — the allow-list lives in the tool's Profile (this plan widens the *mechanism* from `codex.LogFileNames` to per-tool Profiles; each profile's list must stay narrow and explicit). NEVER user project files.
- **When uncertain, report — never resolve.** The shallow map's golden rule: finding a connection is trustworthy ("hands off"); finding nothing is NOT proof of safety and never authorizes action.
- **Stay low-write.** No SQLite/databases for CodexSSD's own storage — JSONL only. Behavioral detection writes one line per new-entry event, never one per poll.
- **Check before touching.** `clean`/`restore` refuse if the target tool's process is running.
- **`internal/mcpserver` stays read-only with exactly five tools** — this plan does not touch it.
- No third-party deps outside `internal/tui`. Pure functions take inputs explicitly (`Scan(dir)`, injected `now`) so tests use `t.TempDir()`. Human sizes via `codex.HumanBytes` (binary units). Plain-language output. Comment the *why*, especially safety intent.
- **Config can never brick the tool** — warn and carry on with defaults.
- Before claiming any task done: `go build ./... && go vet ./... && go test ./...` and `gofmt -l .` must be clean.
- Backward compatibility: with no `--tool` flag, every command's behavior, output, and JSON shape must be byte-identical to today (existing tests in `cmd/codexssd` and `internal/*` must pass unmodified, except where a task explicitly says it updates a test).

## Deliberately OUT of this plan (follow-up plans, per YAGNI)

- Deep relationship map (Phase 4's fuller map) — Phase 3's shallow map only.
- Cursor / Gemini CLI profiles — the Profile abstraction makes each a ~30-line follow-up once its layout is researched; don't guess layouts now.
- `watch --tool claude` — the risk engine is WAL/SQLite-shaped; generalizing thresholds is its own design job. `watch` stays Codex-only (behavioral detection hooks into the existing Codex watch loop).
- TUI multi-tool support; MCP `tool` parameter; cost/token awareness; daily summaries; menu-bar app; editor plugin; team settings.

---

### Task 1: `internal/tool` — Profile type, Codex profile, registry

**Files:**
- Create: `internal/tool/profile.go`
- Test: `internal/tool/profile_test.go`

**Interfaces:**
- Consumes: `os.UserHomeDir`. **Deliberately does NOT import `internal/codex`** — Task 3 makes `codex` import `tool` (codex becomes the delegating shim), so any `tool → codex` import, including from an in-package test file, would be a cycle. The Codex allow-list values are written as literals here; Task 3 re-points `codex.LogFileNames` at this profile so there is still one source of truth.
- Produces (later tasks rely on these exact names):
  - `type Profile struct { Name, DisplayName, DirName string; OwnFixedFiles, OwnStaleGlobs, NeverTouch, ProcessNames, ProcessHints []string }`
  - `const BackupDirName = "codexssd-backups"` (moves here from `cleaner`; Task 5 re-points `cleaner.BackupDirName` at it)
  - `func Codex() Profile`, `func All() []Profile`, `func ByName(name string) (Profile, error)`, `func (p Profile) Dir() (string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/tool/profile_test.go
package tool

import (
	"path/filepath"
	"strings"
	"testing"
)

// NOTE: this test intentionally does not import internal/codex (see the
// Interfaces note — codex will import tool from Task 3 onward). The expected
// literals below ARE the documented Codex allow-list.
func TestCodexProfileValues(t *testing.T) {
	p := Codex()
	if p.Name != "codex" || p.DisplayName != "Codex" || p.DirName != ".codex" {
		t.Errorf("Codex() = %+v, want name codex / display Codex / dir .codex", p)
	}
	want := []string{"logs_2.sqlite", "logs_2.sqlite-wal", "logs_2.sqlite-shm"}
	if len(p.OwnFixedFiles) != len(want) {
		t.Fatalf("OwnFixedFiles = %v, want %v", p.OwnFixedFiles, want)
	}
	for i, name := range want {
		if p.OwnFixedFiles[i] != name {
			t.Errorf("OwnFixedFiles[%d] = %q, want %q", i, p.OwnFixedFiles[i], name)
		}
	}
}

func TestByName(t *testing.T) {
	if _, err := ByName("codex"); err != nil {
		t.Errorf("ByName(codex) error = %v, want nil", err)
	}
	if _, err := ByName("clippy"); err == nil || !strings.Contains(err.Error(), "clippy") {
		t.Errorf("ByName(clippy) error = %v, want unknown-tool error naming clippy", err)
	}
}

func TestDirIsUnderHome(t *testing.T) {
	dir, err := Codex().Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if filepath.Base(dir) != ".codex" {
		t.Errorf("Dir() = %q, want basename .codex", dir)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -v`
Expected: FAIL to build — `undefined: Codex`, `undefined: ByName`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/tool/profile.go
// Package tool defines per-tool profiles: where each supported AI coding tool
// keeps its local data, and which of its OWN files codexssd may ever act on
// autonomously.
//
// SAFETY: a Profile's allow-list (OwnFixedFiles + OwnStaleGlobs) is the ONLY
// set of files codexssd may move aside for that tool, and NeverTouch prefixes
// win over any allow-list match. Widening a profile's list is a product
// decision, never a convenience.
package tool

import (
	"fmt"
	"os"
	"path/filepath"
)

// BackupDirName is the recycling-bin root created inside each tool's directory.
// It lives here (not in cleaner) so profiles can exclude it from scans without
// an import cycle; cleaner re-exports it.
const BackupDirName = "codexssd-backups"

// Profile describes one supported AI coding tool.
type Profile struct {
	Name          string   // CLI id, e.g. "codex"
	DisplayName   string   // human name, e.g. "Codex"
	DirName       string   // data dir under $HOME, e.g. ".codex"
	OwnFixedFiles []string // dir-relative files cleanable regardless of age
	OwnStaleGlobs []string // dir-relative globs cleanable only when stale
	NeverTouch    []string // dir-relative prefixes that are NEVER cleanable
	ProcessNames  []string // executable base names identifying the tool
	ProcessHints  []string // command-line substrings identifying the tool
}

// Codex is the founding profile. Its OwnFixedFiles ARE the canonical Codex
// allow-list from Phase 1 — internal/codex re-exports codex.LogFileNames from
// here (from Task 3 onward), so this stays the single source of truth. The
// values are literals because codex imports tool, never the reverse.
func Codex() Profile {
	return Profile{
		Name:          "codex",
		DisplayName:   "Codex",
		DirName:       ".codex", // must match codex.DirName
		OwnFixedFiles: []string{"logs_2.sqlite", "logs_2.sqlite-wal", "logs_2.sqlite-shm"},
		ProcessNames:  []string{"codex"},
		ProcessHints:  []string{"codex app-server", "codex desktop"},
	}
}

// All lists every supported profile. (Claude Code is added in a later task.)
func All() []Profile { return []Profile{Codex()} }

// ByName resolves a CLI --tool value to a profile.
func ByName(name string) (Profile, error) {
	for _, p := range All() {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("unknown tool %q (supported: codex)", name)
}

// Dir returns the tool's data directory under the user's home. Like codex.Dir,
// it does not check existence — callers decide how to handle a missing dir.
func (p Profile) Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p.DirName), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/tool/profile.go internal/tool/profile_test.go
git commit -m "feat(tool): per-tool Profile type with Codex as founding profile"
```

---

### Task 2: `Profile.CleanablePaths` + `Profile.Allows` — the generalized allow-list gate

**Files:**
- Create: `internal/tool/scan.go`
- Test: `internal/tool/scan_test.go`

**Interfaces:**
- Consumes: `Profile` from Task 1.
- Produces:
  - `type FoundFile struct { Rel string; Path string; Size int64 }` (`Rel` is dir-relative, slash-separated)
  - `func (p Profile) CleanablePaths(dir string, now time.Time, staleAfter time.Duration) []FoundFile`
  - `func (p Profile) Allows(dir, path string) bool` — the safety gate Task 5's cleaner re-checks against

- [ ] **Step 1: Write the failing test**

```go
// internal/tool/scan_test.go
package tool

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAged(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := now.Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func testProfile() Profile {
	return Profile{
		Name:          "testtool",
		DirName:       ".testtool",
		OwnFixedFiles: []string{"logs.db"},
		OwnStaleGlobs: []string{"projects/*/*.jsonl"},
		NeverTouch:    []string{"memory"},
	}
}

func TestCleanablePathsFixedAndStaleGlobs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := 30 * 24 * time.Hour
	writeAged(t, filepath.Join(dir, "logs.db"), time.Minute, now)                       // fixed: age-exempt
	writeAged(t, filepath.Join(dir, "projects", "p1", "old.jsonl"), 40*24*time.Hour, now) // stale: cleanable
	writeAged(t, filepath.Join(dir, "projects", "p1", "new.jsonl"), time.Hour, now)       // fresh: NOT cleanable
	writeAged(t, filepath.Join(dir, "secrets.txt"), 90*24*time.Hour, now)                 // unlisted: NOT cleanable

	got := testProfile().CleanablePaths(dir, now, stale)

	rels := map[string]bool{}
	for _, f := range got {
		rels[f.Rel] = true
	}
	if len(got) != 2 || !rels["logs.db"] || !rels["projects/p1/old.jsonl"] {
		t.Errorf("CleanablePaths = %v, want exactly logs.db + projects/p1/old.jsonl", got)
	}
}

func TestCleanablePathsNeverTouchWins(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	p := testProfile()
	p.OwnStaleGlobs = append(p.OwnStaleGlobs, "memory/*.md") // even an allow-listed glob…
	writeAged(t, filepath.Join(dir, "memory", "fact.md"), 90*24*time.Hour, now)

	if got := p.CleanablePaths(dir, now, 24*time.Hour); len(got) != 0 {
		t.Errorf("CleanablePaths returned %v from a NeverTouch prefix; want none", got)
	}
}

func TestCleanablePathsSkipsRecyclingBin(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	p := Profile{Name: "t", OwnStaleGlobs: []string{"*/manifest.json"}}
	writeAged(t, filepath.Join(dir, BackupDirName, "manifest.json"), 90*24*time.Hour, now)

	if got := p.CleanablePaths(dir, now, 24*time.Hour); len(got) != 0 {
		t.Errorf("CleanablePaths returned %v from the recycling bin; want none", got)
	}
}

func TestAllows(t *testing.T) {
	dir := t.TempDir()
	p := testProfile()
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(dir, "logs.db"), true},
		{filepath.Join(dir, "projects", "p1", "a.jsonl"), true}, // staleness is a clean-time gate, not re-checked here
		{filepath.Join(dir, "memory", "fact.md"), false},
		{filepath.Join(dir, "secrets.txt"), false},
		{filepath.Join(dir, "..", "outside.jsonl"), false},
		{"/somewhere/else/logs.db", false},
	}
	for _, c := range cases {
		if got := p.Allows(dir, c.path); got != c.want {
			t.Errorf("Allows(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -v`
Expected: FAIL to build — `undefined: CleanablePaths` / `p.Allows undefined`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/tool/scan.go
package tool

import (
	"os"
	"path/filepath"
	slashpath "path"
	"strings"
	"time"
)

// FoundFile is one file a profile may act on right now.
type FoundFile struct {
	Rel  string // dir-relative, slash-separated (becomes the backup item name)
	Path string
	Size int64
}

// CleanablePaths returns the files under dir this profile may move aside NOW:
// every existing fixed file, plus every stale-glob match at least staleAfter
// old. NeverTouch prefixes and the recycling bin always win.
//
// SAFETY: read-only (Stat/Glob only). The stale gate exists because glob-listed
// files (e.g. session transcripts) may still be wanted by the tool while fresh;
// fixed files (Codex's runaway logs) are cleanable at any age by design.
func (p Profile) CleanablePaths(dir string, now time.Time, staleAfter time.Duration) []FoundFile {
	var out []FoundFile
	add := func(path string, info os.FileInfo) {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if p.offLimits(rel) {
			return
		}
		out = append(out, FoundFile{Rel: rel, Path: path, Size: info.Size()})
	}

	for _, name := range p.OwnFixedFiles {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			add(path, info)
		}
	}
	for _, g := range p.OwnStaleGlobs {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(g)))
		if err != nil {
			continue // a malformed pattern must never brick a command
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}
			if now.Sub(info.ModTime()) < staleAfter {
				continue
			}
			add(m, info)
		}
	}
	return out
}

// Allows is the safety gate the cleaner re-checks every move against: path must
// resolve inside dir and match the allow-list. Staleness is NOT re-checked here
// — it gates what gets planned, while Allows also validates restores of files
// that were already moved aside.
func (p Profile) Allows(dir, path string) bool {
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || p.offLimits(rel) {
		return false
	}
	for _, name := range p.OwnFixedFiles {
		if rel == name {
			return true
		}
	}
	for _, g := range p.OwnStaleGlobs {
		if ok, _ := slashpath.Match(g, rel); ok {
			return true
		}
	}
	return false
}

// offLimits reports whether rel is under a NeverTouch prefix or the bin.
func (p Profile) offLimits(rel string) bool {
	for _, nt := range append([]string{BackupDirName}, p.NeverTouch...) {
		if rel == nt || strings.HasPrefix(rel, nt+"/") {
			return true
		}
	}
	return false
}
```

Note: `slashpath.Match` matches `*` per path segment, so `projects/*/*.jsonl` works on the slash-normalized rel; `filepath.Glob` handles the on-disk side. Both treat `*` as non-separator-crossing, keeping the allow-list one directory level deep by construction.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -v`
Expected: PASS (all tests, including Task 1's)

- [ ] **Step 5: Commit**

```bash
git add internal/tool/scan.go internal/tool/scan_test.go
git commit -m "feat(tool): CleanablePaths scan and Allows safety gate per profile"
```

---

### Task 3: Generic process detection; `internal/codex` delegates

**Files:**
- Create: `internal/tool/process.go`
- Test: `internal/tool/process_test.go`
- Modify: `internal/codex/process.go` (delegate, keep public API)

**Interfaces:**
- Consumes: `Profile` (Task 1). Reuses the parsing approach from `internal/codex/process.go:69-88`.
- Produces:
  - `type Process struct { PID int; Name string; Command string }` (same JSON tags as `codex.Process`)
  - `func DetectProcesses(p Profile) ([]Process, error)`, `func IsRunning(p Profile) (bool, error)`
  - `var ErrUnsupportedPlatform = errors.New(...)` (codex re-exports its existing one by assignment)
- Existing `codex.DetectProcesses` / `codex.IsCodexRunning` keep working unchanged (their tests in `internal/codex/process_test.go` must pass unmodified).

- [ ] **Step 1: Write the failing test**

```go
// internal/tool/process_test.go
package tool

import "testing"

func TestMatchesProfile(t *testing.T) {
	p := Profile{Name: "claude", ProcessNames: []string{"claude"}}
	cases := []struct {
		command string
		want    bool
	}{
		{"/usr/local/bin/claude --resume", true},
		{"node /Users/x/.nvm/bin/claude", true},                 // node runner containing the tool name
		{"/usr/local/bin/codexssd watch", false},                // never match ourselves
		{"vim /Users/x/notes/claude-ideas.md", false},           // mentioning the name is not running it
		{"", false},
	}
	for _, c := range cases {
		got := matchesProfile(p, Process{Command: c.command})
		if got != c.want {
			t.Errorf("matchesProfile(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestMatchesProfileHints(t *testing.T) {
	p := Codex()
	if !matchesProfile(p, Process{Command: "/opt/thing codex app-server --port 1"}) {
		t.Error("command hint 'codex app-server' should match the Codex profile")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -v`
Expected: FAIL to build — `undefined: matchesProfile`, `undefined: Process`

- [ ] **Step 3: Write minimal implementation**

`internal/tool/process.go` — move the body of `codex.DetectProcesses`, `parseProcesses`, and the matcher here, generalized:

```go
// internal/tool/process.go
package tool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrUnsupportedPlatform: process detection unavailable (Windows). Callers must
// treat this as "cannot verify the tool is stopped" and refuse to act.
var ErrUnsupportedPlatform = errors.New("process detection not supported on this platform")

// Process is a read-only snapshot of a running process.
type Process struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// DetectProcesses returns running processes that look like the profiled tool.
// SAFETY: observation only — never signals or alters a process; excludes self.
func DetectProcesses(p Profile) ([]Process, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrUnsupportedPlatform
	}
	out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var matched []Process
	for _, proc := range parseProcesses(string(out)) {
		if proc.PID == self {
			continue
		}
		if matchesProfile(p, proc) {
			matched = append(matched, proc)
		}
	}
	return matched, nil
}

// IsRunning reports whether any process matching the profile is running.
func IsRunning(p Profile) (bool, error) {
	procs, err := DetectProcesses(p)
	if err != nil {
		return false, err
	}
	return len(procs) > 0, nil
}

// parseProcesses turns `ps -axo pid=,args=` output into Processes.
func parseProcesses(psOutput string) []Process {
	var procs []Process
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		name := filepath.Base(strings.Fields(command)[0])
		procs = append(procs, Process{PID: pid, Name: name, Command: command})
	}
	return procs
}

// matchesProfile: base-name match, hint substring match, or a node runner whose
// command line contains the tool name (Codex and Claude Code both ship as node
// apps). Never matches codexssd itself.
func matchesProfile(p Profile, proc Process) bool {
	fields := strings.Fields(proc.Command)
	if len(fields) == 0 {
		return false
	}
	base := filepath.Base(fields[0])
	if base == "codexssd" {
		return false
	}
	for _, n := range p.ProcessNames {
		if base == n {
			return true
		}
	}
	for _, h := range p.ProcessHints {
		if strings.Contains(proc.Command, h) {
			return true
		}
	}
	return base == "node" && strings.Contains(proc.Command, p.Name)
}
```

Then shrink `internal/codex/process.go` to a delegating shim (keep `Process`, `ErrUnsupportedPlatform`, `DetectProcesses`, `IsCodexRunning` public — everything else deleted):

```go
// internal/codex/process.go  (replaces the whole file)
package codex

import "github.com/0xdefence/codexssd/internal/tool"

// ErrUnsupportedPlatform mirrors tool.ErrUnsupportedPlatform (same sentinel, so
// errors.Is works across both packages).
var ErrUnsupportedPlatform = tool.ErrUnsupportedPlatform

// Process is a read-only snapshot of a running process.
type Process = tool.Process

// DetectProcesses returns running processes that look like Codex.
func DetectProcesses() ([]Process, error) { return tool.DetectProcesses(tool.Codex()) }

// IsCodexRunning reports whether any Codex-like process is currently running.
func IsCodexRunning() (bool, error) { return tool.IsRunning(tool.Codex()) }
```

**Import direction:** this task makes `codex` import `tool` — legal because `tool` never imports `codex` (Task 1 used literals for exactly this reason). Also re-point the canonical list in `internal/codex/logs.go` at the profile so there is still ONE source of truth:

```go
// LogFileNames are Codex's OWN local SQLite log files, relative to ~/.codex.
// The canonical definition now lives in the Codex tool profile; this alias
// keeps existing callers and the documented safety rule pointing at one list.
var LogFileNames = tool.Codex().OwnFixedFiles
```

and `internal/codex/paths.go`: `const DirName = ".codex"` stays as-is (add a comment `// must match tool.Codex().DirName`).

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/tool/ ./internal/codex/ -v`
Expected: PASS — including the untouched `internal/codex/process_test.go`

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS (no other package touched process internals)

- [ ] **Step 6: Commit**

```bash
git add internal/tool/ internal/codex/
git commit -m "refactor(process): generic per-profile detection in internal/tool; codex delegates"
```

---

### Task 4: The Claude Code profile

**Files:**
- Modify: `internal/tool/profile.go` (add `Claude()`, register in `All()`, widen `ByName` error text)
- Test: `internal/tool/claude_test.go`

**Interfaces:**
- Produces: `func Claude() Profile` with `Name: "claude"`, `DisplayName: "Claude Code"`, `DirName: ".claude"`.

**Layout facts this encodes** (verified on a real `~/.claude`): session transcripts live at `projects/<encoded-project-path>/<session-id>.jsonl` (the bulk of the footprint); `shell-snapshots/` holds regenerable shell state; `memory/`, `settings.json`, `settings.local.json`, `CLAUDE.md`, `plugins/`, `agents/`, `commands/`, `skills/`, `hooks/`, `todos/`, `keybindings.json` are small and load-bearing — never cleanable. Cleaning a transcript breaks `claude --resume` for that session, which is why transcripts are stale-gated, never fixed-listed.

- [ ] **Step 1: Write the failing test**

```go
// internal/tool/claude_test.go
package tool

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProfileRegistered(t *testing.T) {
	p, err := ByName("claude")
	if err != nil {
		t.Fatalf("ByName(claude) error = %v", err)
	}
	if p.DisplayName != "Claude Code" || p.DirName != ".claude" {
		t.Errorf("Claude profile = %+v, want display 'Claude Code', dir '.claude'", p)
	}
	if len(p.OwnFixedFiles) != 0 {
		t.Errorf("Claude has fixed files %v; transcripts must be stale-gated, never age-exempt", p.OwnFixedFiles)
	}
}

func TestClaudeCleanablePicksOnlyStaleTranscriptsAndSnapshots(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := 30 * 24 * time.Hour
	old := 40 * 24 * time.Hour

	writeAged(t, filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl"), old, now)      // cleanable
	writeAged(t, filepath.Join(dir, "projects", "-Users-jo-app", "s2.jsonl"), time.Hour, now) // fresh → keep
	writeAged(t, filepath.Join(dir, "shell-snapshots", "snap.sh"), old, now)                  // cleanable
	writeAged(t, filepath.Join(dir, "memory", "MEMORY.md"), old, now)                         // never
	writeAged(t, filepath.Join(dir, "settings.json"), old, now)                               // never
	writeAged(t, filepath.Join(dir, "todos", "t.json"), old, now)                             // never
	writeAged(t, filepath.Join(dir, "CLAUDE.md"), old, now)                                   // never

	got := Claude().CleanablePaths(dir, now, stale)
	rels := map[string]bool{}
	for _, f := range got {
		rels[f.Rel] = true
	}
	if len(got) != 2 || !rels["projects/-Users-jo-app/s1.jsonl"] || !rels["shell-snapshots/snap.sh"] {
		t.Errorf("CleanablePaths = %v, want exactly the stale transcript + snapshot", got)
	}
}

func TestClaudeAllowsNeverTouchesMemory(t *testing.T) {
	dir := t.TempDir()
	if Claude().Allows(dir, filepath.Join(dir, "memory", "fact.md")) {
		t.Error("Allows must reject memory/ even hypothetically")
	}
	if !Claude().Allows(dir, filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl")) {
		t.Error("Allows must accept a projects transcript")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -v`
Expected: FAIL to build — `undefined: Claude`

- [ ] **Step 3: Write minimal implementation** (append to `internal/tool/profile.go`)

```go
// Claude is the Claude Code profile. Its own recoverable data is session
// transcripts (projects/<slug>/<id>.jsonl) and shell snapshots — and ONLY when
// stale, because cleaning a transcript breaks `claude --resume` for that
// session. Everything load-bearing (memory, settings, plugins, todos, skills)
// is NeverTouch: small, valuable, and not clutter.
func Claude() Profile {
	return Profile{
		Name:          "claude",
		DisplayName:   "Claude Code",
		DirName:       ".claude",
		OwnStaleGlobs: []string{"projects/*/*.jsonl", "shell-snapshots/*"},
		NeverTouch: []string{
			"memory", "settings.json", "settings.local.json", "CLAUDE.md",
			"plugins", "agents", "commands", "skills", "hooks", "todos",
			"keybindings.json",
		},
		ProcessNames: []string{"claude"},
	}
}
```

And update the registry + error text in the same file:

```go
func All() []Profile { return []Profile{Codex(), Claude()} }
```

```go
	return Profile{}, fmt.Errorf("unknown tool %q (supported: codex, claude)", name)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -v`
Expected: PASS. Also fix Task 1's `TestByName` if its "clippy" assertion now sees the new error text — it checks only that the error names "clippy", so it still passes.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/
git commit -m "feat(tool): Claude Code profile — stale-gated transcripts, load-bearing files never touched"
```

---

### Task 5: Generalize the cleaner — per-tool plans, nested paths, tool-tagged manifests

**Files:**
- Modify: `internal/cleaner/clean.go` (add `Plan.Tool`, `PlanTool`; re-point `BackupDirName`; delete `isCodexLog`)
- Modify: `internal/cleaner/apply.go` (gate via `Profile.Allows`; nested targets; `Manifest.Tool`)
- Modify: `internal/cleaner/restore.go` (gate via `Profile.Allows`; nested sources; recreate parent dirs; remove empty subdirs)
- Test: `internal/cleaner/tool_test.go` (new); existing cleaner tests must pass unmodified.

**Interfaces:**
- Consumes: `tool.Profile`, `tool.ByName`, `(Profile).Allows`, `(Profile).CleanablePaths`, `tool.BackupDirName`.
- Produces:
  - `Plan` gains `Tool string \`json:"tool,omitempty"\``; `Manifest` gains the same field. Empty string means "codex" (pre-multi-tool manifests stay restorable).
  - `func PlanTool(p tool.Profile, toolDir string, now time.Time, staleAfter time.Duration) (Plan, error)`
  - `PlanItem.Name` is now the dir-relative slash path (for Codex fixed files this is identical to the old base name, so old manifests/JSON are unaffected).
  - `PlanCodexLogs(codexDir)` becomes `return PlanTool(tool.Codex(), codexDir, time.Time{}, 0)` — signature unchanged.
  - `BackupDirName` re-exported: `const BackupDirName = tool.BackupDirName` (visibility keeps compiling untouched).

- [ ] **Step 1: Write the failing test**

```go
// internal/cleaner/tool_test.go
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/tool"
)

func writeAged(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := now.Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCleanRestoreRoundTripNestedPaths(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	transcript := filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl")
	writeAged(t, transcript, 40*24*time.Hour, now)

	plan, err := PlanTool(tool.Claude(), dir, now, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Name != "projects/-Users-jo-app/s1.jsonl" {
		t.Fatalf("plan items = %+v, want the nested transcript by relative name", plan.Items)
	}
	if plan.Tool != "claude" {
		t.Errorf("plan.Tool = %q, want claude", plan.Tool)
	}

	backupDir, err := plan.Apply(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Error("transcript still at original path after Apply; want moved aside")
	}
	movedTo := filepath.Join(backupDir, "projects", "-Users-jo-app", "s1.jsonl")
	if _, err := os.Stat(movedTo); err != nil {
		t.Errorf("moved transcript not found at %s: %v", movedTo, err)
	}

	if err := Restore(backupDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("transcript not restored to original path: %v", err)
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Error("backup dir should be gone after a full restore")
	}
}

func TestApplyRefusesFileOutsideProfileAllowList(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	forbidden := filepath.Join(dir, "memory", "MEMORY.md")
	writeAged(t, forbidden, 90*24*time.Hour, now)

	plan := Plan{
		Tool:       "claude",
		CodexDir:   dir,
		BackupRoot: filepath.Join(dir, BackupDirName),
		Items:      []PlanItem{{Name: "memory/MEMORY.md", Path: forbidden, Size: 4}},
	}
	if _, err := plan.Apply(now); err == nil {
		t.Fatal("Apply moved a NeverTouch file; want refusal")
	}
	if _, err := os.Stat(forbidden); err != nil {
		t.Errorf("forbidden file was disturbed: %v", err)
	}
}

func TestManifestWithoutToolFieldRestoresAsCodex(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeAged(t, filepath.Join(dir, "logs_2.sqlite"), time.Hour, now)

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatal(err)
	}
	backupDir, err := plan.Apply(now)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-multi-tool manifest: strip the tool field.
	m, err := readManifest(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	m.Tool = ""
	if err := writeManifest(backupDir, m); err != nil {
		t.Fatal(err)
	}
	if err := Restore(backupDir); err != nil {
		t.Fatalf("Restore of legacy manifest failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleaner/ -v`
Expected: FAIL to build — `undefined: PlanTool`, `plan.Tool undefined`

- [ ] **Step 3: Implement**

`internal/cleaner/clean.go` — replace the `BackupDirName` const, `Plan`, `PlanCodexLogs`, and delete `isCodexLog`:

```go
// BackupDirName is the recycling-bin root, created inside each tool's data dir
// so moves stay atomic renames on one filesystem. Canonical value lives in the
// tool package (profiles must exclude it from scans without an import cycle).
const BackupDirName = tool.BackupDirName

// Plan is a read-only description of what clean WOULD move aside.
type Plan struct {
	Tool       string     `json:"tool,omitempty"` // "" means codex (pre-multi-tool plans)
	CodexDir   string     `json:"codex_dir"`      // the tool's data dir; JSON name kept for compatibility
	BackupRoot string     `json:"backup_root"`
	Items      []PlanItem `json:"items"`
	TotalBytes int64      `json:"total_bytes"`
}

// PlanTool inspects toolDir and returns a move-aside plan for the profile's own
// files. staleAfter gates glob-listed files; fixed files always qualify.
// SAFETY: read-only; items come exclusively from Profile.CleanablePaths.
func PlanTool(p tool.Profile, toolDir string, now time.Time, staleAfter time.Duration) (Plan, error) {
	plan := Plan{
		Tool:       p.Name,
		CodexDir:   toolDir,
		BackupRoot: filepath.Join(toolDir, BackupDirName),
	}
	for _, f := range p.CleanablePaths(toolDir, now, staleAfter) {
		plan.Items = append(plan.Items, PlanItem{Name: f.Rel, Path: f.Path, Size: f.Size})
		plan.TotalBytes += f.Size
	}
	return plan, nil
}

// PlanCodexLogs is the Phase-1 entry point, unchanged for existing callers.
// Codex's fixed logs ignore staleness, so now/staleAfter are zero.
func PlanCodexLogs(codexDir string) (Plan, error) {
	return PlanTool(tool.Codex(), codexDir, time.Time{}, 0)
}

// profileFor resolves a stored tool name; empty means codex (legacy manifests).
func profileFor(name string) (tool.Profile, error) {
	if name == "" {
		name = "codex"
	}
	return tool.ByName(name)
}
```

(Imports become `"path/filepath"` + `"time"` + the tool package; the codex import goes away.)

`internal/cleaner/apply.go` — in `ApplyWithHold`, replace the gate loop and the move loop, and tag the manifest:

```go
	prof, err := profileFor(p.Tool)
	if err != nil {
		return "", err
	}
	toolDir := filepath.Dir(p.BackupRoot)
	for _, it := range p.Items {
		if !prof.Allows(toolDir, it.Path) {
			return "", fmt.Errorf("refusing to move file outside %s's own-file allow-list: %s", prof.DisplayName, it.Path)
		}
	}
```

```go
	manifest := Manifest{
		Tool:      p.Tool,
		MovedAt:   now,
		HoldUntil: now.Add(hold),
	}
```

```go
	for _, it := range p.Items {
		target := filepath.Join(dest, filepath.FromSlash(it.Name))
		// Nested own-files (e.g. Claude transcripts) keep their relative
		// structure inside the backup so restore is a pure mirror image.
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			rollback()
			return "", err
		}
		if err := os.Rename(it.Path, target); err != nil {
			rollback()
			return "", fmt.Errorf("moving %s: %w", it.Name, err)
		}
		moved = append(moved, [2]string{it.Path, target})
		manifest.Items = append(manifest.Items, ManifestItem{
			Name:         it.Name,
			OriginalPath: it.Path,
			Size:         it.Size,
		})
	}
```

And the `Manifest` definition becomes:

```go
// Manifest is the receipt written into each backup directory.
type Manifest struct {
	Tool      string         `json:"tool,omitempty"` // "" means codex (legacy)
	MovedAt   time.Time      `json:"moved_at"`
	HoldUntil time.Time      `json:"hold_until"`
	Items     []ManifestItem `json:"items"`
}
```

`internal/cleaner/restore.go` — `Restore` becomes:

```go
// Restore moves every file in backupDir's manifest back to its original path,
// then removes the emptied backup directory.
//
// SAFETY: renames only; every path re-validated against the owning tool's
// allow-list; never overwrites an existing file; rolls back on partial failure.
func Restore(backupDir string) error {
	m, err := readManifest(backupDir)
	if err != nil {
		return err
	}
	prof, err := profileFor(m.Tool)
	if err != nil {
		return err
	}
	// The bin lives at <toolDir>/codexssd-backups/<timestamp>.
	toolDir := filepath.Dir(filepath.Dir(backupDir))
	for _, it := range m.Items {
		if !prof.Allows(toolDir, it.OriginalPath) {
			return fmt.Errorf("refusing to restore file outside %s's own-file allow-list: %s", prof.DisplayName, it.OriginalPath)
		}
		if _, err := os.Stat(it.OriginalPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", it.OriginalPath)
		}
	}

	var moved [][2]string // (from, to) for rollback
	for _, it := range m.Items {
		src := filepath.Join(backupDir, filepath.FromSlash(it.Name))
		// The original parent may have been tidied away since the move.
		if err := os.MkdirAll(filepath.Dir(it.OriginalPath), 0o700); err != nil {
			return fmt.Errorf("restoring %s: %w", it.Name, err)
		}
		if err := os.Rename(src, it.OriginalPath); err != nil {
			for _, mv := range moved {
				_ = os.Rename(mv[1], mv[0])
			}
			return fmt.Errorf("restoring %s: %w", it.Name, err)
		}
		moved = append(moved, [2]string{src, it.OriginalPath})
	}

	// Tidy the emptied backup: manifest, then now-empty dirs bottom-up.
	// os.Remove refuses non-empty dirs, so an unexpectedly present file makes
	// this a no-op rather than a delete — deliberately NOT os.RemoveAll.
	_ = os.Remove(filepath.Join(backupDir, manifestName))
	removeEmptyDirs(backupDir)
	return nil
}

// removeEmptyDirs removes dir and any now-empty subdirectories, deepest first.
func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			removeEmptyDirs(filepath.Join(dir, e.Name()))
		}
	}
	_ = os.Remove(dir) // fails (harmlessly) if anything remains
}
```

- [ ] **Step 4: Run cleaner tests, then everything**

Run: `go test ./internal/cleaner/ -v && go test ./...`
Expected: PASS — new tests plus all existing cleaner/visibility/cmd tests unmodified.

- [ ] **Step 5: Commit**

```bash
git add internal/cleaner/
git commit -m "feat(cleaner): per-tool plans and manifests; nested own-file paths survive the bin round-trip"
```

---

### Task 6: CLI wiring — `--tool` on status, report, clean, restore, prune

**Files:**
- Modify: `cmd/codexssd/main.go` (flag + resolver + per-command wiring + usage text)
- Test: `cmd/codexssd/tool_flag_test.go` (new); existing `main_test.go` / `integration_test.go` pass unmodified.

**Interfaces:**
- Consumes: `tool.ByName`, `(Profile).Dir()`, `tool.IsRunning`, `cleaner.PlanTool`, `config.StaleAfter()`.
- Produces: every listed command accepts `--tool codex|claude` (default `codex`). With the default, output is byte-identical to today. Recorder receipts and human/JSON output for non-codex tools name the tool.

- [ ] **Step 1: Write the failing test**

The `run(args []string) int` entry point is already testable (see `main_test.go`). Test through it with `$HOME` redirected:

```go
// cmd/codexssd/tool_flag_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempHome points $HOME at a temp dir so ~/.claude and ~/.codex resolve
// inside the test sandbox.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeAgedFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestStatusRejectsUnknownTool(t *testing.T) {
	withTempHome(t)
	if code := run([]string{"status", "--tool", "clippy"}); code != 2 {
		t.Errorf("status --tool clippy exit = %d, want 2", code)
	}
}

func TestStatusClaudeRunsCleanly(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl"), 40*24*time.Hour)
	if code := run([]string{"status", "--tool", "claude"}); code != 0 {
		t.Errorf("status --tool claude exit = %d, want 0", code)
	}
}

func TestCleanClaudeDryRunTouchesNothing(t *testing.T) {
	home := withTempHome(t)
	transcript := filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl")
	writeAgedFile(t, transcript, 40*24*time.Hour)
	if code := run([]string{"clean", "--tool", "claude"}); code != 0 {
		t.Errorf("clean --tool claude (dry run) exit = %d, want 0", code)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("dry run moved the transcript: %v", err)
	}
}
```

Note: if `main_test.go` already defines a temp-home helper, reuse it instead of `withTempHome` — read that file first and match its idiom.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/codexssd/ -run 'TestStatusRejects|TestStatusClaude|TestCleanClaude' -v`
Expected: FAIL — `flag provided but not defined: -tool` (exit code 2 from flag parsing makes `TestStatusRejectsUnknownTool` pass trivially; the other two fail)

- [ ] **Step 3: Implement wiring in `cmd/codexssd/main.go`**

Add one shared resolver:

```go
// resolveTool maps a --tool value to its profile and data dir. Exit-code
// semantics match the rest of main: 2 = bad usage, 1 = environment problem.
func resolveTool(name string) (tool.Profile, string, int) {
	p, err := tool.ByName(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
		return tool.Profile{}, "", 2
	}
	dir, err := p.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return tool.Profile{}, "", 1
	}
	return p, dir, 0
}
```

Per command (pattern shown for `status`; apply the same shape to `report`, `clean`, `restore`, `prune`):

```go
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	toolName := fs.String("tool", "codex", "which AI tool to inspect (codex, claude)")
	// …existing fs.Usage, updated to mention --tool…
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}
	if p.Name == "codex" {
		// Unchanged Phase-1 path: fixed-file report via codex.ScanLogs(dir).
		// (Existing code moves here verbatim.)
	}
	// Glob-profile tools: summarize cleanable vs in-use own files.
	cfg := loadConfigWarned() // the existing warn-and-continue config load helper in main.go; reuse it
	now := time.Now()
	cleanable := p.CleanablePaths(dir, now, cfg.StaleAfter())
	// print: "<DisplayName> directory: <dir>", each cleanable file with
	// codex.HumanBytes, a total, and a plain-language note that fresh
	// session files are deliberately not listed (still in use).
	…
}
```

Command-specific notes (each is small; all reuse `resolveTool`):
- **`report`**: `visibility.Scan(dir, …)` already takes a plain dir — pass the profile's dir; change only the header line to use `p.DisplayName` + dir.
- **`clean`**: build the plan with `cleaner.PlanTool(p, dir, time.Now(), cfg.StaleAfter())` (for codex this yields exactly today's plan, since fixed files ignore the stale gate); replace the `codex.IsCodexRunning()` refusal with `tool.IsRunning(p)` and word the refusal with `p.DisplayName`. Recorder receipt gains the tool name in its action string (e.g. `"clean --tool claude"`) — check `internal/recorder`'s existing receipt fields and extend the action string only, no schema change.
- **`restore`**: backups list/restore already key off the dir — pass profile dir; running-check via `tool.IsRunning(p)`.
- **`prune`**: `cleaner.ReleaseExpired(dir, now)` with the profile dir.
- Update the top-level `usage` string: `--tool` mentioned once under a new line `Most commands accept --tool codex|claude (default codex).`

- [ ] **Step 4: Run the new tests, then the full suite**

Run: `go test ./cmd/codexssd/ -v && go test ./...`
Expected: PASS — including untouched `main_test.go`/`integration_test.go` (default-tool behavior identical).

- [ ] **Step 5: Manual sanity check (read-only)**

Run: `go run ./cmd/codexssd status --tool claude && go run ./cmd/codexssd report --tool claude && go run ./cmd/codexssd clean --tool claude`
Expected: real `~/.claude` report; `clean` prints a dry-run plan of stale transcripts only and touches nothing.

- [ ] **Step 6: Commit**

```bash
git add cmd/codexssd/
git commit -m "feat(cli): --tool flag on status/report/clean/restore/prune; Claude Code end-to-end"
```

---

### Task 7 (Phase 3): `internal/shallowmap` — the shallow connection probe + `report --connections`

**Files:**
- Create: `internal/shallowmap/shallowmap.go`
- Test: `internal/shallowmap/shallowmap_test.go`
- Modify: `cmd/codexssd/main.go` (`cmdReport`: add `--connections` flag + section)

**Interfaces:**
- Consumes: `visibility.Scan` (`internal/visibility/scan.go:40`), `tool.Profile`.
- Produces:
  - `type Connection string`; `const Connected Connection = "connected"`, `const Unknown Connection = "unknown"`
  - `type Result struct { Connection Connection; Evidence string }`
  - `func DecodePath(entryName string) string`
  - `func ProbeClaudeProject(entryName string, statFn func(string) (os.FileInfo, error)) Result`
  - `type ProjectEntry struct { visibility.Entry; DecodedPath string; Connection Connection; Evidence string }`
  - `func ScanClaudeProjects(claudeDir string, now time.Time, staleAfter time.Duration) []ProjectEntry`

- [ ] **Step 1: Write the failing test**

```go
// internal/shallowmap/shallowmap_test.go
package shallowmap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecodePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"-Users-jo-code-myapp", filepath.FromSlash("/Users/jo/code/myapp")},
		{"no-leading-dash", ""}, // not the encoding we know; refuse to guess
		{"", ""},
	}
	for _, c := range cases {
		if got := DecodePath(c.in); got != c.want {
			t.Errorf("DecodePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProbeConnectedWhenProjectExists(t *testing.T) {
	statOK := func(string) (os.FileInfo, error) { return nil, nil }
	r := ProbeClaudeProject("-Users-jo-code-myapp", statOK)
	if r.Connection != Connected || r.Evidence == "" {
		t.Errorf("probe = %+v, want Connected with plain-language evidence", r)
	}
}

func TestProbeUnknownNeverClaimsSafe(t *testing.T) {
	statGone := func(string) (os.FileInfo, error) { return nil, errors.New("not found") }
	r := ProbeClaudeProject("-Users-jo-code-gone", statGone)
	if r.Connection != Unknown {
		t.Errorf("probe of missing project = %+v, want Unknown (never a 'safe' verdict)", r)
	}
	if r.Evidence != "" {
		t.Errorf("Unknown must carry no evidence text (nothing found is not a finding), got %q", r.Evidence)
	}
}

func TestScanClaudeProjects(t *testing.T) {
	claudeDir := t.TempDir()
	proj := filepath.Join(claudeDir, "projects", "-Users-jo-code-myapp")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := ScanClaudeProjects(claudeDir, time.Now(), 30*24*time.Hour)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Name != "-Users-jo-code-myapp" || e.DecodedPath == "" {
		t.Errorf("entry = %+v, want named slug with a decoded path", e)
	}
	// The decoded path does not exist in this sandbox → Unknown, never Connected.
	if e.Connection != Unknown {
		t.Errorf("Connection = %q, want unknown for a nonexistent decoded path", e.Connection)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shallowmap/ -v`
Expected: FAIL to build — package does not exist yet

- [ ] **Step 3: Write minimal implementation**

```go
// internal/shallowmap/shallowmap.go
// Package shallowmap implements Phase 3's deliberately shallow connection
// probe: "does anything obvious point at this entry?".
//
// GOLDEN RULE (from the roadmap): finding a connection is trustworthy — the
// entry is in use, hands off, extra caution. Finding NOTHING is not proof of
// safety; an Unknown entry may only ever be REPORTED, never acted on. This
// package therefore has no verdict meaning "safe to remove" — by design there
// is nowhere for such a verdict to exist.
package shallowmap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdefence/codexssd/internal/visibility"
)

// Connection is the probe's two-value verdict. There is deliberately no third
// "safe" value.
type Connection string

const (
	Connected Connection = "connected" // evidence found: hands off
	Unknown   Connection = "unknown"   // nothing found: still report-only
)

// Result is one probe outcome. Evidence is plain language and only ever set
// when Connected — "we found nothing" is not a finding.
type Result struct {
	Connection Connection `json:"connection"`
	Evidence   string     `json:"evidence,omitempty"`
}

// DecodePath turns a Claude Code projects dir name into its best-guess source
// path: "-Users-jo-code-app" → "/Users/jo/code/app". The encoding is lossy
// (dashes inside real folder names are indistinguishable from separators), so
// the result is only ever a PROBE input, never an action target. Names without
// the leading dash aren't the encoding we know; refuse to guess.
func DecodePath(entryName string) string {
	if !strings.HasPrefix(entryName, "-") {
		return ""
	}
	return filepath.FromSlash(strings.ReplaceAll(entryName, "-", "/"))
}

// ProbeClaudeProject checks whether the project a transcripts folder belongs to
// still exists on disk. statFn is injected for tests (production: os.Stat).
func ProbeClaudeProject(entryName string, statFn func(string) (os.FileInfo, error)) Result {
	decoded := DecodePath(entryName)
	if decoded == "" {
		return Result{Connection: Unknown}
	}
	if _, err := statFn(decoded); err == nil {
		return Result{
			Connection: Connected,
			Evidence:   fmt.Sprintf("its project folder still exists on disk (%s)", decoded),
		}
	}
	return Result{Connection: Unknown}
}

// ProjectEntry is one per-project row for the connections section of `report`.
type ProjectEntry struct {
	visibility.Entry
	DecodedPath string     `json:"decoded_path,omitempty"`
	Connection  Connection `json:"connection"`
	Evidence    string     `json:"evidence,omitempty"`
}

// ScanClaudeProjects sizes each project's transcript folder (read-only, via
// visibility.Scan on the projects dir) and probes its connection.
func ScanClaudeProjects(claudeDir string, now time.Time, staleAfter time.Duration) []ProjectEntry {
	report := visibility.Scan(filepath.Join(claudeDir, "projects"), now, staleAfter)
	var out []ProjectEntry
	for _, e := range report.Entries {
		probe := ProbeClaudeProject(e.Name, os.Stat)
		out = append(out, ProjectEntry{
			Entry:       e,
			DecodedPath: DecodePath(e.Name),
			Connection:  probe.Connection,
			Evidence:    probe.Evidence,
		})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/shallowmap/ -v`
Expected: PASS

- [ ] **Step 5: Wire `report --connections` in `cmd/codexssd/main.go`**

In `cmdReport`, add `connections := fs.Bool("connections", false, "probe whether anything obvious points at each entry (shallow map, read-only)")`. After the normal report output, when `*connections` is set:

- For `--tool claude`: call `shallowmap.ScanClaudeProjects(dir, now, cfg.StaleAfter())` and print a section:

```
Connections (shallow map — read-only):
  -Users-jo-code-myapp     1.2 GiB   connected — its project folder still exists on disk (/Users/jo/code/myapp)
  -Users-jo-old-thing      842 MiB   unknown — nothing obvious points here, but that is NOT proof it's safe; your call
```

- For `--tool codex`: print `Connections: no shallow probe exists for Codex entries yet — everything stays report-only.` (honest about coverage, per the no-silent-caps principle).
- For `--json`, wrap: `out := struct { visibility.Report; Connections []shallowmap.ProjectEntry `json:"connections,omitempty"` }{report, entries}` — the field is omitted entirely without `--connections`, keeping today's JSON byte-identical.

Add a CLI test in `cmd/codexssd/tool_flag_test.go`:

```go
func TestReportConnectionsClaude(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl"), 40*24*time.Hour)
	if code := run([]string{"report", "--tool", "claude", "--connections"}); code != 0 {
		t.Errorf("report --connections exit = %d, want 0", code)
	}
}
```

- [ ] **Step 6: Run the full suite**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS, no vet issues, no unformatted files

- [ ] **Step 7: Commit**

```bash
git add internal/shallowmap/ cmd/codexssd/
git commit -m "feat(shallowmap): Phase 3 shallow connection probe; report --connections"
```

---

### Task 8 (Phase 4): `internal/behavior` — behavioral detection during `watch`

**Files:**
- Create: `internal/behavior/behavior.go`
- Test: `internal/behavior/behavior_test.go`
- Modify: `cmd/codexssd/watch.go` (observe each poll), `cmd/codexssd/main.go` (`cmdReport` annotation)

**Interfaces:**
- Consumes: `recorder.Dir()` (CodexSSD's state dir, `internal/recorder`); the existing watch loop in `cmd/codexssd/watch.go`.
- Produces:
  - `type Event struct { Time time.Time; Tool string; Entry string }` (JSON tags `time`, `tool`, `entry`)
  - `func NewTracker(toolName, provenancePath string, initial []string) *Tracker`
  - `func (t *Tracker) Observe(names []string, agentRunning bool, now time.Time) []Event`
  - `func ProvenancePath() (string, error)` → `<state-dir>/provenance.jsonl`
  - `func Load(path string) ([]Event, error)` — missing file → `(nil, nil)`

- [ ] **Step 1: Write the failing test**

```go
// internal/behavior/behavior_test.go
package behavior

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObserveRecordsOnlyNewEntriesWhileAgentRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.jsonl")
	tr := NewTracker("codex", path, []string{"logs_2.sqlite", "sessions"})
	now := time.Now()

	// Nothing new → no events, no file writes.
	if evs := tr.Observe([]string{"logs_2.sqlite", "sessions"}, true, now); len(evs) != 0 {
		t.Errorf("Observe with no change = %v, want none", evs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("provenance file written with nothing to record; must stay low-write")
	}

	// New entry while the agent runs → exactly one event, one JSONL line.
	evs := tr.Observe([]string{"logs_2.sqlite", "sessions", "cache-v2"}, true, now)
	if len(evs) != 1 || evs[0].Entry != "cache-v2" || evs[0].Tool != "codex" {
		t.Fatalf("Observe = %+v, want one cache-v2 event", evs)
	}
	// Same listing again → already seen, no duplicate.
	if evs := tr.Observe([]string{"logs_2.sqlite", "sessions", "cache-v2"}, true, now); len(evs) != 0 {
		t.Errorf("re-observing same entry = %v, want none", evs)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"cache-v2"`) {
		t.Errorf("provenance = %q, want exactly one line naming cache-v2", string(data))
	}
}

func TestObserveIgnoresAppearancesWhileAgentStopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.jsonl")
	tr := NewTracker("codex", path, []string{"a"})
	// Agent not running: we did NOT watch the agent make this, so it is not
	// evidence — record nothing (but remember it, so it isn't misattributed later).
	if evs := tr.Observe([]string{"a", "b"}, false, time.Now()); len(evs) != 0 {
		t.Errorf("Observe while stopped = %v, want none", evs)
	}
	if evs := tr.Observe([]string{"a", "b"}, true, time.Now()); len(evs) != 0 {
		t.Errorf("entry first seen while stopped later attributed to agent: %v", evs)
	}
}

func TestLoadMissingFileIsNil(t *testing.T) {
	evs, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || evs != nil {
		t.Errorf("Load(missing) = %v, %v; want nil, nil", evs, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/behavior/ -v`
Expected: FAIL to build — package does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// internal/behavior/behavior.go
// Package behavior implements Phase 4's behavioral detection: while `watch`
// runs, notice new top-level entries appearing in the tool's directory and
// record that provenance — "I watched this appear during an agent session" is
// a far stronger signal than guessing by name.
//
// SAFETY: observation only — it never touches the entries it records. It
// appends one small JSONL line per NEW entry (never per poll, never a
// database), in keeping with the low-write promise.
package behavior

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/recorder"
)

// FileName is the provenance file inside CodexSSD's state dir.
const FileName = "provenance.jsonl"

// Event records one entry observed appearing while the agent ran.
type Event struct {
	Time  time.Time `json:"time"`
	Tool  string    `json:"tool"`
	Entry string    `json:"entry"`
}

// Tracker diffs successive directory listings against what it has seen.
type Tracker struct {
	tool string
	path string
	seen map[string]bool
}

// NewTracker starts from the entries present before watching began — those are
// pre-existing, so they are never attributed to the watched session.
func NewTracker(toolName, provenancePath string, initial []string) *Tracker {
	seen := make(map[string]bool, len(initial))
	for _, n := range initial {
		seen[n] = true
	}
	return &Tracker{tool: toolName, path: provenancePath, seen: seen}
}

// Observe records entries appearing for the first time. Only appearances while
// agentRunning become events — an entry first seen while the agent was stopped
// is remembered but never attributed (we did not watch it being made).
// Append failures are swallowed: provenance is best-effort and must never
// disturb the watch loop.
func (t *Tracker) Observe(names []string, agentRunning bool, now time.Time) []Event {
	var events []Event
	for _, n := range names {
		if t.seen[n] {
			continue
		}
		t.seen[n] = true
		if !agentRunning {
			continue
		}
		events = append(events, Event{Time: now, Tool: t.tool, Entry: n})
	}
	if len(events) > 0 {
		_ = appendEvents(t.path, events)
	}
	return events
}

// appendEvents writes one JSONL line per event.
func appendEvents(path string, events []Event) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// ProvenancePath is <state-dir>/provenance.jsonl.
func ProvenancePath() (string, error) {
	dir, err := recorder.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads recorded events; a missing file means nothing recorded (nil, nil).
// Unparseable lines are skipped — a damaged history must not brick `report`.
func Load(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var events []Event
	dec := json.NewDecoder(bytesReader(data))
	for {
		var e Event
		if err := dec.Decode(&e); err != nil {
			break
		}
		events = append(events, e)
	}
	return events, nil
}
```

(`bytesReader` = `bytes.NewReader`; import `"bytes"` and call it directly — the helper name above is only to keep the snippet narrow.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/behavior/ -v`
Expected: PASS

- [ ] **Step 5: Wire into the watch loop and report**

In `cmd/codexssd/watch.go` (read the file first; it owns the poll loop):
- Before the loop: `os.ReadDir(codexDir)` for initial names, `behavior.ProvenancePath()`, `behavior.NewTracker("codex", provPath, names)`. If `ProvenancePath` errors, log one warning and skip tracking — never fail `watch` over provenance.
- Each poll (same place sizes are sampled): re-`ReadDir`, collect names, `tracker.Observe(names, agentRunning, time.Now())` where `agentRunning` comes from the loop's existing process check if it has one, else `tool.IsRunning(tool.Codex())` — read the loop to see which; print one plain line per event: `noticed: "cache-v2" appeared in ~/.codex while Codex was running`.

In `cmdReport` (`cmd/codexssd/main.go`): load `behavior.Load(provPath)` (warn-and-continue on error), build `map[string]bool` of entry names for the current tool, and annotate matching human-output lines with `— appeared during a watched session`. For `--json`, add to the wrapper struct from Task 7: `Provenance []behavior.Event \`json:"provenance,omitempty"\`` (only entries matching the current tool).

Add a watch-side test to `cmd/codexssd/watch_test.go` only if the loop is already testable with an injected dir (read it first); otherwise the behavior package tests plus a report test suffice:

```go
func TestReportShowsProvenance(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".codex", "cache-v2", "f.bin"), time.Hour)
	// Pre-seed provenance in CodexSSD's state dir (~/.codexssd under the temp home).
	prov := filepath.Join(home, ".codexssd", "provenance.jsonl")
	if err := os.MkdirAll(filepath.Dir(prov), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"time":"2026-07-18T10:00:00Z","tool":"codex","entry":"cache-v2"}` + "\n"
	if err := os.WriteFile(prov, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"report"}); code != 0 {
		t.Errorf("report exit = %d, want 0", code)
	}
}
```

(If `recorder.Dir()` is not `~/.codexssd`, read `internal/recorder/jsonl.go` and use its actual path.)

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... && gofmt -l .`
Expected: all PASS / clean

- [ ] **Step 7: Commit**

```bash
git add internal/behavior/ cmd/codexssd/
git commit -m "feat(behavior): record entries appearing during watched sessions; surface in report"
```

---

### Task 9: Documentation sync

**Files:**
- Modify: `README.md`, `CLAUDE.md`, `docs/roadmap.md`, `docs/scope.md`

**Interfaces:** none — prose only, but it must match what shipped exactly.

- [ ] **Step 1: Update CLAUDE.md**

- Safety rule 2: rewrite to name the new mechanism — the per-tool allow-lists now live in `internal/tool` Profiles (`OwnFixedFiles`/`OwnStaleGlobs`, with `NeverTouch` winning); "do not widen a profile casually" replaces the `LogFileNames` wording, and note `codex.LogFileNames` now aliases the Codex profile.
- Current state: add `--tool codex|claude` to status/report/clean/restore/prune; add `report --connections`; describe `internal/shallowmap` (golden rule verbatim: "finding a connection is trustworthy; finding nothing is not") and `internal/behavior` (one JSONL line per appearance, observation only, best-effort).
- Layout block: add `internal/tool/`, `internal/shallowmap/`, `internal/behavior/` lines.

- [ ] **Step 2: Update README.md**

- Usage: `--tool` on the five commands with a short Claude Code example (`codexssd status --tool claude`); a `report --connections` example mirroring the section format from Task 7; a short "Beyond Codex: Claude Code" subsection stating the stale-gate (fresh transcripts are never offered for cleaning because they power `claude --resume`) and the never-touch list in plain language.
- MCP section: unchanged (still five read-only tools).

- [ ] **Step 3: Update roadmap + scope**

- `docs/roadmap.md`: Phase 3 gets `> Status (2026-07): shipped — shallow probe for Claude Code project folders; Codex entries remain unprobed (report-only) for now.` Phase 4 gets `> Status (2026-07): partially shipped — tool profiles with Claude Code support (status/report/clean/restore/prune) and behavioral detection. Deep map, Cursor/Gemini, cost/token awareness, summaries, and extra interfaces remain future work.`
- `docs/scope.md`: move "Support for other AI tools" and "Behavioural detection" from the out-of-scope list into a new "Shipped since" note; keep the remaining out-of-scope items where they are.

- [ ] **Step 4: Verify and commit**

Run: `go build ./... && go vet ./... && go test ./... && gofmt -l .`
Expected: all PASS / clean (docs cannot break the build, but this is the final gate before the branch is done)

```bash
git add README.md CLAUDE.md docs/roadmap.md docs/scope.md
git commit -m "docs: sync README, CLAUDE.md, roadmap, scope to Phase 3 + Phase 4 core"
```

---

## Self-Review (completed)

- **Spec coverage:** Phase 3 shallow map → Task 7 (golden rule encoded as a two-value verdict type with no "safe" state). Phase 4 multi-tool → Tasks 1–6 (Claude Code end-to-end). Phase 4 behavioral detection → Task 8. Docs → Task 9. Deep map / Cursor / Gemini / cost-token / summaries / interfaces / team settings → explicitly deferred in the scope section, each independent.
- **Placeholder scan:** every code step carries real code; the two "read the file first" notes in Tasks 6 and 8 are for matching existing idiom in files this plan doesn't rewrite, with the required behavior fully specified.
- **Type consistency:** `Profile`/`FoundFile`/`Allows`/`CleanablePaths` names match across Tasks 1–6; `Plan.Tool`/`Manifest.Tool` empty-means-codex is consistent between Task 5's code and tests; `shallowmap.ProjectEntry` embeds `visibility.Entry` as consumed in Task 7's wiring; `behavior.Event` fields match the pre-seeded JSON line in Task 8's report test. Import-cycle resolution (tool must not import codex; codex aliases the profile) is spelled out in Task 3 and consistent with Task 1's final state.
