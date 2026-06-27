# self + recorder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Finish Phase 1's two small engine pieces — `recorder` (append a JSONL session receipt, capped, no DB) and `self` (`codexssd self` reports CodexSSD's own footprint).

**Architecture:** Both are pure-ish, stdlib-only. `recorder` appends one JSON line per session to `~/.codexssd/sessions.jsonl` (true append; trims to a record cap). `self` measures the size of CodexSSD's own state dir and prints a footprint. `self` is built after `recorder` so it can reuse `recorder.Dir()`.

**Tech Stack:** Go 1.25, stdlib only.

## Global Constraints

- **No database** — receipts are plain JSONL, appended; nothing else persisted. Low-write: one line per session; the file is trimmed to a cap.
- **CodexSSD's own storage lives only under `~/.codexssd`** (DirName). Neither package touches Codex's logs or the user's project.
- **Pure + stdlib-only**; no new dependencies. Functions take paths explicitly so tests use `t.TempDir()`.
- Naming: CodexSSD / `codexssd`.
- **Verification gate:** `go build ./... && go vet ./... && go test ./...` green and `gofmt -l .` empty before each commit.

## File Structure

- `internal/recorder/jsonl.go` — **replace.** Keep `DirName`/`FileName`/`Receipt`/`Path`; add `Dir`, `maxRecords`, real `Append`, unexported `appendTo`/`readLines`/`trimToMax`.
- `internal/recorder/jsonl_test.go` — **create.**
- `internal/self/report.go` — **replace.** New `Report{Mode, StateDir, HistoryBytes}` + `Measure(stateDir string)`.
- `internal/self/report_test.go` — **create.**
- `cmd/codexssd/main.go` — **modify.** Wire `self` → `cmdSelf`.
- `cmd/codexssd/main_test.go` — **modify.**

Existing scaffold: `recorder.Receipt{At time.Time; DurationSec float64; DiskWritten int64; PeakMBPerMin float64; FilesChanged int; Risk string}`, `recorder.Path()`, `recorder.DirName=".codexssd"`, `recorder.FileName="sessions.jsonl"`. `codex.HumanBytes`. CLI helper `emitJSON(v any) int` exists in main.go.

---

## Task 1: recorder — append-only JSONL receipts (capped)

**Files:**
- Replace: `internal/recorder/jsonl.go`
- Test: `internal/recorder/jsonl_test.go` (create)

**Interfaces:**
- Produces: `func Dir() (string, error)`; `func Append(r Receipt) error`; `const maxRecords = 1000`; unexported `appendTo(path string, r Receipt, max int) error`, `readLines(path string) ([]string, error)`, `trimToMax(path string, max int) error`. (Keeps `DirName`, `FileName`, `Receipt`, `Path`.)

- [ ] **Step 1: Write the failing test**

Create `internal/recorder/jsonl_test.go`:

```go
package recorder

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendToWritesOneLinePerCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	for i := 0; i < 3; i++ {
		if err := appendTo(path, Receipt{Risk: "LOW", DurationSec: float64(i)}, 1000); err != nil {
			t.Fatalf("appendTo: %v", err)
		}
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatalf("readLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
}

func TestAppendToTrimsToCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	for i := 0; i < 5; i++ {
		if err := appendTo(path, Receipt{DurationSec: float64(i)}, 3); err != nil {
			t.Fatalf("appendTo: %v", err)
		}
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatalf("readLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (capped)", len(lines))
	}
	// The newest (DurationSec 4) must be retained; the oldest (0,1) dropped.
	if want := `"duration_sec":4`; !contains(lines[2], want) {
		t.Errorf("last line = %q, want it to contain %q", lines[2], want)
	}
}

func TestReadLinesMissingFile(t *testing.T) {
	lines, err := readLines(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines = %d, want 0", len(lines))
	}
}

func TestDirUnderHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/whatever-home")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != filepath.Join("/tmp/whatever-home", DirName) {
		t.Errorf("Dir = %q", dir)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (func() bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}()) }

var _ = time.Now // keep time import if unused elsewhere
```

