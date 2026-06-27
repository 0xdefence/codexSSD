# install-agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `codexssd install-agent` writes a disk/token-safe `AGENTS.md` into a repo so the AI agent makes less mess at the source — with profiles and a never-clobber safety default.

**Architecture:** `internal/agent` becomes a small, pure, stdlib-only package: `profiles.go` renders the file content per profile; `install.go` writes it with a refuse-if-exists default and a hidden marker so `--force` can tell our file from a hand-written one. The CLI wires it up. No process gate (this never touches Codex logs).

**Tech Stack:** Go 1.25, stdlib only.

## Global Constraints

- **Never clobber by default:** if `AGENTS.md` exists, refuse (`ErrExists`) unless `--force`. Generated files carry a hidden marker (`<!-- codexssd:generated profile=… -->`); `--force` over a file lacking the marker reports it replaced a foreign (hand-written) file.
- **Writes ONE file** (`AGENTS.md`) into the user-chosen dir; never deletes anything; no other files touched.
- **Pure + stdlib-only**; no new dependencies. `internal/agent` imports nothing outside the stdlib.
- Profiles: `balanced` (default), `strict`, `repo-only`, `disk-token-safe`.
- Naming: CodexSSD / `codexssd`.
- **Verification gate:** `go build ./... && go vet ./... && go test ./...` green and `gofmt -l .` empty before each commit.

## File Structure

- `internal/agent/profiles.go` — **replace.** `Profile` + constants (keep), `Profiles`, `Parse`, `coreRules`, `profileExtra`, `Content`.
- `internal/agent/install.go` — **replace.** Package doc, `FileName` (keep), `marker`, `ErrExists`, `Install`, `isGenerated`.
- `internal/agent/*_test.go` — **create.**
- `cmd/codexssd/main.go` — **modify.** Wire `install-agent`; add `cmdInstallAgent`; update `usage`.
- `cmd/codexssd/main_test.go` — **modify.**

---

## Task 1: profiles — Parse + Content

**Files:**
- Replace: `internal/agent/profiles.go`
- Test: `internal/agent/profiles_test.go` (create)

