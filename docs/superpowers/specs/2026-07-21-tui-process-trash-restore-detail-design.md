# TUI Round 2: Process List, Backup Original Paths, Trash Destination Detail

## Summary

Round 1 (`docs/superpowers/specs/2026-07-21-tui-visibility-design.md`, merged
via PR #15) closed most of the gap between what CodexSSD tracks and what the
TUI shows. This round closes three more, smaller gaps: the dashboard's "Codex:
running" line doesn't say which process(es); the Confirm Restore screen names
a backup id but not the original file paths it will restore to; and neither
`internal/trash` nor `internal/cleaner`'s release path exposes where files
actually land in the OS Trash, so the TUI's auto-release note can't say where
released backups went. All three are additive, low-risk changes layered onto
already-existing, already-tested engine code — no new packages, no new
safety-relevant logic.

## Component 1: Process list on the dashboard

Add a new seam in `internal/tui/commands.go`:

```go
detectProcesses = codex.DetectProcesses
```

alongside the existing seams (`isCodexRunning`, `codexMemory`, etc.).
`codex.DetectProcesses` already exists (`internal/codex/process.go`) and
returns `([]Process, error)`, where `codex.Process` is a type alias for
`tool.Process{PID int, Name string, Command string}`.

Call it unconditionally in `loadCmd`, alongside the existing `codexMemory()`
fetch, using the same best-effort pattern already used there:

```go
mem, _ := codexMemory() // best-effort; 0 on any error — never blocks the dashboard
procs, _ := detectProcesses() // best-effort; empty slice on any error — never blocks the dashboard
```

Store the result on `Model` as a new field:

```go
processes []codex.Process
```

carried through `loadedMsg` the same way `memBytes` already is.

### Rendering

`internal/tui/view.go`'s `renderDashboard`, in the existing Risk panel switch:

```go
switch {
case !m.supported:
    fmt.Fprint(&risk, "Codex: can't check")
case m.running:
    fmt.Fprint(&risk, "Codex: running")
default:
    fmt.Fprint(&risk, "Codex: not running")
}
```

Render the process list only when `m.running && len(m.processes) > 0`,
replacing the plain `"Codex: running"` line in that case:

- One process → `"Codex: running (PID 1234)"`
- Multiple processes → `"Codex: running (2 processes, PIDs 1234, 5678)"`

If `m.running` is true but `m.processes` is empty (e.g. a race between the two
independent best-effort reads in `loadCmd` — the boolean check and the process
detection can observe different process-table states a few milliseconds
apart), fall back to the current plain `"Codex: running"` text. Never render
an inconsistent or alarming state (e.g. never imply zero processes while also
claiming Codex is running).

### Explicitly out of scope / do not touch

The authoritative `isCodexRunning()` re-checks inside `cleanCmd` and
`restoreCmd` in `internal/tui/commands.go` (the safety-critical "is it
actually closed right now" gates immediately before a mutating action, at
lines ~66 and ~107) must remain byte-for-byte unchanged. This component adds
only an additional best-effort read for passive display in `loadCmd`,
entirely separate from the action-time safety gates. Do not route the new
process list through, or otherwise touch, `cleanCmd`/`restoreCmd`.

## Component 2: Backup `ManifestItem.OriginalPath` on Confirm Restore

`renderConfirmRestore()` in `internal/tui/view.go` currently renders a single
line:

```go
func (m Model) renderConfirmRestore() string {
    id := filepathBase(m.backups[m.selected].Dir)
    body := fmt.Sprintf("Move the logs in backup %s back to your Codex folder?", id)
    return m.screen("Restore backup", body, "y yes · n no")
}
```

Extend `body` to list each file in `m.backups[m.selected].Manifest.Items`
(each a `cleaner.ManifestItem{Name string, OriginalPath string, Size int64}`)
with its `OriginalPath` underneath the question, e.g.:

```
Move the logs in backup 20260601-000000 back to your Codex folder?

logs_2.sqlite       6.0 MiB  → /Users/you/.codex/logs_2.sqlite
logs_2.sqlite-wal   1.8 MiB  → /Users/you/.codex/logs_2.sqlite-wal
```

This is a pure rendering change. `ManifestItem.OriginalPath` is already
loaded onto the model via the existing `m.backups` field (populated by
`listBackups` in `loadCmd`) — no new data fetch, no new state field.

### Explicitly out of scope

The Restore List screen (`renderRestoreList()` — the screen that lists all
backups with one aggregate row each, before a specific backup is selected)
stays exactly as it is today. This per-file `OriginalPath` detail only
appears once a specific backup is selected and the user has moved on to the
Confirm Restore screen.

## Component 3: Trash-destination detail

This component touches `internal/trash` and `internal/cleaner` in addition to
`internal/tui` — it is the only component in this round that changes engine
code rather than purely wiring existing data into the TUI.

### `internal/trash.Move`

Change the signature in `internal/trash/trash.go` from:

```go
func Move(path string) error
```

to:

```go
func Move(path string) (dest string, err error)
```

Both platform branches already compute the final destination internally, so
this is exposing existing information, not adding new logic:

- The darwin branch already calls `moveInto(dir, path)`, which already
  returns `(string, error)` — the target path. Return that value directly.
- The linux branch (`moveLinuxXDG`) currently returns only `error`, but
  internally calls `moveInto(filesDir, path)` and already binds the result to
  a local `target` variable (used to build the `.trashinfo` sidecar file
  name). Thread that `target` value up through `moveLinuxXDG`'s own signature
  (which must also change to `(string, error)`) and back to `Move`.
- The `default:` branch (unsupported platform) returns `("", ErrUnsupported)`.

### `internal/cleaner.Release`

Change `internal/cleaner/release.go` from:

```go
func Release(backupDir string) error
```

to:

```go
func Release(backupDir string) (dest string, err error)
```

propagating `moveToTrash`'s return values directly (`moveToTrash` is the
existing seam, currently `var moveToTrash = trash.Move`; its type changes
accordingly to `func(string) (string, error)`).

