// Package tui is CodexSSD's interactive app: `codexssd` with no subcommand opens
// this screen. It is a thin Bubble Tea layer over the safety-tested engine
// packages (internal/codex, internal/cleaner) — it adds no file-mutating logic
// of its own.
//
// DEPENDENCY BOUNDARY: the charmbracelet libraries are imported ONLY in this
// package. The engine packages remain standard-library only.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/tool"
	"github.com/0xdefence/codexssd/internal/visibility"
)

// deadweightThreshold is the total Codex-log size at or above which the
// dashboard emphasizes that tidying is worthwhile.
const deadweightThreshold int64 = 100 * 1024 * 1024 // 100 MiB

// maxSamples bounds the in-memory sample window the monitor evaluates.
const maxSamples = 20

// pollInterval is how often the open dashboard re-checks ~/.codex (read-only).
const pollInterval = 30 * time.Second

// state is the current screen.
type state int

const (
	stateDashboard state = iota
	stateConfirmClean
	stateCleaning
	stateRestoreList
	stateConfirmRestore
	stateRestoring
	stateResult
	stateBlocked
	stateError
	stateInfo
	stateClaude
	stateClaudeConfirmClean
	stateClaudeRestoreList
	stateClaudeConfirmRestore
)

// Model is the whole application state. Fields beyond the skeleton are populated
// by later tasks (load, clean, restore); unused-but-declared fields are fine.
type Model struct {
	state    state
	showHelp bool
	width    int
	height   int
	cfg      config.Config

	// status (populated by loadCmd in Task 2)
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup
	memBytes  int64           // total Codex RSS (0 when unknown)
	processes []codex.Process // running Codex-like processes (empty when none/unknown)

	// Claude Code status (populated by loadClaudeCmd), a parallel set of fields
	// alongside the Codex ones above — the Codex fields are never touched by
	// Claude-side loading/actions, and vice versa.
	claudeDir       string
	claudeLoadErr   error
	claudeRunning   bool
	claudeSupported bool
	claudeRunErr    error
	claudeProcesses []tool.Process
	claudeCleanable []tool.FoundFile
	claudePlan      cleaner.Plan
	claudeBackups   []cleaner.Backup

	// returnState/workingLabel generalize the shared "please wait"/result/
	// blocked screens across both tools: set immediately before every
	// clean/restore dispatch so those screens know whether esc/enter goes back
	// to stateDashboard (+ loadCmd) or stateClaude (+ loadClaudeCmd), and what
	// the working screen's label should read while the action is in flight.
	returnState  state
	workingLabel string

	// monitor (write-activity risk)
	samples    []monitor.Sample
	assessment monitor.Assessment

	// session tracking (for the receipt written when the dashboard is quit).
	// These live outside the bounded sample window so they reflect the WHOLE
	// session, not just the trailing ~10 minutes the ring buffer retains.
	startedAt  time.Time    // first successful load of this session
	startBytes int64        // total log size at the first load
	peakRate   float64      // highest MB/min seen this session
	peakRisk   monitor.Risk // highest risk level seen this session

	// interaction state
	selected      int    // restore list cursor
	resultMsg     string // success text on the result screen
	resultErr     error  // error on the result screen
	blockedReason string // why an action was refused
	releaseNote   string // note shown after an auto-release on start

	// info screen (populated lazily by infoCmd when the screen is entered)
	infoLoaded bool
	selfReport self.Report
	selfErr    error
	diskReport visibility.Report

	// claudeLoaded gates the Claude screen's "loading…" state, same pattern as
	// infoLoaded: pressing l dispatches a fresh loadClaudeCmd and this is reset
	// to false until loadedClaudeMsg arrives, so the detail screen is never
	// stale relative to when it was opened.
	claudeLoaded bool
}

// New returns the initial model configured with cfg.
func New(cfg config.Config) Model {
	return Model{state: stateDashboard, cfg: cfg}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadCmd, loadClaudeCmd(m.cfg.StaleAfter()), tickCmd(m.cfg.PollInterval()), releaseCmd)
}

// deadweight reports whether the Codex logs are large enough to emphasize.
func (m Model) deadweight() bool {
	return m.report.TotalBytes >= deadweightThreshold
}

// sessionReceipt summarizes this dashboard session for the JSONL history. It is
// pure so it can be tested without launching the program. DiskWritten is the log
// growth observed across the session, clamped so a mid-session tidy (which
// shrinks the logs) never reports a negative amount.
func (m Model) sessionReceipt(now time.Time) recorder.Receipt {
	dur := 0.0
	if !m.startedAt.IsZero() {
		dur = now.Sub(m.startedAt).Seconds()
	}
	// Growth is measured from the session's first load (startBytes) to the most
	// recent report — NOT off m.samples, which is a bounded ring buffer whose
	// oldest entry is not the session start for any session open longer than
	// the window. Clamped so a mid-session tidy (logs shrink) never goes negative.
	grew := m.report.TotalBytes - m.startBytes
	if grew < 0 {
		grew = 0
	}
	return recorder.Receipt{
		At:           now,
		Action:       "session",
		DurationSec:  dur,
		DiskWritten:  grew,
		PeakMBPerMin: m.peakRate,
		Risk:         m.peakRisk.String(),
	}
}

// lastTidy returns the most recent backup time, if any backups exist.
func (m Model) lastTidy() (time.Time, bool) { return lastTidyOf(m.backups) }

// soonestRelease returns the earliest upcoming backup release time, if any.
func (m Model) soonestRelease() (time.Time, bool) { return soonestReleaseOf(m.backups) }

// claudeLastTidy is lastTidy's Claude-backup counterpart.
func (m Model) claudeLastTidy() (time.Time, bool) { return lastTidyOf(m.claudeBackups) }

// claudeSoonestRelease is soonestRelease's Claude-backup counterpart.
func (m Model) claudeSoonestRelease() (time.Time, bool) { return soonestReleaseOf(m.claudeBackups) }

// lastTidyOf returns the most recent backup time in backups, if any exist.
// Shared by lastTidy/claudeLastTidy so the two tools' recycling-bin summaries
// stay byte-identical in behavior.
func lastTidyOf(backups []cleaner.Backup) (time.Time, bool) {
	var newest time.Time
	found := false
	for _, b := range backups {
		if b.Manifest.MovedAt.After(newest) {
			newest = b.Manifest.MovedAt
			found = true
		}
	}
	return newest, found
}

// soonestReleaseOf returns the earliest upcoming release time in backups, if
// any exist. Shared by soonestRelease/claudeSoonestRelease.
func soonestReleaseOf(backups []cleaner.Backup) (time.Time, bool) {
	var soonest time.Time
	found := false
	for _, b := range backups {
		if !found || b.Manifest.HoldUntil.Before(soonest) {
			soonest = b.Manifest.HoldUntil
			found = true
		}
	}
	return soonest, found
}

// Run launches the interactive app. Called by main when no subcommand is given.
// Never called from tests.
func Run() error {
	// A malformed config must never block the dashboard from opening — the
	// error is ignored because LoadDefault always returns usable defaults.
	cfg, _ := config.LoadDefault()
	final, err := tea.NewProgram(New(cfg), tea.WithAltScreen()).Run()
	// On quit, record a one-line session receipt (best-effort — a failed write
	// must never mask the program's own exit status).
	if m, ok := final.(Model); ok {
		_ = appendReceipt(m.sessionReceipt(time.Now()))
	}
	return err
}