(Use `strings.Contains` instead of the hand-rolled `contains` if you prefer — import `strings` and delete the helper.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/recorder/ -v`
Expected: FAIL — `appendTo`/`readLines`/`Dir` undefined (build error; old `Append` is a stub).

- [ ] **Step 3: Replace jsonl.go**

Replace the entire contents of `internal/recorder/jsonl.go` with:

```go
// Package recorder persists CodexSSD's own session history as a simple JSONL
// file (one record per session).
//
// SAFETY / DESIGN: deliberately NOT a database. A tool that guards against
// aggressive local SQLite writes must not do the same itself — storage is a
// lightweight append-only plain-text file, written once per session and trimmed
// to a record cap.
package recorder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirName is CodexSSD's own state directory under the user's home directory.
const DirName = ".codexssd"

// FileName is the append-only session-history file inside DirName.
const FileName = "sessions.jsonl"

// maxRecords caps how many session receipts are kept (oldest trimmed first).
const maxRecords = 1000

// Receipt is the single record appended at the end of a session.
type Receipt struct {
	At           time.Time `json:"at"`
	DurationSec  float64   `json:"duration_sec"`
	DiskWritten  int64     `json:"disk_written_bytes"`
	PeakMBPerMin float64   `json:"peak_mb_per_min"`
	FilesChanged int       `json:"files_changed"`
	Risk         string    `json:"risk"`
}

// Dir returns the absolute path to CodexSSD's state directory (~/.codexssd).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// Path returns the absolute path to the session-history file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Append writes one Receipt as a single JSON line, then trims to maxRecords.
//
// DESIGN: append-only JSONL, capped history; never a database.
func Append(r Receipt) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return appendTo(path, r, maxRecords)
}

// appendTo appends r to the JSONL file at path and trims to max records.
func appendTo(path string, r Receipt, max int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return trimToMax(path, max)
}

// readLines returns the non-empty lines of the file at path (none if missing).
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// trimToMax rewrites path keeping only the most-recent max lines (no-op if at
// or under the cap). This is the only non-append write, and only when needed.
func trimToMax(path string, max int) error {
	if max <= 0 {
		return nil
	}
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	if len(lines) <= max {
		return nil
	}
	kept := lines[len(lines)-max:]
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}
```

- [ ] **Step 4: Run tests; verify build/vet/format**

Run: `go test ./internal/recorder/ -v && go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: PASS; no `gofmt` output; all green.

- [ ] **Step 5: Commit**

```bash
git add internal/recorder
git commit -m "feat(recorder): append-only JSONL session receipts with a record cap"
```

---

## Task 2: self — footprint report + CLI

**Files:**
- Replace: `internal/self/report.go`
- Test: `internal/self/report_test.go` (create)
- Modify: `cmd/codexssd/main.go`, `cmd/codexssd/main_test.go`

**Interfaces:**
- Consumes: `recorder.Dir` (Task 1), `codex.HumanBytes`, `emitJSON`.
- Produces: `type Report struct { Mode string; StateDir string; HistoryBytes int64 }`; `func Measure(stateDir string) (Report, error)`; `cmdSelf([]string) int`.

- [ ] **Step 1: Write the failing test**

Create `internal/self/report_test.go`:

```go
package self

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureSumsStateDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sessions.jsonl"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o700)
	if err := os.WriteFile(filepath.Join(sub, "x"), make([]byte, 50), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Measure(dir)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if r.HistoryBytes != 150 {
		t.Errorf("HistoryBytes = %d, want 150", r.HistoryBytes)
	}
	if r.Mode != "low-write" {
		t.Errorf("Mode = %q, want low-write", r.Mode)
	}
	if r.StateDir != dir {
		t.Errorf("StateDir = %q, want %q", r.StateDir, dir)
	}
}

func TestMeasureMissingDirIsZero(t *testing.T) {
	r, err := Measure(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if r.HistoryBytes != 0 {
		t.Errorf("HistoryBytes = %d, want 0", r.HistoryBytes)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/self/ -v`