Existing tests that stub `moveToTrash` (`internal/cleaner/release_test.go`)
need their fakes' signatures updated to match, e.g.:

```go
moveToTrash = func(p string) (string, error) {
    moved = append(moved, filepath.Base(p))
    return p, nil
}
```

### `internal/cleaner.ReleaseExpired`

Change `internal/cleaner/release.go` from:

```go
func ReleaseExpired(codexDir string, now time.Time) ([]string, error)
```

to:

```go
func ReleaseExpired(codexDir string, now time.Time) (released []string, trashDir string, err error)
```

`trashDir` is `filepath.Dir()` of the destination returned by the **last**
successful `Release` call made during this invocation — a single
representative "where releases go" location, not a per-file list. If nothing
was released during the call (empty backup set, all releases failed before
any succeeded, etc.), `trashDir` is the empty string.

### `internal/tui`

`releaseCmd`/`releasedMsg` in `internal/tui/commands.go` thread the new
`trashDir` value through: `releasedMsg` gains a `trashDir string` field,
populated from `ReleaseExpired`'s second return value in `releaseCmd`.

The existing release note (`m.releaseNote`, set in `internal/tui/update.go`'s
handling of `releasedMsg`, shown on the dashboard after an auto-release on
startup) gets a suffix when something was released:

```
released 2 backup(s) → ~/.Trash
```

with the home directory shortened to `~` for readability. Shortening is
best-effort: if the home directory can't be resolved (e.g. `os.UserHomeDir()`
returns an error), fall back to rendering the raw, unshortened path rather
than failing the note entirely.

The zero-release case (`len(msg.ids) == 0`) keeps rendering exactly as it
does today — no note, or whatever the current no-op behavior is. Never
produce a dangling `"→ "` with an empty path; the arrow-and-destination
suffix must only ever be appended when `trashDir` is non-empty (which is
only possible when at least one backup was actually released).

### `cmd/codexssd/main.go`

The `prune` command's call site for `cleaner.ReleaseExpired` (around line
818) needs updating to match the new three-return-value signature. This
round does **not** change `prune`'s CLI or `--json` output — the new
`trashDir` return value is unused/ignored at that call site, just enough to
keep the code compiling. Do not add a `--json` field or a new flag for it.

## Component 4: Testing & verification

- **Process list:** unit tests on the `loadCmd`/rendering path — process list
  appears once populated, falls back to the plain "running" text when the
  list is empty despite `running == true`, and correct singular vs.
  plural/multiple-PID formatting (one process → `(PID 1234)`; multiple →
  `(N processes, PIDs ...)`).
- **OriginalPath:** extend the Confirm Restore render tests — each
  `ManifestItem`'s `OriginalPath` appears in the rendered body, with multiple
  items all listed.
- **Trash API change:** update existing `internal/trash` tests to assert the
  returned `dest` matches the actual moved-to path, on whichever
  platform-specific branch(es) run on the test machine — follow the existing
  test patterns already in `internal/trash/trash_test.go` for how
  platform-specific behavior is skipped or faked there today.
- **Release/ReleaseExpired:** update `internal/cleaner/release_test.go`'s
  `moveToTrash` stub signature and any assertions that depended on the old
  single-return-value signatures of `Release`/`ReleaseExpired`; add a case
  asserting `ReleaseExpired` returns the correct `trashDir` (the directory of
  the last successful release's destination).
- **`prune` call site:** update `cmd/codexssd/main.go` to match the new
  `ReleaseExpired` signature; verify with `go build ./...` that this
  compiles cleanly. This round does not change `prune`'s user-visible
  output, so no new assertions are needed on `prune`'s CLI/JSON tests beyond
  making sure existing ones still pass.
- **Release note:** TUI test asserting the note reads
  `"released N backup(s) → <path>"` with the home directory shortened to
  `~`, and that the zero-release case still renders correctly with no
  dangling arrow or empty path.
- **Full suite gate (mandatory, per `CLAUDE.md`):**
  `go build ./... && go vet ./... && go test ./... && gofmt -l .` must be
  clean. This round touches `internal/trash` and `internal/cleaner` in
  addition to `internal/tui`, so all three packages — plus anything
  transitively affected, such as `cmd/codexssd` — need to pass.

## Branch / workflow note

This work happens on `feat/tui-process-trash-restore-detail`, cut from
`origin/staging` — which already includes round 1's `feat/tui-visibility`
work (merged via PR #15) and `feat/phase3-4-multi-tool` (merged via PR #14).

Scope is deliberately narrow: `internal/tui`, `internal/trash`,
`internal/cleaner`, and the `prune` call site in `cmd/codexssd/main.go` —
no other files.

Once implemented and verified, this work is intended to be pushed and opened
as a PR against `staging` — but pushing and PR creation require separate,
explicit human confirmation and are out of scope for whoever implements this
spec.
