package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
)

// Engine seams. They default to the real engine functions and are overridden in
// tests so the commands (including the running-gate) are hermetically testable.
var (
	codexDir       = codex.Dir
	scanLogs       = codex.ScanLogs
	isCodexRunning = codex.IsCodexRunning
	planLogs       = cleaner.PlanCodexLogs
	listBackups    = cleaner.ListBackups
	restoreBackup  = cleaner.Restore
	// applyPlan moves a plan's logs aside and reports (dest, bytesMoved, err).
	applyPlan = func(p cleaner.Plan) (string, int64, error) {
		dest, err := p.Apply(time.Now())
		return dest, p.TotalBytes, err
	}
)

// cleanResultMsg reports the outcome of a tidy.
type cleanResultMsg struct {
	dest       string
	movedBytes int64
	err        error
}

// blockedMsg means an action was refused because Codex is running or the
// platform can't be checked.
type blockedMsg struct {
	reason string
}

// cleanCmd re-checks that Codex is stopped (authoritative gate), then moves the
// logs aside. It NEVER calls applyPlan while Codex is running.
func cleanCmd() tea.Msg {
	running, runErr := isCodexRunning()
	if runErr == codex.ErrUnsupportedPlatform {
		return blockedMsg{reason: "This platform can't verify Codex is closed, so tidying is disabled here."}
	}
	if runErr != nil {
		return cleanResultMsg{err: runErr}
	}
	if running {
		return blockedMsg{reason: "Codex appears to be running. Close it first, then try again."}
	}
	dir, err := codexDir()
	if err != nil {
		return cleanResultMsg{err: err}
	}
	plan, err := planLogs(dir)
	if err != nil {
		return cleanResultMsg{err: err}
	}
	if plan.Empty() {
		return cleanResultMsg{dest: "", movedBytes: 0}
	}
	dest, moved, err := applyPlan(plan)
	return cleanResultMsg{dest: dest, movedBytes: moved, err: err}
}

// loadedMsg carries a full status snapshot for the dashboard.
type loadedMsg struct {
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup
}

// loadCmd gathers the dashboard snapshot (read-only).
func loadCmd() tea.Msg {
	dir, err := codexDir()
	if err != nil {
		return loadedMsg{loadErr: err}
	}
	report := scanLogs(dir)
	running, runErr := isCodexRunning()
	supported := runErr != codex.ErrUnsupportedPlatform
	plan, _ := planLogs(dir)
	backups, _ := listBackups(dir)
	return loadedMsg{
		report: report, running: running, supported: supported,
		runErr: runErr, plan: plan, backups: backups,
	}
}
