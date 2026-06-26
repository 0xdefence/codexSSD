# `clean` & `restore` Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `codexssd clean` (dry-run by default; `--yes` moves Codex's own log files into a recoverable recycling bin) and `codexssd restore` (move them back from a receipt), with a process-based "is Codex running?" safety gate.

**Architecture:** A two-phase model — a read-only `Plan` describes what *would* move; `Plan.Apply` is the only writing path and moves files via `os.Rename` into `~/.codex/codexssd-backups/<timestamp>/`, writing a `manifest.json`. The recycling bin sits on the same filesystem as the source so moves are atomic metadata operations (no byte copy). `restore` replays a manifest. A process check (`ps` on macOS/Linux) refuses to act while Codex is running.

**Tech Stack:** Go standard library only (no third-party dependencies). `os/exec` to shell out to `ps`. `encoding/json` for the manifest.

## Global Constraints

Copied verbatim from the project's safety rules and stack (CLAUDE.md, docs/scope.md, docs/stack.md). Every task implicitly includes these:

- **Move aside, never hard-delete.** This stage adds NO permanent-deletion code path. Only `os.Rename` is used to relocate files; `os.Remove` is used only on empty directories and the manifest after a successful restore — never on a log file.
- **Only touch Codex's OWN known log files** (`codex.LogFileNames` = `logs_2.sqlite`, `logs_2.sqlite-wal`, `logs_2.sqlite-shm`), only inside `~/.codex`. Every move/restore is gated by `isCodexLog`.
- **When uncertain, report — never resolve.** If Codex may be running (or the platform can't be checked), refuse to act and tell the user.
- **No database for our own storage.** The manifest is a single small JSON file written once per clean. No SQLite.
- **No third-party dependencies.** Standard library only; single static binary.
- **Naming:** product is **CodexSSD**; module/binary/command is lowercase `codexssd`. Module path `github.com/0xdefence/codexssd`. Go 1.22+ (repo developed on the current toolchain).
- **Platforms:** macOS + Linux support the busy-check. On Windows, `codex.DetectProcesses` returns `codex.ErrUnsupportedPlatform`; `clean --yes` and `restore` refuse (dry-run / list still work).
- **Verification gate:** every task ends green on `go build ./... && go vet ./... && go test ./...` and `gofmt -l .` (must list nothing).

---

## File Structure

- `internal/codex/process.go` — **modify.** Replace the stub with real process detection (`DetectProcesses`, `IsCodexRunning`, `ErrUnsupportedPlatform`) plus pure helpers (`parseProcesses`, `matchesCodex`).
- `internal/codex/process_test.go` — **create.** Tests for the pure parse/match helpers.
- `internal/cleaner/clean.go` — **modify.** Replace stubbed `Plan`/`PlanItem`/`PlanCodexLogs`; keep `isCodexLog` and `BackupDirName`. `PlanCodexLogs` becomes read-only and takes `codexDir`.
- `internal/cleaner/clean_test.go` — **create.** Tests for the read-only plan.
- `internal/cleaner/apply.go` — **create.** `Plan.Apply`, `Manifest`, manifest read/write, the move + rollback logic.
- `internal/cleaner/apply_test.go` — **create.** Tests for move-aside + manifest + refusal + rollback.
- `internal/cleaner/restore.go` — **create.** `Backup`, `ListBackups`, `Restore`.
- `internal/cleaner/restore_test.go` — **create.** Tests for list + restore + overwrite refusal.
- `cmd/codexssd/main.go` — **modify.** Wire `clean` and `restore`; add render helpers writing to `io.Writer`.
- `cmd/codexssd/main_test.go` — **create.** Tests for the render helpers against a buffer.

---

## Task 1: Codex process detection

**Files:**
- Modify: `internal/codex/process.go` (replace entire file body below the package clause)
- Test: `internal/codex/process_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Process struct { PID int; Name string; Command string }` (JSON-tagged)
  - `var ErrUnsupportedPlatform error`
  - `func DetectProcesses() ([]Process, error)`
  - `func IsCodexRunning() (bool, error)`
  - unexported `parseProcesses(string) []Process`, `matchesCodex(Process) bool`

- [ ] **Step 1: Write the failing test**

Create `internal/codex/process_test.go`:

```go
package codex

import "testing"

func TestParseProcesses(t *testing.T) {
	out := "" +
		"  101 /usr/bin/codex --serve\n" +
		"  202 /usr/local/bin/node /opt/codex/cli.js\n" +
		"303 /bin/zsh -l\n" +
		"\n" + // blank line should be skipped
		"notanumber /bin/bad\n" // non-numeric pid skipped

	got := parseProcesses(out)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(got), got)
	}
	if got[0].PID != 101 || got[0].Name != "codex" {
		t.Errorf("got[0] = %+v, want pid 101 name codex", got[0])
	}
	if got[1].PID != 202 || got[1].Name != "node" {
		t.Errorf("got[1] = %+v, want pid 202 name node", got[1])
	}
	if got[2].PID != 303 || got[2].Name != "zsh" {
		t.Errorf("got[2] = %+v, want pid 303 name zsh", got[2])
	}
}

func TestMatchesCodex(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{"/usr/bin/codex --serve", true},      // exact base name codex
		{"codex", true},                       // bare
		{"/usr/bin/codex app-server", true},   // multiword hint
		{"/usr/bin/codex desktop", true},      // multiword hint
		{"node /opt/codex/cli.js", true},      // node wrapper running codex
		{"/bin/zsh -l", false},                // unrelated
		{"/Users/me/go/bin/codexssd status", false}, // our own tool, excluded
		{"", false},                           // empty
	}
	for _, c := range cases {
		got := matchesCodex(Process{Command: c.command})
		if got != c.want {
			t.Errorf("matchesCodex(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codex/ -run 'TestParseProcesses|TestMatchesCodex' -v`
Expected: FAIL — `parseProcesses`/`matchesCodex` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

Replace the entire contents of `internal/codex/process.go` with:

```go
package codex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrUnsupportedPlatform is returned when process detection is not available on
// the current OS (currently: Windows). Callers must treat this as "cannot
// verify Codex is stopped" and refuse to act, rather than assuming it is safe.
var ErrUnsupportedPlatform = errors.New("process detection not supported on this platform")

// Process is a read-only snapshot of a running process.
type Process struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`    // executable base name
	Command string `json:"command"` // full command line
}