**Interfaces:**
- Produces: `Profile` (string) + `ProfileBalanced/ProfileStrict/ProfileRepoOnly/ProfileDiskTokenSafe`; `var Profiles []Profile`; `func Parse(name string) (Profile, error)`; `func Content(p Profile) string`. (`Content` references `marker`, defined in `install.go` — same package; Task 2 adds it. To keep Task 1 self-contained and compiling, define `marker` here in Task 1; Task 2's `install.go` will NOT redefine it.)

- [ ] **Step 1: Write the failing test**

Create `internal/agent/profiles_test.go`:

```go
package agent

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, name := range []string{"balanced", "strict", "repo-only", "disk-token-safe"} {
		if _, err := Parse(name); err != nil {
			t.Errorf("Parse(%q) errored: %v", name, err)
		}
	}
	if _, err := Parse("bogus"); err == nil {
		t.Error("Parse(\"bogus\") should error")
	}
}

func TestContentHasMarkerAndRules(t *testing.T) {
	c := Content(ProfileBalanced)
	if !strings.HasPrefix(c, marker) {
		t.Errorf("content should start with the generated marker:\n%s", c)
	}
	if !strings.Contains(c, "profile=balanced") {
		t.Errorf("content should record the profile name")
	}
	if !strings.Contains(c, "minimal") {
		t.Errorf("content should include the core 'minimal edits' rule")
	}

	cases := map[Profile]string{
		ProfileStrict:        "smallest change",
		ProfileRepoOnly:      ".gitignore",
		ProfileDiskTokenSafe: "ls -R",
	}
	for p, want := range cases {
		if !strings.Contains(Content(p), want) {
			t.Errorf("Content(%s) missing %q", p, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agent/ -run 'TestParse|TestContent' -v`
Expected: FAIL — `Parse`/`Content`/`marker` undefined (build error; old stub has `Rules`).

- [ ] **Step 3: Replace profiles.go**

Replace the entire contents of `internal/agent/profiles.go` with:

```go
package agent

import (
	"fmt"
	"strings"
)

// Profile selects how strict the installed AGENTS.md rules are.
type Profile string

const (
	// ProfileBalanced is the good default: discourages unnecessary churn.
	ProfileBalanced Profile = "balanced"
	// ProfileStrict suits fragile repos, low disk space, or long sessions.
	ProfileStrict Profile = "strict"
	// ProfileRepoOnly focuses on source-control cleanliness.
	ProfileRepoOnly Profile = "repo-only"
	// ProfileDiskTokenSafe guards both SSD writes and token budget.
	ProfileDiskTokenSafe Profile = "disk-token-safe"
)

// Profiles is the set of valid profiles (for help text and validation).
var Profiles = []Profile{ProfileBalanced, ProfileStrict, ProfileRepoOnly, ProfileDiskTokenSafe}

// marker is the hidden first-line tag CodexSSD writes into every generated
// AGENTS.md, so it can tell its own file from a hand-written one.
const marker = "<!-- codexssd:generated"

// Parse validates a profile name.
func Parse(name string) (Profile, error) {
	for _, p := range Profiles {
		if string(p) == name {
			return p, nil
		}
	}
	return "", fmt.Errorf("unknown profile %q (choose balanced, strict, repo-only, disk-token-safe)", name)
}

// coreRules apply to every profile.
var coreRules = []string{
	"Make minimal, targeted edits — don't rewrite whole files.",
	"Don't modify lockfiles unless the task is specifically about dependencies.",
	"Don't re-run the full test suite repeatedly — prefer targeted tests.",
	"Don't create coverage/, dist/, build/, or cache directories casually.",
	"Don't paste large command output (full test logs, full git diff, ls -R) back into context.",
	"Don't create persistent local databases or caches.",
}

// profileExtra adds profile-specific emphasis on top of the core rules.
var profileExtra = map[Profile][]string{
	ProfileBalanced: nil,
	ProfileStrict: {
		"Make the smallest change that works; never rewrite a file wholesale.",
		"Ask before running the full test suite or other long-running commands.",
		"Don't start background processes; clean up any generated artifacts.",
	},
	ProfileRepoOnly: {
		"Never touch lockfiles or commit generated/build artifacts.",
		"Respect .gitignore; don't leave stray tracked files behind.",
		"Keep diffs small and reviewable.",
	},
	ProfileDiskTokenSafe: {
		"Avoid ls -R, repeated full git diff, and verbose build/test/docker logs in context.",
		"Keep command output short; summarize instead of pasting.",
		"Never create persistent local DBs or caches.",
	},
}

// Content renders the full AGENTS.md for a profile, including the marker.
func Content(p Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s profile=%s -->\n", marker, p)
	b.WriteString("# AGENTS.md\n\n")
	b.WriteString("House rules for AI coding agents in this repo, installed by CodexSSD\n")
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -v`
Expected: FAIL TO BUILD if `install.go` still has the old stub referencing the removed `Rules`/`errNotImplemented`. That's fine — Task 2 replaces `install.go`. To verify Task 1 in isolation, temporarily this package won't compile until Task 2. Therefore: do Task 1 and Task 2 edits, then run. (See Task 2 Step 4 for the combined green run.)

Note: because the old `install.go` stub calls `Rules` and uses `errNotImplemented`, the package won't compile after Task 1 alone. Proceed directly into Task 2 before running the suite; commit once both compile. (This is the one place two files must change together.)

- [ ] **Step 5: (commit deferred to Task 2 — the package compiles only after both files are replaced)**

---

## Task 2: install — write with never-clobber default + marker

**Files:**
- Replace: `internal/agent/install.go`
- Test: `internal/agent/install_test.go` (create)

**Interfaces:**
- Consumes: `Content`, `Profile`, `FileName`, `marker` (Task 1).
- Produces: `const FileName = "AGENTS.md"`; `var ErrExists error`; `func Install(dir string, p Profile, force bool) (path string, replacedForeign bool, err error)`; `func isGenerated(path string) bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/install_test.go`:

```go
package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWritesFile(t *testing.T) {
	dir := t.TempDir()
	path, foreign, err := Install(dir, ProfileBalanced, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if foreign {
		t.Error("replacedForeign should be false on a fresh write")
	}
	if path != filepath.Join(dir, FileName) {
		t.Errorf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(data), marker) {
		t.Error("written file should carry the generated marker")
	}
}

func TestInstallRefusesExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Install(dir, ProfileBalanced, false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Second install without --force must refuse and leave the file intact.
	before, _ := os.ReadFile(filepath.Join(dir, FileName))
	_, _, err := Install(dir, ProfileStrict, false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(before) != string(after) {
		t.Error("refused install must not change the file")
	}
}

func TestInstallForceOverwritesOwnFile(t *testing.T) {
	dir := t.TempDir()
	Install(dir, ProfileBalanced, false)
	_, foreign, err := Install(dir, ProfileStrict, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if foreign {
		t.Error("overwriting our own generated file should not report foreign")
	}
}

func TestInstallForceReportsForeign(t *testing.T) {
	dir := t.TempDir()
	// A hand-written AGENTS.md (no marker).
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("# my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, foreign, err := Install(dir, ProfileBalanced, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if !foreign {
		t.Error("force over a non-CodexSSD file should report replacedForeign=true")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agent/ -run TestInstall -v`
Expected: FAIL — `ErrExists`/`Install` new signature/`isGenerated` not yet defined (build error).

- [ ] **Step 3: Replace install.go**

Replace the entire contents of `internal/agent/install.go` with:

```go
// Package agent installs "please behave" rules (an AGENTS.md file) for AI coding
// agents, to reduce avoidable disk and token churn at the source.
//
// SAFETY: it writes a single new file into a user-chosen repo and never deletes
// anything. It refuses to overwrite an existing AGENTS.md unless forced, and
// marks its own files so a forced overwrite can warn when it replaces a
// hand-written one.
package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the file written into the target repository.
const FileName = "AGENTS.md"

// ErrExists is returned when an AGENTS.md already exists and force is false.
var ErrExists = errors.New("AGENTS.md already exists")

// Install writes an AGENTS.md for the given profile into dir.
//
// If the file exists and force is false, it returns ErrExists and writes
// nothing. With force, it overwrites; replacedForeign is true when the existing
// file was NOT one CodexSSD generated (i.e. likely hand-written).
func Install(dir string, p Profile, force bool) (path string, replacedForeign bool, err error) {
	path = filepath.Join(dir, FileName)
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		if !force {
			return "", false, ErrExists
		}
		replacedForeign = !isGenerated(path)
	}
	if err := os.WriteFile(path, []byte(Content(p)), 0o644); err != nil {
		return "", false, err
	}
	return path, replacedForeign, nil
}

// isGenerated reports whether the file at path was written by CodexSSD (carries
// the marker on its first line).
func isGenerated(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(data), marker)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -v`
Expected: PASS (Task 1 + Task 2 tests).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green.

- [ ] **Step 6: Commit**

```bash
git add internal/agent
git commit -m "feat(agent): render + install AGENTS.md with profiles and never-clobber default"
```

---

## Task 3: CLI command

**Files:**
- Modify: `cmd/codexssd/main.go`
- Test: `cmd/codexssd/main_test.go`

**Interfaces:**
- Consumes: `agent.Parse`, `agent.Content`, `agent.Install`, `agent.ErrExists`, `agent.FileName`, `agent.ProfileBalanced`.
- Produces: `cmdInstallAgent([]string) int`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/codexssd/main_test.go`:

```go
import (
	"os"
	"path/filepath"
	// (keep existing imports: bytes, strings, testing, cleaner)
)

// withSilencedStdout sends stdout/stderr to /dev/null for the test.
func withSilencedStdout(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("devnull: %v", err)
	}
	o, e := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	t.Cleanup(func() { os.Stdout, os.Stderr = o, e; devnull.Close() })
}

