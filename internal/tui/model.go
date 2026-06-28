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
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/recorder"
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
)

// Model is the whole application state. Fields beyond the skeleton are populated
// by later tasks (load, clean, restore); unused-but-declared fields are fine.
type Model struct {
	state    state
	showHelp bool
	width    int

	// status (populated by loadCmd in Task 2)
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup

	// monitor (write-activity risk)
	samples    []monitor.Sample
	assessment monitor.Assessment

	// session tracking
	startedAt time.Time    // first load time of this session
	peakRate  float64      // peak MB/min seen this session
	peakRisk  monitor.Risk // peak risk level seen this session

	// interaction state
	selected      int    // restore list cursor
	resultMsg     string // success text on the result screen
	resultErr     error  // error on the result screen
	blockedReason string // why an action was refused
}

// New returns the initial model.
func New() Model {
	return Model{state: stateDashboard}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadCmd, tickCmd())
}

// writeReceipt records a session receipt; a seam so tests can stub it.
var writeReceipt = recorder.Append

// sessionReceipt summarizes this session for the JSONL history.
func (m Model) sessionReceipt(now time.Time) recorder.Receipt {
	dur := 0.0
	if !m.startedAt.IsZero() {
		dur = now.Sub(m.startedAt).Seconds()
	}
	var grew int64
	if len(m.samples) >= 2 {
		grew = m.samples[len(m.samples)-1].TotalBytes - m.samples[0].TotalBytes
		if grew < 0 {
			grew = 0
		}
	}
	return recorder.Receipt{
		At:           now,
		DurationSec:  dur,
		DiskWritten:  grew,
		PeakMBPerMin: m.peakRate,
		Risk:         m.peakRisk.String(),
	}
}

// deadweight reports whether the Codex logs are large enough to emphasize.
func (m Model) deadweight() bool {
	return m.report.TotalBytes >= deadweightThreshold
}

// lastTidy returns the most recent backup time, if any backups exist.
func (m Model) lastTidy() (time.Time, bool) {
	var newest time.Time
	found := false
	for _, b := range m.backups {
		if b.Manifest.MovedAt.After(newest) {
			newest = b.Manifest.MovedAt
			found = true
		}
	}
	return newest, found
}

// Run launches the interactive app. Called by main when no subcommand is given.
// Never called from tests.
func Run() error {
	final, err := tea.NewProgram(New(), tea.WithAltScreen()).Run()
	if m, ok := final.(Model); ok {
		_ = writeReceipt(m.sessionReceipt(time.Now()))
	}
	return err
}