// codexExactNames are executable base names that identify Codex itself.
var codexExactNames = []string{"codex"}

// codexCommandHints are substrings within a full command line that identify a
// Codex sub-process.
var codexCommandHints = []string{"codex app-server", "codex desktop"}

// DetectProcesses returns running processes that look like Codex.
//
// SAFETY: observation only — it never signals or alters a process. It also
// excludes codexssd's own process.
func DetectProcesses() ([]Process, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrUnsupportedPlatform
	}
	out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var matched []Process
	for _, p := range parseProcesses(string(out)) {
		if p.PID == self {
			continue
		}
		if matchesCodex(p) {
			matched = append(matched, p)
		}
	}
	return matched, nil
}

// IsCodexRunning reports whether any Codex-like process is currently running.
func IsCodexRunning() (bool, error) {
	procs, err := DetectProcesses()
	if err != nil {
		return false, err
	}
	return len(procs) > 0, nil
}

// parseProcesses turns `ps -axo pid=,args=` output into Processes. Each line is
// "<pid> <full command line>". Lines without a numeric leading PID are skipped.
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

// matchesCodex reports whether a process looks like Codex (and is not codexssd).
func matchesCodex(p Process) bool {
	fields := strings.Fields(p.Command)
	if len(fields) == 0 {
		return false
	}
	base := filepath.Base(fields[0])
	if base == "codexssd" {
		return false
	}
	for _, n := range codexExactNames {
		if base == n {
			return true
		}
	}
	for _, h := range codexCommandHints {
		if strings.Contains(p.Command, h) {
			return true
		}
	}
	if base == "node" && strings.Contains(p.Command, "codex") {
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/codex/ -v`
Expected: PASS (existing `TestHumanBytes`, `TestScanLogs*`, plus the two new tests).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: no output from `gofmt -l .`; build and vet succeed.

- [ ] **Step 6: Commit**

```bash
git add internal/codex/process.go internal/codex/process_test.go
git commit -m "feat(codex): detect running Codex processes via ps"
```

---

## Task 2: Read-only clean Plan

**Files:**
- Modify: `internal/cleaner/clean.go`
- Test: `internal/cleaner/clean_test.go`

**Interfaces:**
- Consumes: `codex.ScanLogs(dir) codex.LogReport`, `codex.LogFile{Name,Path,Exists,Size}`.
- Produces:
  - `type PlanItem struct { Name string; Path string; Size int64 }`
  - `type Plan struct { CodexDir string; BackupRoot string; Items []PlanItem; TotalBytes int64 }`
  - `func PlanCodexLogs(codexDir string) (Plan, error)`
  - `func (p Plan) Empty() bool`
  - `const BackupDirName = "codexssd-backups"` (unchanged)
  - unexported `isCodexLog(path string) bool` (unchanged)

- [ ] **Step 1: Write the failing test**

Create `internal/cleaner/clean_test.go`:

```go
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a file of exactly n bytes for testing.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}

func TestPlanCodexLogsEmpty(t *testing.T) {
	dir := t.TempDir() // no log files inside

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("Empty() = false, want true for %+v", plan)
	}
	if plan.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", plan.TotalBytes)
	}
}

func TestPlanCodexLogsListsPresentFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)
	// -shm intentionally absent

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	if plan.Empty() {
		t.Fatal("Empty() = true, want false")
	}
	if len(plan.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 (%+v)", len(plan.Items), plan.Items)
	}
	if plan.TotalBytes != 150 {
		t.Errorf("TotalBytes = %d, want 150", plan.TotalBytes)
	}
	wantRoot := filepath.Join(dir, BackupDirName)
	if plan.BackupRoot != wantRoot {
		t.Errorf("BackupRoot = %q, want %q", plan.BackupRoot, wantRoot)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleaner/ -v`
Expected: FAIL — `PlanCodexLogs` signature mismatch / `Empty` undefined / `TotalBytes` field missing (build error).

- [ ] **Step 3: Write minimal implementation**

Replace the entire contents of `internal/cleaner/clean.go` with:

```go
// Package cleaner safely tidies Codex's OWN log files by MOVING them into a
// recoverable recycling bin — never by hard-deleting.
//
// SAFETY (non-negotiable):
//   - It only ever acts on Codex's known log files (codex.LogFileNames).
//   - It MOVES files aside; it never deletes a log file. Permanent deletion must
//     be a separate, explicit user action (not in this build).
//   - Computing a Plan is read-only; only Plan.Apply touches the filesystem.
package cleaner

import (
	"path/filepath"

	"github.com/0xdefence/codexssd/internal/codex"
)

// BackupDirName is the recycling-bin root, created under ~/.codex. Moved-aside
// files land in a timestamped subdirectory beneath it. Keeping the bin under
// ~/.codex puts it on the same filesystem as the logs, so moves are atomic
// renames (no byte copy) — in keeping with the low-write design.
const BackupDirName = "codexssd-backups"

// PlanItem describes one file that clean would move aside.
type PlanItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

// Plan is a read-only description of what clean WOULD move aside. Building a Plan
// performs no writes.
type Plan struct {
	CodexDir   string     `json:"codex_dir"`
	BackupRoot string     `json:"backup_root"`
	Items      []PlanItem `json:"items"`
	TotalBytes int64      `json:"total_bytes"`
}

// Empty reports whether there is nothing to move aside.
func (p Plan) Empty() bool { return len(p.Items) == 0 }

// PlanCodexLogs inspects Codex's own logs in codexDir and returns a move-aside
// plan.
//
// SAFETY: read-only. It only ever considers codex.LogFileNames.
func PlanCodexLogs(codexDir string) (Plan, error) {
	report := codex.ScanLogs(codexDir)
	plan := Plan{
		CodexDir:   codexDir,
		BackupRoot: filepath.Join(codexDir, BackupDirName),
	}
	for _, f := range report.Files {
		if !f.Exists {
			continue
		}
		plan.Items = append(plan.Items, PlanItem{Name: f.Name, Path: f.Path, Size: f.Size})
		plan.TotalBytes += f.Size
	}
	return plan, nil
}

// isCodexLog is the safety gate: it reports whether path is one of Codex's own
// known log files. The cleaner must never move or restore anything this rejects.
func isCodexLog(path string) bool {
	base := filepath.Base(path)
	for _, name := range codex.LogFileNames {
		if base == name {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleaner/ -v`
Expected: PASS (both new tests).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: no `gofmt` output; build and vet succeed. (Note: `cmd/codexssd` still references nothing from cleaner yet, so it builds.)

- [ ] **Step 6: Commit**

```bash
git add internal/cleaner/clean.go internal/cleaner/clean_test.go
git commit -m "feat(cleaner): read-only Plan for Codex log move-aside"
```

---

## Task 3: Apply — move aside + manifest

**Files:**
- Create: `internal/cleaner/apply.go`
- Test: `internal/cleaner/apply_test.go`

**Interfaces:**
- Consumes: `Plan`, `PlanItem`, `isCodexLog` (Task 2).
- Produces:
  - `type ManifestItem struct { Name string; OriginalPath string; Size int64 }`
  - `type Manifest struct { MovedAt time.Time; HoldUntil time.Time; Items []ManifestItem }`
  - `const RetentionDays = 14`
  - `func (p Plan) Apply(now time.Time) (string, error)` — returns the created backup dir path.
  - unexported `writeManifest(dir string, m Manifest) error`, `readManifest(dir string) (Manifest, error)`, `const manifestName`, `const timestampLayout`.

- [ ] **Step 1: Write the failing test**

Create `internal/cleaner/apply_test.go`:

```go
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedTime() time.Time {
	return time.Date(2026, 6, 26, 14, 30, 0, 0, time.UTC)
}

func TestApplyMovesFilesAndWritesManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}

	dest, err := plan.Apply(fixedTime())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Originals are gone from the Codex dir.
	if _, err := os.Stat(filepath.Join(dir, "logs_2.sqlite")); !os.IsNotExist(err) {
		t.Errorf("logs_2.sqlite still present in codex dir")
	}

	// Files exist in the timestamped backup dir.
	wantDest := filepath.Join(dir, BackupDirName, "20260626-143000")
	if dest != wantDest {
		t.Errorf("dest = %q, want %q", dest, wantDest)
	}
	info, err := os.Stat(filepath.Join(dest, "logs_2.sqlite"))
	if err != nil || info.Size() != 100 {
		t.Errorf("moved logs_2.sqlite = %v (err %v), want size 100", info, err)
	}

	// Manifest is present and correct.
	m, err := readManifest(dest)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if len(m.Items) != 2 {
		t.Fatalf("manifest items = %d, want 2", len(m.Items))
	}
	if !m.MovedAt.Equal(fixedTime()) {
		t.Errorf("MovedAt = %v, want %v", m.MovedAt, fixedTime())
	}
	if !m.HoldUntil.Equal(fixedTime().AddDate(0, 0, RetentionDays)) {
		t.Errorf("HoldUntil = %v, want +%d days", m.HoldUntil, RetentionDays)
	}
	if m.Items[0].OriginalPath != filepath.Join(dir, "logs_2.sqlite") {
		t.Errorf("OriginalPath = %q", m.Items[0].OriginalPath)
	}
}

func TestApplyRefusesNonCodexFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "important.txt"), 10)

	// Hand-craft a malicious plan pointing at a non-Codex file.
	plan := Plan{
		CodexDir:   dir,
		BackupRoot: filepath.Join(dir, BackupDirName),
		Items:      []PlanItem{{Name: "important.txt", Path: filepath.Join(dir, "important.txt"), Size: 10}},
		TotalBytes: 10,
	}

	if _, err := plan.Apply(fixedTime()); err == nil {
		t.Fatal("Apply succeeded on a non-Codex file, want error")
	}
	// The file must be untouched.
	if _, err := os.Stat(filepath.Join(dir, "important.txt")); err != nil {
		t.Errorf("non-Codex file was moved/removed: %v", err)
	}
}