func TestInstallAgentWritesToDir(t *testing.T) {
	withSilencedStdout(t)
	dir := t.TempDir()
	if code := cmdInstallAgent([]string{dir}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md not written: %v", err)
	}
}

func TestInstallAgentPrintWritesNothing(t *testing.T) {
	withSilencedStdout(t)
	dir := t.TempDir()
	if code := cmdInstallAgent([]string{"--print", dir}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("--print should not write a file")
	}
}

func TestInstallAgentUnknownProfile(t *testing.T) {
	withSilencedStdout(t)
	if code := cmdInstallAgent([]string{"--profile", "bogus", t.TempDir()}); code != 2 {
		t.Errorf("exit = %d, want 2 for unknown profile", code)
	}
}

func TestInstallAgentRefusesExisting(t *testing.T) {
	withSilencedStdout(t)
	dir := t.TempDir()
	if code := cmdInstallAgent([]string{dir}); code != 0 {
		t.Fatalf("first exit = %d, want 0", code)
	}
	if code := cmdInstallAgent([]string{dir}); code != 1 {
		t.Errorf("second exit = %d, want 1 (refuse existing)", code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/codexssd/ -run TestInstallAgent -v`
Expected: FAIL — `cmdInstallAgent` undefined.

- [ ] **Step 3: Wire the command**

In `cmd/codexssd/main.go`, add to the import block:

```go
	"errors"
	...
	"github.com/0xdefence/codexssd/internal/agent"
```

Replace the dispatch line `case "install-agent": return cmdNotImplemented("install-agent")` with:

```go
	case "install-agent":
		return cmdInstallAgent(rest)
```

Update the `usage` string's `install-agent` line to mention the flags (optional wording):

```
  install-agent  Write a disk/token-safe AGENTS.md into a repo (--profile, --force, --print)
```

Add the command function:

```go
// cmdInstallAgent implements `codexssd install-agent`.
//
// It writes a disk/token-safe AGENTS.md into a repo. It refuses to overwrite an
// existing AGENTS.md unless --force; --print previews the rules without writing.
func cmdInstallAgent(args []string) int {
	fs := flag.NewFlagSet("install-agent", flag.ContinueOnError)
	profileName := fs.String("profile", string(agent.ProfileBalanced), "rule profile: balanced, strict, repo-only, disk-token-safe")
	force := fs.Bool("force", false, "overwrite an existing AGENTS.md")
	printOnly := fs.Bool("print", false, "print the rules to stdout instead of writing a file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd install-agent [--profile <name>] [--force] [--print] [dir]\n\n")
		fmt.Fprintf(os.Stderr, "Write a disk/token-safe AGENTS.md into dir (default \".\").\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, err := agent.Parse(*profileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
		return 2
	}

	if *printOnly {
		fmt.Print(agent.Content(p))
		return 0
	}

	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	path, replacedForeign, err := agent.Install(dir, p, *force)
	if err != nil {
		if errors.Is(err, agent.ErrExists) {
			fmt.Fprintf(os.Stderr, "codexssd: %s already exists — leaving it untouched.\n", filepath.Join(dir, agent.FileName))
			fmt.Fprintln(os.Stderr, "Re-run with --force to overwrite, or --print to preview.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "codexssd: could not write AGENTS.md: %v\n", err)
		return 1
	}
	if replacedForeign {
		fmt.Printf("Note: replaced an existing AGENTS.md that CodexSSD didn't create.\n")
	}
	fmt.Printf("Wrote %s (%s profile).\n", path, p)
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/codexssd/ -v`
Expected: PASS (new install-agent tests + all prior).

- [ ] **Step 5: Verify build/vet/format and exercise**

Run:
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
go run ./cmd/codexssd install-agent --print | head -5
```
Expected: no `gofmt` output; all green; `--print` shows the marker + rules.

- [ ] **Step 6: Commit**

```bash
git add cmd/codexssd/main.go cmd/codexssd/main_test.go
git commit -m "feat(cli): install-agent command (profiles, --force, --print)"
```

---

## Self-Review notes

- **Coverage:** profile rendering + parse (T1); never-clobber install + marker + foreign detection (T2); CLI with `--profile`/`--force`/`--print`/dir + refuse-existing exit code (T3).
- **Safety:** writes one file, never deletes; refuses existing without `--force`; `os.WriteFile` only. No process gate needed (doesn't touch Codex logs).
- **Type consistency:** `marker` defined once (in profiles.go, Task 1); `Install(dir, p, force) (path, replacedForeign, err)`, `ErrExists`, `Content(p)`, `Parse(name)` used identically across tasks and the CLI.
- **Note:** the package compiles only after BOTH profiles.go and install.go are replaced (the old stub's `Rules`/`errNotImplemented` go away) — Tasks 1 and 2 commit together at the end of Task 2.
- **Out of scope:** surfacing install-agent as an in-app dashboard action (later slice).
