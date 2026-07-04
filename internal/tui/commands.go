package tui

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/recorder"
)

// Engine seams. They default to the real engine functions and are overridden in
// tests so the commands (including the running-gate) are hermetically testable.
var (
	codexDir       = codex.Dir
	scanLogs       = codex.ScanLogs
	isCodexRunning = codex.IsCodexRunning
	codexMemory    = codex.ProcessMemory
	planLogs       = cleaner.PlanCodexLogs
	listBackups    = cleaner.ListBackups
	restoreBackup  = cleaner.Restore
	// applyPlan moves a plan's logs aside, holding them for hold before they
	// become eligible for release, and reports (dest, bytesMoved, err).
	applyPlan = func(p cleaner.Plan, hold time.Duration) (string, int64, error) {
		dest, err := p.ApplyWithHold(time.Now(), hold)
		return dest, p.TotalBytes, err
	}
	// appendReceipt records a session receipt. The TUI has no stderr channel
	// worth interrupting for, so callers ignore the error entirely.
	appendReceipt = recorder.Append
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
// logs aside held for hold. It NEVER calls applyPlan while Codex is running.
func cleanCmd(hold time.Duration) tea.Cmd {
	return func() tea.Msg {
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
		dest, moved, err := applyPlan(plan, hold)
		if err == nil {
			// Best-effort bookkeeping; the TUI has no stderr channel to warn on, so
			// the error is ignored entirely rather than surfaced or retried.
			_ = appendReceipt(recorder.Receipt{At: time.Now(), Action: "clean", BytesMoved: moved, FilesChanged: len(plan.Items), BackupID: filepathBase(dest)})
		}
		return cleanResultMsg{dest: dest, movedBytes: moved, err: err}
	}
}

// restoreResultMsg reports the outcome of a restore.
type restoreResultMsg struct {
	id  string
	err error
}

// restoreCmd re-checks that Codex is stopped, then restores the backup at dir.
// It NEVER calls restoreBackup while Codex is running.
func restoreCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		running, runErr := isCodexRunning()
		if runErr == codex.ErrUnsupportedPlatform {
			return blockedMsg{reason: "This platform can't verify Codex is closed, so restoring is disabled here."}
		}
		if runErr != nil {
			return restoreResultMsg{err: runErr}
		}
		if running {
			return blockedMsg{reason: "Codex appears to be running. Close it first, then try again."}
		}
		err := restoreBackup(dir)
		if err == nil {
			// Best-effort bookkeeping; ignored entirely — see cleanCmd.
			_ = appendReceipt(recorder.Receipt{At: time.Now(), Action: "restore", BackupID: filepathBase(dir)})
		}
		return restoreResultMsg{id: filepathBase(dir), err: err}
	}
}

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
	if len(released) > 0 {
		// Best-effort bookkeeping; ignored entirely — see cleanCmd.
		_ = appendReceipt(recorder.Receipt{At: time.Now(), Action: "prune", BackupIDs: released})
	}
	return releasedMsg{ids: released}
}

// filepathBase wraps filepath.Base for use in view rendering.
func filepathBase(p string) string { return filepath.Base(p) }

// loadedMsg carries a full status snapshot for the dashboard.
type loadedMsg struct {
	at        time.Time
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup
	memBytes  int64 // total Codex RSS (0 when unknown)
}

// walBytes returns the size of the -wal file from a scan report (0 if absent).
func walBytes(r codex.LogReport) int64 {
	for _, f := range r.Files {
		if f.Name == "logs_2.sqlite-wal" && f.Exists {
			return f.Size
		}
	}
	return 0
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
	mem, _ := codexMemory() // best-effort; 0 on any error — never blocks the dashboard
	return loadedMsg{
		at: time.Now(), report: report, running: running, supported: supported,
		runErr: runErr, plan: plan, backups: backups, memBytes: mem,
	}
}

// tickMsg fires on the poll interval to keep the dashboard live.
type tickMsg struct{}

// tickCmd schedules the next poll tick after interval.
func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return tickMsg{} })
}
