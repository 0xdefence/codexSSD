// Package cleaner safely tidies Codex's OWN log files by MOVING them into a
// recoverable recycling bin — never by hard-deleting.
//
// SAFETY (non-negotiable):
//   - It only ever acts on Codex's known log files (codex.LogFileNames).
//   - It MOVES files aside; it never deletes. Permanent deletion must be a
//     separate, explicit user action.
//   - Computing a Plan is read-only; only Plan.Apply touches the filesystem.
package cleaner

import (
	"errors"
	"path/filepath"

	"github.com/0xdefence/codexssd/internal/codex"
)

// errNotImplemented marks scaffolding that has a defined shape but no logic yet.
var errNotImplemented = errors.New("not implemented")

// BackupDirName is the recycling-bin root, created under ~/.codex.
// Moved-aside files land in a timestamped subdirectory beneath it.
const BackupDirName = "codexssd-backups"

// PlanItem describes one file that clean would move aside.
type PlanItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
	Dest string `json:"dest"` // where it would be moved inside the recycling bin
}

// Plan is a dry-run description of what clean WOULD move aside. Building a Plan
// performs no writes.
type Plan struct {
	BackupDir string     `json:"backup_dir"`
	Items     []PlanItem `json:"items"`
}

// PlanCodexLogs inspects Codex's own logs and returns a move-aside plan.
//
// SAFETY: read-only. It only ever considers codex.LogFileNames.
//
// STUB: not implemented yet.
func PlanCodexLogs() (Plan, error) {
	return Plan{}, errNotImplemented
}

// Apply moves the planned items into the recycling bin.
//
// SAFETY: MOVES only, never deletes; refuses any path that is not a known Codex
// log file (see isCodexLog).
//
// STUB: not implemented yet.
func (p Plan) Apply() error {
	return errNotImplemented
}

// isCodexLog is the safety gate: it reports whether path is one of Codex's own
// known log files. The cleaner must never act on anything this rejects.
func isCodexLog(path string) bool {
	base := filepath.Base(path)
	for _, name := range codex.LogFileNames {
		if base == name {
			return true
		}
	}
	return false
}
