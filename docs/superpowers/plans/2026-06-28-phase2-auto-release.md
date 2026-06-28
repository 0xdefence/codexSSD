# Phase 2 — Recycling-Bin Auto-Release + Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Release recycling-bin backups whose ~2-week hold has elapsed by MOVING them into the OS Trash (never `rm`), automatically on app start and via `codexssd prune`, and surface each backup's age + release date.

**Architecture:** A new stdlib-only `internal/trash` package moves a path into the OS Trash (macOS `~/.Trash`; Linux XDG trash + `.trashinfo`; Windows = unsupported). `internal/cleaner` gains a pure `Expired` filter and `Release`/`ReleaseExpired` (move-only, gated to `codexssd-backups/` dirs, behind a `moveToTrash` seam). The CLI gets `prune`; the TUI auto-releases on start and shows bin ages/release dates.

**Tech Stack:** Go 1.25, stdlib only (charmbracelet stays only in `internal/tui`).

## Global Constraints

- **Release = MOVE to the OS Trash, never hard-delete.** CodexSSD never calls `os.Remove`/`os.RemoveAll` on a backup. Emptying the Trash is the user's explicit action (rule #1 preserved).
- **Only act on our own backups:** `Release`/`ReleaseExpired` operate only on directories under a `codexssd-backups/` parent.
- **Unsupported platform → keep held:** if the OS Trash is unsupported, release nothing; never hard-delete.
- **Hold:** a backup is releasable when `now` is at or after its manifest `HoldUntil` (= `MovedAt` + `RetentionDays`=14). Boundary: released when `now == HoldUntil`.
- **stdlib-only, no new dependencies.** `internal/trash` imported only by `internal/cleaner`.
- Naming: CodexSSD / `codexssd`.
- **Verification gate:** `go build ./... && go vet ./... && go test ./...` green and `gofmt -l .` empty before each commit. The TUI program is never started from tests; tests never touch the real OS Trash (use a stubbed seam / temp HOME).

## File Structure

- `internal/trash/trash.go` (+ `trash_test.go`) — **create.** `Move`, `ErrUnsupported`, `moveInto`.
- `internal/cleaner/release.go` (+ `release_test.go`) — **create.** `Expired`, `Release`, `ReleaseExpired`, `moveToTrash` seam, `isBackupDir`.
- `cmd/codexssd/main.go` (+ `main_test.go`) — **modify.** `prune` command.
- `internal/tui/commands.go`, `model.go`, `update.go`, `view.go` (+ `update_test.go`) — **modify.** Auto-release on start + bin visibility.

Existing API: `cleaner.Backup{Dir string; Manifest Manifest}`, `Manifest{MovedAt, HoldUntil time.Time; Items []ManifestItem}`, `cleaner.ListBackups(codexDir) ([]Backup, error)`, `cleaner.BackupDirName = "codexssd-backups"`, `cleaner.RetentionDays = 14`, unexported `writeManifest(dir, Manifest)`. `codex.Dir()`, `codex.HumanBytes`. CLI helper `emitJSON(v any) int`.

---

## Task 1: `internal/trash` — move a path into the OS Trash

**Files:**
- Create: `internal/trash/trash.go`, `internal/trash/trash_test.go`

**Interfaces:**
- Produces: `var ErrUnsupported error`; `func Move(path string) error`; unexported `moveInto(dir, path string) (string, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/trash/trash_test.go`:

```go
package trash

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMoveIntoCollisionSafe(t *testing.T) {
	dir := t.TempDir()
	mk := func(content string) string {
		p := filepath.Join(t.TempDir(), "x.txt")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p1, err := moveInto(dir, mk("1"))
	if err != nil {
		t.Fatalf("moveInto: %v", err)
	}
	p2, err := moveInto(dir, mk("2"))
	if err != nil {
		t.Fatalf("moveInto: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("collision not handled: both went to %s", p1)
	}
	for _, p := range []string{p1, p2} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestMoveSendsToOSTrash(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("trash unsupported on this OS")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))

	src := filepath.Join(t.TempDir(), "logs_2.sqlite")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Move(src); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source should be gone after Move")
	}
	var landed string
	if runtime.GOOS == "darwin" {
		landed = filepath.Join(home, ".Trash", "logs_2.sqlite")
	} else {
		landed = filepath.Join(home, "xdg", "Trash", "files", "logs_2.sqlite")
	}
	if _, err := os.Stat(landed); err != nil {
		t.Errorf("expected file in trash at %s: %v", landed, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/trash/ -v`
Expected: FAIL — package/`moveInto`/`Move` undefined (build error).

- [ ] **Step 3: Create trash.go**

Create `internal/trash/trash.go`:

```go
// Package trash moves files and directories into the operating system's Trash.
// A "released" item stays recoverable by the user; CodexSSD never hard-deletes,
// and emptying the Trash is the user's own explicit action.
//
// stdlib-only.
package trash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ErrUnsupported is returned on platforms without a known Trash location.
var ErrUnsupported = errors.New("trash not supported on this platform")

// Move moves path into the OS Trash.
func Move(path string) error {
	switch runtime.GOOS {
	case "darwin":
		dir, err := macTrashDir()
		if err != nil {
			return err
		}
		_, err = moveInto(dir, path)
		return err
	case "linux":
		return moveLinuxXDG(path)
	default:
		return ErrUnsupported
	}
}

func macTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func xdgTrashRoot() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

func moveLinuxXDG(path string) error {
	root, err := xdgTrashRoot()
	if err != nil {
		return err
	}
	filesDir := filepath.Join(root, "files")
	infoDir := filepath.Join(root, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	target, err := moveInto(filesDir, path)
	if err != nil {
		return err
	}
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", abs, time.Now().Format("2006-01-02T15:04:05"))
	return os.WriteFile(filepath.Join(infoDir, filepath.Base(target)+".trashinfo"), []byte(info), 0o600)
}

// moveInto moves path into dir, choosing a collision-safe name. Returns the
// final path. SAFETY: a plain os.Rename — a move, never a delete.
func moveInto(dir, path string) (string, error) {
	target := uniqueName(filepath.Join(dir, filepath.Base(path)))
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

func uniqueName(target string) string {
	if !pathExists(target) {
		return target
	}
	ext := filepath.Ext(target)
	stem := target[:len(target)-len(ext)]
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !pathExists(cand) {
			return cand
		}
	}
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
```

- [ ] **Step 4: Run tests; verify build/vet/format**

Run: `go test ./internal/trash/ -v && go build ./... && go vet ./... && gofmt -l .`
Expected: PASS; no `gofmt` output.

- [ ] **Step 5: Commit**

```bash
git add internal/trash
git commit -m "feat(trash): move files into the OS Trash (macOS ~/.Trash, Linux XDG)"
```

---

## Task 2: `internal/cleaner` — release expired backups

**Files:**
- Create: `internal/cleaner/release.go`, `internal/cleaner/release_test.go`

**Interfaces:**
- Consumes: `trash.Move` (Task 1); `Backup`, `Manifest`, `ListBackups`, `BackupDirName`, `writeManifest` (existing).
- Produces: `func Expired(backups []Backup, now time.Time) []Backup`; `func Release(backupDir string) error`; `func ReleaseExpired(codexDir string, now time.Time) ([]string, error)`; `var moveToTrash = trash.Move`.

- [ ] **Step 1: Write the failing test**

Create `internal/cleaner/release_test.go`:

