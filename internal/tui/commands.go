package tui

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/notify"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/visibility"
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
	// recorderStateDir resolves CodexSSD's own state dir (~/.codexssd), the
	// input to self.Measure.
	recorderStateDir = recorder.Dir
	// measureSelf and scanVisibility back the Info screen; overridden in tests
	// with t.TempDir()-backed fakes so no real ~/.codex or ~/.codexssd is touched.
	measureSelf    = self.Measure
	scanVisibility = visibility.Scan
	// notifyFn fires a desktop notification. Overridden in tests so no real
	// notification UI is ever spawned.
	notifyFn = notify.Notify
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

// infoMsg carries the Info screen's snapshot: CodexSSD's own footprint and the
// ~/.codex disk breakdown. Both come from already-tested, read-only packages —
// this just wires them into the TUI.
type infoMsg struct {
	self    self.Report
	selfErr error
	disk    visibility.Report
}

// infoCmd gathers the Info screen's snapshot (100% read-only): CodexSSD's own
// footprint via self.Measure(recorder.Dir()), and a full ~/.codex disk
// breakdown via visibility.Scan. staleAfter comes straight from config so the
// disk report's STALE flags match the same threshold the rest of the tool uses.
func infoCmd(staleAfter time.Duration) tea.Cmd {
	return func() tea.Msg {
		var rep self.Report
		var selfErr error
		if stateDir, err := recorderStateDir(); err != nil {
			selfErr = err
		} else {
			rep, selfErr = measureSelf(stateDir)
		}

		var disk visibility.Report
		if dir, err := codexDir(); err == nil {
			disk = scanVisibility(dir, time.Now(), staleAfter)
		}

		return infoMsg{self: rep, selfErr: selfErr, disk: disk}
	}
}

// escalatedToAlarming reports whether risk just crossed up into HIGH/CRITICAL
// territory — the exact escalation condition cmd/codexssd/watch.go uses
// (a.Level >= monitor.RiskHigh && a.Level > last), promoted to a pure function
// so it is testable without a fake notification channel.
func escalatedToAlarming(last, current monitor.Risk) bool {
	return current >= monitor.RiskHigh && current > last
}

// notifyCmd fires a best-effort desktop notification about an escalated risk
// assessment. CLAUDE.md safety rule 6: notifications are fire-and-forget — a
// failure or delay here must never block, slow, or fail the dashboard's render
// loop, so this spawns its own goroutine and discards the result unconditionally
// (any error, including notify.ErrUnsupported, is swallowed — same as watch.go).
func notifyCmd(a monitor.Assessment) tea.Cmd {
	return func() tea.Msg {
		go func() {
			body := "Codex disk/memory activity looks alarming."
			if len(a.Reasons) > 0 {
				body = a.Reasons[0]
			}
			_ = notifyFn("CodexSSD: "+a.Level.String(), body)
		}()
		return nil
	}
}
