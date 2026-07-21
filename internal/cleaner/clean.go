// Package cleaner safely tidies an AI coding tool's OWN files by MOVING them
// into a recoverable recycling bin — never by hard-deleting.
//
// SAFETY (non-negotiable):
//   - It only ever acts on files a tool.Profile's allow-list names (checked via
//     Profile.Allows, re-verified on every move and every restore).
//   - It MOVES files aside; it never deletes one. Permanent deletion must be a
//     separate, explicit user action (not in this build).
//   - Computing a Plan is read-only; only Plan.Apply touches the filesystem.
package cleaner

import (
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/tool"
)

// BackupDirName is the recycling-bin root, created inside each tool's data dir
// so moves stay atomic renames on one filesystem. Canonical value lives in the
// tool package (profiles must exclude it from scans without an import cycle).
const BackupDirName = tool.BackupDirName

// PlanItem describes one file that clean would move aside.
type PlanItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

// Plan is a read-only description of what clean WOULD move aside.
type Plan struct {
	Tool       string     `json:"tool,omitempty"` // "" means codex (pre-multi-tool plans)
	CodexDir   string     `json:"codex_dir"`      // the tool's data dir; JSON name kept for compatibility
	BackupRoot string     `json:"backup_root"`
	Items      []PlanItem `json:"items"`
	TotalBytes int64      `json:"total_bytes"`
}

// Empty reports whether there is nothing to move aside.
func (p Plan) Empty() bool { return len(p.Items) == 0 }

// PlanTool inspects toolDir and returns a move-aside plan for the profile's own
// files. staleAfter gates glob-listed files; fixed files always qualify.
// SAFETY: read-only; items come exclusively from Profile.CleanablePaths.
//
// Plan.Tool is left "" for the codex profile rather than set to "codex":
// empty-means-codex is the documented legacy semantics (see profileFor), and
// codex plans/manifests must stay byte-identical to pre-multi-tool JSON —
// which never had a "tool" key. Every other profile's name is recorded as-is.
func PlanTool(p tool.Profile, toolDir string, now time.Time, staleAfter time.Duration) (Plan, error) {
	planTool := p.Name
	if planTool == "codex" {
		planTool = ""
	}
	plan := Plan{
		Tool:       planTool,
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