Expected: FAIL — `Measure(dir)` signature mismatch / `Report` fields changed (build error).

- [ ] **Step 3: Replace report.go**

Replace the entire contents of `internal/self/report.go` with:

```go
// Package self reports CodexSSD's OWN footprint so the tool holds itself to the
// same standard it holds the agents it watches — and can show it isn't the
// thing causing the problem.
package self

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Report is CodexSSD's own footprint.
type Report struct {
	Mode         string `json:"mode"`
	StateDir     string `json:"state_dir"`
	HistoryBytes int64  `json:"history_bytes"`
}

// Measure reports CodexSSD's own footprint: the total size of its state
// directory (its only persistent storage). A missing dir reports 0, not an error.
func Measure(stateDir string) (Report, error) {
	r := Report{Mode: "low-write", StateDir: stateDir}
	size, err := dirSize(stateDir)
	if err != nil {
		return r, err
	}
	r.HistoryBytes = size
	return r, nil
}

// dirSize sums the sizes of all regular files under dir (0 if dir is absent).
func dirSize(dir string) (int64, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
```

- [ ] **Step 4: Run self tests**

Run: `go test ./internal/self/ -v`
Expected: PASS.

- [ ] **Step 5: Write the CLI test**

Add to `cmd/codexssd/main_test.go` (reuse the existing stdout-silencing helper if present — Task install-agent added `withSilencedStdout`):

```go
func TestSelfRuns(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdSelf(nil); code != 0 {
		t.Errorf("self exit = %d, want 0", code)
	}
}

func TestSelfJSON(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdSelf([]string{"--json"}); code != 0 {
		t.Errorf("self --json exit = %d, want 0", code)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./cmd/codexssd/ -run TestSelf -v`
Expected: FAIL — `cmdSelf` undefined.

- [ ] **Step 7: Wire the command**

In `cmd/codexssd/main.go`, add imports `"github.com/0xdefence/codexssd/internal/recorder"` and `"github.com/0xdefence/codexssd/internal/self"`. Replace the dispatch line `case "self": return cmdNotImplemented("self")` with:

```go
	case "self":
		return cmdSelf(rest)
```

Add the command:

```go
// cmdSelf implements `codexssd self`: report CodexSSD's own footprint.
func cmdSelf(args []string) int {
	fs := flag.NewFlagSet("self", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd self [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Report CodexSSD's own footprint (read-only).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dir, err := recorder.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return 1
	}
	rep, err := self.Measure(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not measure footprint: %v\n", err)
		return 1
	}

	if *jsonOut {
		return emitJSON(rep)
	}
	fmt.Println("CodexSSD's own footprint:")
	fmt.Printf("  mode:     %s\n", rep.Mode)
	fmt.Printf("  storage:  %s  (%s)\n", codex.HumanBytes(rep.HistoryBytes), rep.StateDir)
	return 0
}
```

- [ ] **Step 8: Run tests; verify build/vet/format**

Run: `go test ./cmd/codexssd/ -v && go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: PASS; no `gofmt` output; all green. (Optional: `go run ./cmd/codexssd self`.)

- [ ] **Step 9: Commit**

```bash
git add internal/self cmd/codexssd
git commit -m "feat(self): report CodexSSD's own footprint (storage size); wire self command"
```

---

## Self-Review notes

- **Coverage:** recorder append + cap + missing-file + Dir (T1); self dir-size + missing-dir + CLI run/JSON (T2).
- **No DB:** recorder is append-only JSONL, trimmed by count; the only non-append write is the rare trim. self only reads sizes.
- **Type consistency:** `recorder.Dir/Append/appendTo/readLines/trimToMax/maxRecords`, `self.Report{Mode,StateDir,HistoryBytes}`/`Measure(stateDir)`, `cmdSelf` used consistently; `cmdSelf` uses `recorder.Dir()` for the path.
- **Out of scope (later):** wiring `recorder.Append` into the TUI session lifecycle (write a receipt on exit) and age/size-based trim (`max_days`/`max_mb`) — the count cap is the v1 mechanism; the watch-loop integration is a future micro-task.