func TestApplyEmptyPlanErrors(t *testing.T) {
	plan := Plan{CodexDir: t.TempDir()}
	if _, err := plan.Apply(fixedTime()); err == nil {
		t.Error("Apply on empty plan succeeded, want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleaner/ -run TestApply -v`
Expected: FAIL — `Apply` signature mismatch / `readManifest`, `Manifest`, `RetentionDays` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleaner/apply.go`:

```go
package cleaner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RetentionDays is how long a moved-aside backup is held before it becomes
// eligible for release (auto-release itself is a later phase).
const RetentionDays = 14

const (
	manifestName    = "manifest.json"
	timestampLayout = "20060102-150405"
)

// ManifestItem records one moved file and where it came from.
type ManifestItem struct {
	Name         string `json:"name"`
	OriginalPath string `json:"original_path"`
	Size         int64  `json:"size_bytes"`
}

// Manifest is the receipt written into each backup directory. It makes the move
// recoverable and lets a later phase release the backup after HoldUntil.
type Manifest struct {
	MovedAt   time.Time      `json:"moved_at"`
	HoldUntil time.Time      `json:"hold_until"`
	Items     []ManifestItem `json:"items"`
}

// Apply moves the planned files into a new timestamped backup directory and
// writes a manifest. It returns the backup directory path.
//
// SAFETY: MOVES via os.Rename only — it never deletes a log file. Every item is
// re-checked against isCodexLog before being moved. On any move failure, files
// already moved this call are moved back (rollback) so a torn database is never
// left behind. `now` is injected so the directory name is deterministic/testable.
func (p Plan) Apply(now time.Time) (string, error) {
	if p.Empty() {
		return "", errors.New("nothing to move aside")
	}
	for _, it := range p.Items {
		if !isCodexLog(it.Path) {
			return "", fmt.Errorf("refusing to move non-Codex file: %s", it.Path)
		}
	}

	dest := filepath.Join(p.BackupRoot, now.Format(timestampLayout))
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", err
	}

	manifest := Manifest{
		MovedAt:   now,
		HoldUntil: now.AddDate(0, 0, RetentionDays),
	}
	// moved tracks (from, to) pairs for rollback on failure.
	var moved [][2]string
	rollback := func() {
		for _, mv := range moved {
			_ = os.Rename(mv[1], mv[0]) // best-effort move back
		}
		_ = os.Remove(dest)
	}

	for _, it := range p.Items {
		target := filepath.Join(dest, it.Name)
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

	if err := writeManifest(dest, manifest); err != nil {
		rollback()
		return "", err
	}
	return dest, nil
}

func writeManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), data, 0o600)
}