```go
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpiredFilterBoundary(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	backups := []Backup{
		{Dir: "past", Manifest: Manifest{HoldUntil: now.Add(-time.Hour)}},
		{Dir: "exactly", Manifest: Manifest{HoldUntil: now}}, // boundary → released
		{Dir: "future", Manifest: Manifest{HoldUntil: now.Add(time.Hour)}},
	}
	got := Expired(backups, now)
	if len(got) != 2 {
		t.Fatalf("Expired len = %d, want 2 (past + exactly)", len(got))
	}
	if got[0].Dir != "past" || got[1].Dir != "exactly" {
		t.Errorf("Expired = %v, want [past exactly]", []string{got[0].Dir, got[1].Dir})
	}
}

// mkBackup writes a backup dir with a manifest holding until `hold`.
func mkBackup(t *testing.T, codexDir, id string, hold time.Time) string {
	t.Helper()
	bd := filepath.Join(codexDir, BackupDirName, id)
	if err := os.MkdirAll(bd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bd, "logs_2.sqlite"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeManifest(bd, Manifest{MovedAt: hold.AddDate(0, 0, -RetentionDays), HoldUntil: hold})
	return bd
}

func TestReleaseExpiredMovesOnlyExpired(t *testing.T) {
	prev := moveToTrash
	t.Cleanup(func() { moveToTrash = prev })
	var moved []string
	moveToTrash = func(p string) error { moved = append(moved, filepath.Base(p)); return nil }

	codexDir := t.TempDir()
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	mkBackup(t, codexDir, "20260601-000000", now.Add(-time.Hour)) // expired
	mkBackup(t, codexDir, "20260629-000000", now.Add(48*time.Hour)) // not expired

	released, err := ReleaseExpired(codexDir, now)
	if err != nil {
		t.Fatalf("ReleaseExpired: %v", err)
	}
	if len(released) != 1 || released[0] != "20260601-000000" {
		t.Fatalf("released = %v, want [20260601-000000]", released)
	}
	if len(moved) != 1 || moved[0] != "20260601-000000" {
		t.Errorf("moved = %v, want [20260601-000000]", moved)
	}
}

func TestReleaseRefusesNonBackupPath(t *testing.T) {
	prev := moveToTrash
	t.Cleanup(func() { moveToTrash = prev })
	called := false
	moveToTrash = func(string) error { called = true; return nil }

	if err := Release(filepath.Join(t.TempDir(), "not-a-backup")); err == nil {
		t.Fatal("Release should refuse a path not under codexssd-backups/")
	}
	if called {
		t.Error("moveToTrash must not be called for a refused path")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cleaner/ -run 'TestExpired|TestRelease' -v`
Expected: FAIL — `Expired`/`Release`/`ReleaseExpired`/`moveToTrash` undefined (build error).

- [ ] **Step 3: Create release.go**

Create `internal/cleaner/release.go`:

```go
package cleaner

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/trash"
)

// moveToTrash is a seam so tests can stub trashing without touching the real
// OS Trash. It defaults to the real implementation.
var moveToTrash = trash.Move

// Expired returns the backups whose hold has elapsed (now at or after HoldUntil).
func Expired(backups []Backup, now time.Time) []Backup {
	var out []Backup
	for _, b := range backups {
		if !now.Before(b.Manifest.HoldUntil) { // now >= HoldUntil
			out = append(out, b)
		}
	}
	return out
}

// Release moves a single backup directory into the OS Trash.
//
// SAFETY: a move, never a delete; refuses any path not directly under a
// codexssd-backups/ directory.
func Release(backupDir string) error {
	if !isBackupDir(backupDir) {
		return fmt.Errorf("refusing to release non-backup path: %s", backupDir)
	}
	return moveToTrash(backupDir)
}

// ReleaseExpired moves every expired backup under codexDir into the Trash and
// returns the released backup ids. If the platform's Trash is unsupported, the
// first Release returns trash.ErrUnsupported and nothing is hard-deleted.
func ReleaseExpired(codexDir string, now time.Time) ([]string, error) {
	backups, err := ListBackups(codexDir)
	if err != nil {
		return nil, err
	}
	var released []string
	for _, b := range Expired(backups, now) {
		if err := Release(b.Dir); err != nil {
			return released, err
		}
		released = append(released, filepath.Base(b.Dir))
	}
	return released, nil
}

// isBackupDir reports whether dir sits directly inside a codexssd-backups/ dir.
func isBackupDir(dir string) bool {
	return filepath.Base(filepath.Dir(dir)) == BackupDirName
}
```

