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
	cfg      config.Config

	// status (populated by loadCmd in Task 2)
	report    codex.LogReport
	running   bool
	supported bool
	runErr    error
	loadErr   error
	plan      cleaner.Plan
	backups   []cleaner.Backup
	memBytes  int64 // total Codex RSS (0 when unknown)

	// monitor (write-activity risk)
	samples    []monitor.Sample
	assessment monitor.Assessment

	// interaction state
	selected      int    // restore list cursor
	resultMsg     string // success text on the result screen
	resultErr     error  // error on the result screen
	blockedReason string // why an action was refused
	releaseNote   string // note shown after an auto-release on start
}

// New returns the initial model configured with cfg.
func New(cfg config.Config) Model {
	return Model{state: stateDashboard, cfg: cfg}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadCmd, tickCmd(m.cfg.PollInterval()), releaseCmd)
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

// soonestRelease returns the earliest upcoming backup release time, if any.
func (m Model) soonestRelease() (time.Time, bool) {
	var soonest time.Time
	found := false
	for _, b := range m.backups {
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
	_, err := tea.NewProgram(New(cfg), tea.WithAltScreen()).Run()
	return err
}