func readManifest(dir string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleaner/ -v`
Expected: PASS (all cleaner tests).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: no `gofmt` output; build and vet succeed.

- [ ] **Step 6: Commit**

```bash
git add internal/cleaner/apply.go internal/cleaner/apply_test.go
git commit -m "feat(cleaner): move logs aside with a recoverable manifest"
```

---

## Task 4: Restore from a manifest

**Files:**
- Create: `internal/cleaner/restore.go`
- Test: `internal/cleaner/restore_test.go`

**Interfaces:**
- Consumes: `Manifest`, `readManifest`, `manifestName`, `isCodexLog`, `BackupDirName` (Tasks 2–3).
- Produces:
  - `type Backup struct { Dir string; Manifest Manifest }`
  - `func ListBackups(codexDir string) ([]Backup, error)`
  - `func Restore(backupDir string) error`

- [ ] **Step 1: Write the failing test**

Create `internal/cleaner/restore_test.go`:

```go
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListBackupsNoRoot(t *testing.T) {
	backups, err := ListBackups(t.TempDir()) // no codexssd-backups dir
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("len = %d, want 0", len(backups))
	}
}

func TestRestoreMovesFilesBack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)

	plan, _ := PlanCodexLogs(dir)
	dest, err := plan.Apply(fixedTime())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Sanity: originals gone, one backup listed.
	backups, err := ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("len(backups) = %d, want 1", len(backups))
	}

	if err := Restore(dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Originals are back with correct sizes.
	info, err := os.Stat(filepath.Join(dir, "logs_2.sqlite"))
	if err != nil || info.Size() != 100 {
		t.Errorf("restored logs_2.sqlite = %v (err %v), want size 100", info, err)
	}
	// Backup directory is removed after a clean restore.
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("backup dir still present after restore")
	}
}

func TestRestoreRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)

	plan, _ := PlanCodexLogs(dir)
	dest, err := plan.Apply(fixedTime())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A fresh log appeared at the original location (Codex ran again).
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 7)

	if err := Restore(dest); err == nil {
		t.Fatal("Restore overwrote an existing file, want refusal")
	}
	// The fresh file must be untouched.
	info, _ := os.Stat(filepath.Join(dir, "logs_2.sqlite"))
	if info == nil || info.Size() != 7 {
		t.Errorf("existing file changed during refused restore: %v", info)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleaner/ -run 'TestListBackups|TestRestore' -v`
Expected: FAIL — `ListBackups`, `Restore`, `Backup` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleaner/restore.go`:

```go
package cleaner

import (
	"fmt"
	"os"
	"path/filepath"
)

// Backup is one moved-aside set, identified by its directory and manifest.
type Backup struct {
	Dir      string   `json:"dir"`
	Manifest Manifest `json:"manifest"`
}

// ListBackups returns the recoverable backups under codexDir's recycling bin,
// newest-last (directory names are timestamped, so lexical order is chronological).
// Directories without a readable manifest are skipped.
func ListBackups(codexDir string) ([]Backup, error) {
	root := filepath.Join(codexDir, BackupDirName)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var backups []Backup
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		m, err := readManifest(dir)
		if err != nil {
			continue // not a valid backup; ignore
		}
		backups = append(backups, Backup{Dir: dir, Manifest: m})
	}
	return backups, nil
}

// Restore moves every file recorded in backupDir's manifest back to its original
// path, then removes the now-empty backup directory.
//
// SAFETY: moves via os.Rename only; refuses any recorded path that is not a known
// Codex log; refuses to overwrite a file that already exists at the destination
// (so a fresh live log is never clobbered). On partial failure it rolls back.
func Restore(backupDir string) error {
	m, err := readManifest(backupDir)
	if err != nil {
		return err
	}
	// Pre-flight: validate every item before moving anything.
	for _, it := range m.Items {
		if !isCodexLog(it.OriginalPath) {
			return fmt.Errorf("refusing to restore non-Codex file: %s", it.OriginalPath)
		}
		if _, err := os.Stat(it.OriginalPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", it.OriginalPath)
		}
	}

	var moved [][2]string // (from, to) for rollback
	for _, it := range m.Items {
		src := filepath.Join(backupDir, it.Name)
		if err := os.Rename(src, it.OriginalPath); err != nil {
			for _, mv := range moved {
				_ = os.Rename(mv[1], mv[0])
			}
			return fmt.Errorf("restoring %s: %w", it.Name, err)
		}
		moved = append(moved, [2]string{src, it.OriginalPath})
	}

	// Clean up the now-empty backup directory (manifest + dir only; never a log).
	_ = os.Remove(filepath.Join(backupDir, manifestName))
	_ = os.Remove(backupDir)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleaner/ -v`
Expected: PASS (all cleaner tests).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: no `gofmt` output; build and vet succeed.

- [ ] **Step 6: Commit**

```bash
git add internal/cleaner/restore.go internal/cleaner/restore_test.go
git commit -m "feat(cleaner): restore moved-aside logs from a manifest"
```

---

## Task 5: `clean` command

**Files:**
- Modify: `cmd/codexssd/main.go`
- Test: `cmd/codexssd/main_test.go`

**Interfaces:**
- Consumes: `codex.Dir()`, `codex.IsCodexRunning()`, `codex.ErrUnsupportedPlatform`, `codex.HumanBytes()`, `cleaner.PlanCodexLogs(dir)`, `cleaner.Plan`, `cleaner.Plan.Empty()`, `cleaner.Plan.Apply(now)`.
- Produces: `cmdClean([]string) int`, `renderPlan(w io.Writer, p cleaner.Plan, running bool, supported bool)`.

- [ ] **Step 1: Write the failing test**

Create `cmd/codexssd/main_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/0xdefence/codexssd/internal/cleaner"
)

func TestRenderPlanEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderPlan(&buf, cleaner.Plan{CodexDir: "/x/.codex"}, false, true)
	out := buf.String()
	if !strings.Contains(out, "Nothing to move aside") {
		t.Errorf("empty plan output missing message:\n%s", out)
	}
}

func TestRenderPlanWithItemsAndSafety(t *testing.T) {
	var buf bytes.Buffer
	p := cleaner.Plan{
		CodexDir:   "/x/.codex",
		BackupRoot: "/x/.codex/codexssd-backups",
		Items: []cleaner.PlanItem{
			{Name: "logs_2.sqlite", Path: "/x/.codex/logs_2.sqlite", Size: 1024},
		},
		TotalBytes: 1024,
	}

	// Codex running -> output must warn it is not safe to clean.
	renderPlan(&buf, p, true, true)
	out := buf.String()
	if !strings.Contains(out, "logs_2.sqlite") || !strings.Contains(out, "1.0 KiB") {
		t.Errorf("plan output missing file/size:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "running") {
		t.Errorf("plan output should warn Codex is running:\n%s", out)
	}

	// Codex not running -> output must say it is safe.
	buf.Reset()
	renderPlan(&buf, p, false, true)
	if !strings.Contains(buf.String(), "--yes") {
		t.Errorf("plan output should tell the user to run --yes:\n%s", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/codexssd/ -v`
Expected: FAIL — `renderPlan` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

In `cmd/codexssd/main.go`, add `"io"`, `"time"`, and the cleaner import to the import block:

```go
import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
)
```

Replace the `case "clean":` line in `run`'s switch (currently `return cmdNotImplemented("clean")`) with:

```go
	case "clean":
		return cmdClean(rest)
```

Add these functions (anywhere after `cmdStatus`):

```go
// cmdClean implements `codexssd clean`.
//
// Default is a read-only dry run. `--yes` moves Codex's own logs aside into the
// recycling bin, but only after confirming Codex is not running. Nothing is ever
// deleted.
func cmdClean(args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "actually move the logs aside (default is a dry run)")
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd clean [--yes] [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Move Codex's own log files aside into a recoverable recycling bin.\n")
		fmt.Fprintf(os.Stderr, "Without --yes this only shows what would happen (read-only).\n\n")
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

	plan, err := cleaner.PlanCodexLogs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not inspect Codex logs: %v\n", err)
		return 1
	}

	running, runErr := codex.IsCodexRunning()
	supported := runErr != codex.ErrUnsupportedPlatform

	if !*yes {
		if *jsonOut {
			return emitJSON(map[string]any{
				"plan":               plan,
				"codex_running":      running,
				"platform_supported": supported,
			})
		}
		renderPlan(os.Stdout, plan, running, supported)
		return 0
	}

	// --yes: actually move aside. Refuse unless we can confirm Codex is stopped.
	if !supported {
		fmt.Fprintln(os.Stderr, "codexssd: cannot verify Codex is closed on this platform; refusing to move files.")
		fmt.Fprintln(os.Stderr, "Run without --yes to see what would be moved.")
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not check whether Codex is running: %v\n", runErr)
		return 1
	}
	if running {
		fmt.Fprintln(os.Stderr, "codexssd: Codex appears to be running. Close it first, then try again.")
		return 1
	}
	if plan.Empty() {
		fmt.Println("Nothing to move aside — no Codex log files are present.")
		return 0
	}

	dest, err := plan.Apply(time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: clean failed: %v\n", err)
		return 1
	}
	fmt.Printf("Moved %s of Codex logs aside to:\n  %s\n", codex.HumanBytes(plan.TotalBytes), dest)
	fmt.Println("Nothing was deleted. Restore them any time with \"codexssd restore\".")
	return 0
}

// renderPlan prints a friendly, plain-language dry-run report.
func renderPlan(w io.Writer, p cleaner.Plan, running bool, supported bool) {
	if p.Empty() {
		fmt.Fprintf(w, "Nothing to move aside — no Codex log files are present in %s.\n", p.CodexDir)
		return
	}

	fmt.Fprintf(w, "CodexSSD found %s of Codex log files it can safely move aside:\n\n", codex.HumanBytes(p.TotalBytes))
	for _, it := range p.Items {
		fmt.Fprintf(w, "  %-20s %10s\n", it.Name, codex.HumanBytes(it.Size))
	}
	fmt.Fprintf(w, "  %-20s %10s\n\n", "Total", codex.HumanBytes(p.TotalBytes))
	fmt.Fprintln(w, "These are Codex's own logs. Moving them frees the space; Codex makes")
	fmt.Fprintln(w, "fresh ones next time it runs. Nothing is deleted — files go to a")
	fmt.Fprintln(w, "recoverable bin and can be restored.")
	fmt.Fprintln(w)

	switch {
	case !supported:
		fmt.Fprintln(w, "Note: this platform can't check whether Codex is running, so --yes is disabled here.")
	case running:
		fmt.Fprintln(w, "Codex appears to be running. Close it before cleaning.")
	default:
		fmt.Fprintln(w, "Codex doesn't appear to be running.")
		fmt.Fprintln(w, `Run "codexssd clean --yes" to move them aside.`)
	}
}

// emitJSON writes v to stdout as indented JSON. Returns a process exit code.
func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: failed to encode JSON: %v\n", err)
		return 1
	}
	return 0
}
```

Note: `cmdStatus` already builds its own JSON inline; leave it as-is (do not route it through `emitJSON`) to keep this task's diff focused.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/codexssd/ -v`
Expected: PASS (both render tests).

- [ ] **Step 5: Verify build/vet/format and exercise the command**

Run:
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
go run ./cmd/codexssd clean
```
Expected: no `gofmt` output; all tests pass. `clean` prints a dry-run report (or "Nothing to move aside" if you have no `~/.codex` logs).

- [ ] **Step 6: Commit**

```bash
git add cmd/codexssd/main.go cmd/codexssd/main_test.go
git commit -m "feat(cli): clean command (dry-run by default, --yes moves aside)"
```

---

## Task 6: `restore` command

**Files:**
- Modify: `cmd/codexssd/main.go`
- Test: `cmd/codexssd/main_test.go` (add to the existing file)

**Interfaces:**
- Consumes: `codex.Dir()`, `codex.IsCodexRunning()`, `codex.ErrUnsupportedPlatform`, `codex.HumanBytes()`, `cleaner.ListBackups(dir)`, `cleaner.Backup`, `cleaner.Restore(dir)`, `filepath.Base`.
- Produces: `cmdRestore([]string) int`, `renderBackups(w io.Writer, backups []cleaner.Backup)`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/codexssd/main_test.go`:

```go
func TestRenderBackupsEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderBackups(&buf, nil)
	if !strings.Contains(buf.String(), "No backups") {
		t.Errorf("empty backups output missing message:\n%s", buf.String())
	}
}

func TestRenderBackupsLists(t *testing.T) {
	var buf bytes.Buffer
	backups := []cleaner.Backup{
		{
			Dir: "/x/.codex/codexssd-backups/20260626-143000",
			Manifest: cleaner.Manifest{
				Items: []cleaner.ManifestItem{
					{Name: "logs_2.sqlite", OriginalPath: "/x/.codex/logs_2.sqlite", Size: 2048},
				},
			},
		},
	}
	renderBackups(&buf, backups)
	out := buf.String()
	if !strings.Contains(out, "20260626-143000") {
		t.Errorf("backups output missing id:\n%s", out)
	}
	if !strings.Contains(out, "2.0 KiB") {
		t.Errorf("backups output missing total size:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/codexssd/ -run TestRenderBackups -v`
Expected: FAIL — `renderBackups` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

Add `"path/filepath"` to the import block in `cmd/codexssd/main.go`:

```go
	"os"
	"path/filepath"
	"time"
```

Replace the `case "install-agent":` block's neighbor — specifically add a `restore` case to the switch in `run` (place it right after the `clean` case):

```go
	case "restore":
		return cmdRestore(rest)
```

Update the `usage` string's command list to add the `restore` line (place after the `clean` line):

```
  restore        Move previously cleaned logs back from the recycling bin
```

Add these functions:

```go
// cmdRestore implements `codexssd restore`.
//
// With no argument it lists recoverable backups. With a backup id (the timestamp
// directory name) it moves that backup's files back to their original location.
func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the backup list as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd restore [--json] [backup-id]\n\n")
		fmt.Fprintf(os.Stderr, "With no id, lists recoverable backups. With an id, restores that backup.\n\n")
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

	backups, err := cleaner.ListBackups(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not read backups: %v\n", err)
		return 1
	}

	// No id: list backups.
	if fs.NArg() == 0 {
		if *jsonOut {
			return emitJSON(backups)
		}
		renderBackups(os.Stdout, backups)
		return 0
	}

	// Restoring overwrites the live log location, so refuse while Codex runs.
	running, runErr := codex.IsCodexRunning()
	if runErr == codex.ErrUnsupportedPlatform {
		fmt.Fprintln(os.Stderr, "codexssd: cannot verify Codex is closed on this platform; refusing to restore.")
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not check whether Codex is running: %v\n", runErr)
		return 1
	}
	if running {
		fmt.Fprintln(os.Stderr, "codexssd: Codex appears to be running. Close it first, then try again.")
		return 1
	}

	id := fs.Arg(0)
	for _, b := range backups {
		if filepath.Base(b.Dir) == id {
			if err := cleaner.Restore(b.Dir); err != nil {
				fmt.Fprintf(os.Stderr, "codexssd: restore failed: %v\n", err)
				return 1
			}
			fmt.Printf("Restored backup %s to %s.\n", id, dir)
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "codexssd: no backup with id %q. Run \"codexssd restore\" to list them.\n", id)
	return 1
}

// renderBackups prints the recoverable backups in plain language.
func renderBackups(w io.Writer, backups []cleaner.Backup) {
	if len(backups) == 0 {
		fmt.Fprintln(w, "No backups to restore — nothing has been moved aside yet.")
		return
	}
	fmt.Fprintln(w, "Recoverable backups:")
	for _, b := range backups {
		var total int64
		for _, it := range b.Manifest.Items {
			total += it.Size
		}
		fmt.Fprintf(w, "  %-18s %10s   (%d files)\n", filepath.Base(b.Dir), codex.HumanBytes(total), len(b.Manifest.Items))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Restore one with "codexssd restore <id>".`)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/codexssd/ -v`
Expected: PASS (all render tests).

- [ ] **Step 5: Verify build/vet/format and exercise end-to-end**

Run:
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
go run ./cmd/codexssd restore        # lists (likely "No backups")
go run ./cmd/codexssd help           # shows clean + restore
```
Expected: no `gofmt` output; all tests pass; `help` lists `clean` and `restore`.