- [ ] **Step 4: Run tests; verify build/vet/format**

Run: `go test ./internal/cleaner/ -v && go build ./... && go vet ./... && gofmt -l .`
Expected: PASS; no `gofmt` output.

- [ ] **Step 5: Commit**

```bash
git add internal/cleaner
git commit -m "feat(cleaner): release expired backups to the OS Trash (move-only, gated)"
```

---

## Task 3: `codexssd prune` command

**Files:**
- Modify: `cmd/codexssd/main.go`, `cmd/codexssd/main_test.go`

**Interfaces:**
- Consumes: `codex.Dir`, `cleaner.ListBackups`, `cleaner.Expired`, `cleaner.ReleaseExpired`, `emitJSON`, `filepath.Base`, `time.Now`.
- Produces: `cmdPrune([]string) int`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/codexssd/main_test.go` (reuse the existing `withSilencedStdout` helper; add imports `encoding/json`, `time`, and `github.com/0xdefence/codexssd/internal/cleaner` if not present):

```go
// writeExpiredBackup creates an expired backup under HOME/.codex for prune tests.
func writeExpiredBackup(t *testing.T, home, id string) string {
	t.Helper()
	bd := filepath.Join(home, ".codex", "codexssd-backups", id)
	if err := os.MkdirAll(bd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bd, "logs_2.sqlite"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	m := cleaner.Manifest{MovedAt: past, HoldUntil: past, Items: []cleaner.ManifestItem{{Name: "logs_2.sqlite", OriginalPath: filepath.Join(home, ".codex", "logs_2.sqlite"), Size: 1}}}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(bd, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return bd
}

func TestPruneDryRunReleasesNothing(t *testing.T) {
	withSilencedStdout(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	bd := writeExpiredBackup(t, home, "20000101-000000")

	if code := cmdPrune([]string{"--dry-run"}); code != 0 {
		t.Fatalf("prune --dry-run exit = %d, want 0", code)
	}
	if _, err := os.Stat(bd); err != nil {
		t.Errorf("--dry-run must not move the backup: %v", err)
	}
}

func TestPruneNoBackups(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdPrune(nil); code != 0 {
		t.Errorf("prune with no backups exit = %d, want 0", code)
	}
}

func TestPruneBadFlag(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdPrune([]string{"--nope"}); code != 2 {
		t.Errorf("prune bad flag exit = %d, want 2", code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/codexssd/ -run TestPrune -v`
Expected: FAIL — `cmdPrune` undefined.

- [ ] **Step 3: Wire the command**

In `cmd/codexssd/main.go`, add the dispatch case (after `restore`):

```go
	case "prune":
		return cmdPrune(rest)
```

Add a `prune` line to the `usage` string (after the `restore` line):

```
  prune          Release recycling-bin backups past their ~2-week hold to the Trash
```

Add the command (uses `time`, already imported; `cleaner`, `codex`, `filepath`, `emitJSON` already available):

```go
// cmdPrune implements `codexssd prune`: release backups past their hold to the
// OS Trash. --dry-run lists what would be released (read-only).
func cmdPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "list what would be released, without moving anything")
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd prune [--dry-run] [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Move recycling-bin backups past their ~2-week hold into the OS Trash.\n\n")
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

	if *dryRun {
		backups, err := cleaner.ListBackups(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: could not read backups: %v\n", err)
			return 1
		}
		expired := cleaner.Expired(backups, time.Now())
		ids := make([]string, 0, len(expired))
		for _, b := range expired {
			ids = append(ids, filepath.Base(b.Dir))
		}
		if *jsonOut {
			return emitJSON(map[string]any{"would_release": ids})
		}
		if len(ids) == 0 {
			fmt.Println("Nothing past its hold — nothing to release.")
			return 0
		}
		fmt.Printf("%d backup(s) past their hold would be released to the Trash:\n", len(ids))
		for _, id := range ids {
			fmt.Printf("  %s\n", id)
		}
		return 0
	}

	released, err := cleaner.ReleaseExpired(dir, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: prune failed: %v\n", err)
		return 1
	}
	if *jsonOut {
		return emitJSON(map[string]any{"released": released})
	}
	if len(released) == 0 {
		fmt.Println("Nothing past its hold — nothing to release.")
		return 0
	}
	fmt.Printf("Released %d backup(s) to the Trash (recoverable until you empty it).\n", len(released))
	return 0
}
```

- [ ] **Step 4: Run tests; verify build/vet/format and exercise**

Run:
```bash
go test ./cmd/codexssd/ -v && go build ./... && go vet ./... && gofmt -l .
go run ./cmd/codexssd prune --dry-run
```
Expected: PASS; no `gofmt` output; `prune --dry-run` prints "Nothing past its hold…" (or a list).

- [ ] **Step 5: Commit**

```bash
git add cmd/codexssd/main.go cmd/codexssd/main_test.go
git commit -m "feat(cli): prune command to release expired backups (--dry-run, --json)"
```

---

## Task 4: TUI — auto-release on start + bin visibility

**Files:**
- Modify: `internal/tui/commands.go`, `internal/tui/model.go`, `internal/tui/update.go`, `internal/tui/view.go`, `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `codexDir` seam, `cleaner.ReleaseExpired` (Task 2), `cleaner.Backup`/`Manifest`, `codex.HumanBytes`, `filepathBase`.
- Produces: `type releasedMsg struct { ids []string }`; `func releaseCmd() tea.Msg`; `Model.releaseNote string`; `Model.soonestRelease() (time.Time, bool)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/update_test.go`:

```go
func TestReleasedMsgShowsNoteAndReloads(t *testing.T) {
	m := New()
	m, cmd := step(m, releasedMsg{ids: []string{"a", "b"}})
	if !strings.Contains(m.releaseNote, "2") {
		t.Errorf("releaseNote = %q, want it to mention 2", m.releaseNote)
	}
	if cmd == nil {
		t.Error("a release should trigger a reload command")
	}
}

func TestDashboardShowsRecyclingBin(t *testing.T) {
	msg := loadedWithBackup() // one backup, HoldUntil 2026-06-26 10:00
	m, _ := step(New(), msg)
	view := m.View()
	if !strings.Contains(view, "Recycling bin") {
		t.Errorf("dashboard should show a recycling-bin line:\n%s", view)
	}
}

func TestRestoreListShowsReleaseDate(t *testing.T) {
	m, _ := step(New(), loadedWithBackup())
	m, _ = step(m, key("r"))
	if !strings.Contains(m.View(), "releases") {
		t.Errorf("restore list should show each backup's release date:\n%s", m.View())
	}
}
```

(`loadedWithBackup()` already exists from the interactive-shell tests; it sets one backup with a `Manifest.HoldUntil`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestReleased|TestDashboardShowsRecycling|TestRestoreListShowsRelease' -v`
Expected: FAIL — `releasedMsg`/`releaseNote`/recycling-bin line undefined.

- [ ] **Step 3: Add releaseCmd + releasedMsg**

In `internal/tui/commands.go`, add (uses `time`, `cleaner` — already imported there):

```go
// releasedMsg reports which expired backups were released to the Trash on start.
type releasedMsg struct {
	ids []string
}

// releaseCmd moves any expired recycling-bin backups into the OS Trash. It is
// best-effort: errors (e.g. unsupported platform) release nothing and are ignored.
func releaseCmd() tea.Msg {
	dir, err := codexDir()
	if err != nil {
		return releasedMsg{}
	}
	released, _ := cleaner.ReleaseExpired(dir, time.Now())
	return releasedMsg{ids: released}
}
```

- [ ] **Step 4: Model field, Init, and helper**

In `internal/tui/model.go`: add a field to `Model` (in the interaction section):

```go
	releaseNote string // note shown after an auto-release on start
```

Change `Init` to also run the release sweep:

```go
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadCmd, tickCmd(), releaseCmd)
}
```

Add a helper:

```go
// soonestRelease returns the earliest upcoming backup release time, if any.
func (m Model) soonestRelease() (time.Time, bool) {
	var soonest time.Time
	found := false
	for _, b := range m.backups {
		if !found || b.Manifest.HoldUntil.Before(soonest) {
			soonest = b.Manifest.HoldUntil
			found = true
		}
	}
	return soonest, found
}
```

- [ ] **Step 5: Handle releasedMsg in Update**

In `internal/tui/update.go`, add a case to the `Update` type switch:

```go
	case releasedMsg:
		if len(msg.ids) > 0 {
			m.releaseNote = fmt.Sprintf("Released %d old backup(s) to the Trash.", len(msg.ids))
		}
		return m, loadCmd // refresh the backups list after releasing
```

- [ ] **Step 6: Show the bin in the dashboard + release dates in the restore list**

In `internal/tui/view.go` `renderDashboard`, replace the existing backups/last-tidy block (the `if t, ok := m.lastTidy(); ok { … } else { … }` lines) with:

```go
	if t, ok := m.lastTidy(); ok {
		fmt.Fprintf(&b, "Recycling bin: %d backup(s) (last tidy %s)\n", len(m.backups), t.Format("2006-01-02 15:04"))
		if s, ok := m.soonestRelease(); ok {
			fmt.Fprintf(&b, "  next release: %s\n", s.Format("2006-01-02"))
		}
	} else {
		fmt.Fprintln(&b, "Recycling bin: empty")
	}
	if m.releaseNote != "" {
		fmt.Fprintln(&b, m.releaseNote)
	}
```

In `renderRestoreList`, change the per-backup line to include the release date — replace:

```go
		fmt.Fprintf(&b, "%s%-18s %10s\n", cursor, filepathBase(bk.Dir), codex.HumanBytes(total))
```

with:

```go
		fmt.Fprintf(&b, "%s%-18s %10s   releases %s\n", cursor, filepathBase(bk.Dir), codex.HumanBytes(total), bk.Manifest.HoldUntil.Format("2006-01-02"))
```

- [ ] **Step 7: Run tests; verify build/vet/format**

Run: `go test ./internal/tui/ -v && go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: PASS (new tests + all prior); no `gofmt` output.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): auto-release expired backups on start; show bin ages + release dates"
```

---

## Self-Review notes

- **Spec coverage:** OS-Trash move (Task 1); Expired/Release/ReleaseExpired, move-only + gated + unsupported→nothing (Task 2); `prune` + `--dry-run`/`--json` (Task 3); auto-release on app start + restore-list/dashboard visibility (Task 4). Per-project staleness explicitly deferred (not built).
- **Safety:** no `os.Remove`/`RemoveAll` of a backup anywhere; `Release` gated to `codexssd-backups/`; tests stub `moveToTrash` or use a temp `$HOME`/`$XDG_DATA_HOME` so the real Trash is never touched.
- **Type consistency:** `trash.Move`/`moveInto`, `cleaner.Expired/Release/ReleaseExpired`/`moveToTrash`/`isBackupDir`, `cmdPrune`, `releaseCmd`/`releasedMsg`/`releaseNote`/`soonestRelease` used consistently. Boundary: released when `now == HoldUntil` (`!now.Before(HoldUntil)`), covered by `TestExpiredFilterBoundary`.
- **Out of scope:** per-project "old logs" staleness (Phase 3), configurable retention, emptying the Trash for the user.
