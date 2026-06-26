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