- [ ] **Step 6: Commit**

```bash
git add cmd/codexssd/main.go cmd/codexssd/main_test.go
git commit -m "feat(cli): restore command to recover cleaned logs"
```

---

## Post-implementation: docs

- [ ] **Step 1: Update README usage section**

In `README.md`, under "Usage (today)", add `clean` and `restore` examples and note that `clean` is dry-run by default, moves aside (never deletes), and that restore exists. Keep the plain-language tone. Commit:

```bash
git add README.md
git commit -m "docs: document clean and restore commands"
```

- [ ] **Step 2: Update CLAUDE.md current-state**

In `CLAUDE.md`, update the "Current state" section: `status`, `clean`, and `restore` are implemented; `watch`, `install-agent`, `self` remain stubs. Commit:

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md current state for clean/restore"
```

---

## Self-Review notes

- **Spec coverage:** dry-run (Task 5), move-aside into recycling bin (Task 3), recoverable/manifest (Task 3), restore (Tasks 4 & 6), busy-check refusal (Tasks 1, 5, 6), allow-list safety on both move and restore (Tasks 3, 4), JSON output (Tasks 5, 6), no hard-delete (Global Constraints — no `os.Remove` on logs anywhere), no DB (manifest is JSON). Windows degradation (Tasks 1, 5, 6).
- **Type consistency:** `Plan{CodexDir,BackupRoot,Items,TotalBytes}`, `PlanItem{Name,Path,Size}`, `Manifest{MovedAt,HoldUntil,Items}`, `ManifestItem{Name,OriginalPath,Size}`, `Backup{Dir,Manifest}` are used identically across tasks and tests. `Apply(now time.Time) (string, error)` and `Restore(backupDir string) error` signatures match every call site.
- **Out of scope (confirmed deferred):** permanent deletion, recycling-bin auto-release after `HoldUntil`, repo junk scanning, `watch`/monitor, config file, token tracking.
