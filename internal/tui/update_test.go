package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/monitor"
)

// key builds a KeyMsg for a single key like "q", "?", "enter", "esc", "up".
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// step sends one message and returns the updated Model and any command.
func step(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := step(New(config.Default()), key(k))
		if cmd == nil {
			t.Fatalf("%q produced no command, want quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%q did not produce tea.QuitMsg", k)
		}
	}
}

func TestHelpToggle(t *testing.T) {
	m := New(config.Default())
	if m.showHelp {
		t.Fatal("help should start hidden")
	}
	m, _ = step(m, key("?"))
	if !m.showHelp {
		t.Error("? did not show help")
	}
	m, _ = step(m, key("?"))
	if m.showHelp {
		t.Error("? did not hide help again")
	}
}

func TestWindowSizeStored(t *testing.T) {
	m, _ := step(New(config.Default()), tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.width != 80 {
		t.Errorf("width = %d, want 80", m.width)
	}
}

func sampleLoaded() loadedMsg {
	return loadedMsg{
		report: codex.LogReport{
			CodexDir:   "/home/u/.codex",
			DirExists:  true,
			Files:      []codex.LogFile{{Name: "logs_2.sqlite", Exists: true, Size: 200 * 1024 * 1024}},
			TotalBytes: 200 * 1024 * 1024,
		},
		running:   false,
		supported: true,
		plan: cleaner.Plan{
			Items:      []cleaner.PlanItem{{Name: "logs_2.sqlite", Size: 200 * 1024 * 1024}},
			TotalBytes: 200 * 1024 * 1024,
		},
		backups: nil,
	}
}

func TestLoadedPopulatesDashboard(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	if m.report.TotalBytes != 200*1024*1024 {
		t.Errorf("report not stored: %d", m.report.TotalBytes)
	}
	if !m.deadweight() {
		t.Error("200 MiB should count as deadweight (>= 100 MiB)")
	}
	view := m.View()
	for _, want := range []string{"logs_2.sqlite", "200.0 MiB", "tidy"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard view missing %q:\n%s", want, view)
		}
	}
}

func TestNotDeadweightBelowThreshold(t *testing.T) {
	msg := sampleLoaded()
	msg.report.TotalBytes = 1 * 1024 * 1024 // 1 MiB
	msg.plan.TotalBytes = 1 * 1024 * 1024
	m, _ := step(New(config.Default()), msg)
	if m.deadweight() {
		t.Error("1 MiB should not count as deadweight")
	}
}

func TestLastTidyFromBackups(t *testing.T) {
	when := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	msg := sampleLoaded()
	msg.backups = []cleaner.Backup{{Dir: "/b/20260620-090000", Manifest: cleaner.Manifest{MovedAt: when}}}
	m, _ := step(New(config.Default()), msg)
	got, ok := m.lastTidy()
	if !ok || !got.Equal(when) {
		t.Errorf("lastTidy = %v ok=%v, want %v", got, ok, when)
	}
}

func TestCleanKeyBlockedWhileRunning(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded()) // not running, has plan
	m.running = true                                    // Codex started
	m, cmd := step(m, key("c"))
	if m.state != stateBlocked {
		t.Fatalf("state = %v, want stateBlocked", m.state)
	}
	if cmd != nil {
		t.Error("pressing c while running should not dispatch a command")
	}
}

func TestCleanKeyOpensConfirm(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded()) // not running, 200 MiB plan
	m, _ = step(m, key("c"))
	if m.state != stateConfirmClean {
		t.Fatalf("state = %v, want stateConfirmClean", m.state)
	}
}

func TestConfirmCleanYesDispatchesAndResult(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("c")) // -> confirm
	m, cmd := step(m, key("y"))
	if m.state != stateCleaning {
		t.Fatalf("state = %v, want stateCleaning", m.state)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch cleanCmd")
	}
	// Feed a successful result.
	m, _ = step(m, cleanResultMsg{dest: "/b/20260626-100000", movedBytes: 200 * 1024 * 1024})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if !strings.Contains(m.View(), "200.0 MiB") {
		t.Errorf("result view missing moved size:\n%s", m.View())
	}
}

func TestConfirmCleanNoReturnsToDashboard(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("c"))
	m, _ = step(m, key("n"))
	if m.state != stateDashboard {
		t.Errorf("state = %v, want stateDashboard", m.state)
	}
}

// cleanCmd must NOT touch the engine while Codex is running.
func TestCleanCmdGateRefusesWhileRunning(t *testing.T) {
	origRun, origApply := isCodexRunning, applyPlan
	t.Cleanup(func() { isCodexRunning, applyPlan = origRun, origApply })

	applied := false
	isCodexRunning = func() (bool, error) { return true, nil }
	applyPlan = func(p cleaner.Plan, hold time.Duration) (string, int64, error) { applied = true; return "", 0, nil }

	msg := cleanCmd(config.Default().BinHold())()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("cleanCmd returned %T, want blockedMsg", msg)
	}
	if applied {
		t.Error("applyPlan was called while Codex running — gate bypassed")
	}
}

func loadedWithBackup() loadedMsg {
	msg := sampleLoaded()
	msg.backups = []cleaner.Backup{{
		Dir:      "/home/u/.codex/codexssd-backups/20260626-100000",
		Manifest: cleaner.Manifest{MovedAt: time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)},
	}}
	return msg
}

func TestRestoreKeyOpensListWhenBackupsExist(t *testing.T) {
	m, _ := step(New(config.Default()), loadedWithBackup())
	m, _ = step(m, key("r"))
	if m.state != stateRestoreList {
		t.Fatalf("state = %v, want stateRestoreList", m.state)
	}
	if !strings.Contains(m.View(), "20260626-100000") {
		t.Errorf("restore list view missing backup id:\n%s", m.View())
	}
}

func TestRestoreKeyNoBackupsShowsResult(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded()) // no backups
	m, _ = step(m, key("r"))
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if !strings.Contains(m.View(), "No backups") {
		t.Errorf("expected a 'no backups' message:\n%s", m.View())
	}
}

func TestRestoreConfirmYesDispatches(t *testing.T) {
	m, _ := step(New(config.Default()), loadedWithBackup())
	m, _ = step(m, key("r"))     // list
	m, _ = step(m, key("enter")) // select -> confirm
	if m.state != stateConfirmRestore {
		t.Fatalf("state = %v, want stateConfirmRestore", m.state)
	}
	m, cmd := step(m, key("y"))
	if m.state != stateRestoring {
		t.Fatalf("state = %v, want stateRestoring", m.state)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch restoreCmd")
	}
	m, _ = step(m, restoreResultMsg{id: "20260626-100000"})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
}

// restoreCmd must NOT touch the engine while Codex is running.
func TestRestoreCmdGateRefusesWhileRunning(t *testing.T) {
	origRun, origRestore := isCodexRunning, restoreBackup
	t.Cleanup(func() { isCodexRunning, restoreBackup = origRun, origRestore })

	called := false
	isCodexRunning = func() (bool, error) { return true, nil }
	restoreBackup = func(dir string) error { called = true; return nil }

	msg := restoreCmd("/some/backup")()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("restoreCmd returned %T, want blockedMsg", msg)
	}
	if called {
		t.Error("restoreBackup was called while Codex running — gate bypassed")
	}
}

// cleanCmd moves logs when Codex is not running.
func TestCleanCmdMovesWhenNotRunning(t *testing.T) {
	origDir, origRun := codexDir, isCodexRunning
	t.Cleanup(func() { codexDir, isCodexRunning = origDir, origRun })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs_2.sqlite"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	codexDir = func() (string, error) { return dir, nil }
	isCodexRunning = func() (bool, error) { return false, nil }

	msg := cleanCmd(config.Default().BinHold())()
	res, ok := msg.(cleanResultMsg)
	if !ok {
		t.Fatalf("cleanCmd returned %T, want cleanResultMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("clean failed: %v", res.err)
	}
	if res.movedBytes != 128 {
		t.Errorf("movedBytes = %d, want 128", res.movedBytes)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs_2.sqlite")); !os.IsNotExist(err) {
		t.Error("log was not moved aside")
	}
}

func TestTickKeepsWatchingWithoutChangingState(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded()) // on dashboard
	next, cmd := step(m, tickMsg{})
	if next.state != stateDashboard {
		t.Errorf("tick changed state to %v, want stateDashboard", next.state)
	}
	if cmd == nil {
		t.Error("tick should re-dispatch a command (reload + reschedule)")
	}
}

func TestLoadedMsgDoesNotChangeState(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m.state = stateConfirmClean // user is mid-confirm
	next, _ := step(m, sampleLoaded())
	if next.state != stateConfirmClean {
		t.Errorf("a refresh changed state to %v, want stateConfirmClean", next.state)
	}
}

func TestBannerActionableWhenIdleDeadweight(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded()) // 200 MiB, not running
	if got := m.bannerState(); got != bannerActionable {
		t.Errorf("bannerState = %v, want bannerActionable", got)
	}
	if !strings.Contains(m.View(), "press c to tidy") {
		t.Errorf("actionable banner missing 'press c to tidy':\n%s", m.View())
	}
}

func TestBannerInformationalWhenCodexActive(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m.running = true
	if got := m.bannerState(); got != bannerInformational {
		t.Errorf("bannerState = %v, want bannerInformational", got)
	}
	view := m.View()
	if strings.Contains(view, "press c to tidy") {
		t.Errorf("informational banner should not prompt 'press c' while Codex active:\n%s", view)
	}
}

func TestBannerCalmBelowThreshold(t *testing.T) {
	msg := sampleLoaded()
	msg.report.TotalBytes = 1 * 1024 * 1024
	m, _ := step(New(config.Default()), msg)
	if got := m.bannerState(); got != bannerCalm {
		t.Errorf("bannerState = %v, want bannerCalm", got)
	}
}

func TestReleasedMsgShowsNoteAndReloads(t *testing.T) {
	m := New(config.Default())
	m, cmd := step(m, releasedMsg{ids: []string{"a", "b"}})
	if !strings.Contains(m.releaseNote, "2") {
		t.Errorf("releaseNote = %q, want it to mention 2", m.releaseNote)
	}
	if cmd == nil {
		t.Error("a release should trigger a reload command")
	}
}

func TestDashboardShowsRecyclingBin(t *testing.T) {
	msg := loadedWithBackup() // one backup, HoldUntil 2026-06-26 10:00
	m, _ := step(New(config.Default()), msg)
	view := m.View()
	if !strings.Contains(view, "Recycling bin") {
		t.Errorf("dashboard should show a recycling-bin line:\n%s", view)
	}
}

func TestRestoreListShowsReleaseDate(t *testing.T) {
	m, _ := step(New(config.Default()), loadedWithBackup())
	m, _ = step(m, key("r"))
	if !strings.Contains(m.View(), "releases") {
		t.Errorf("restore list should show each backup's release date:\n%s", m.View())
	}
}

func TestLoadedMsgCarriesMemBytesIntoSample(t *testing.T) {
	msg := sampleLoaded()
	msg.memBytes = 3 << 30 // 3 GiB
	m, _ := step(New(config.Default()), msg)
	if got := m.samples[len(m.samples)-1].MemBytes; got != 3<<30 {
		t.Errorf("samples[last].MemBytes = %d, want %d", got, int64(3<<30))
	}
}

func TestHighRiskDrivesActionableBanner(t *testing.T) {
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	m := New(config.Default())
	// First sample: small, idle.
	first := sampleLoaded()
	first.report.TotalBytes = 10 * 1024 * 1024
	first.plan.TotalBytes = 10 * 1024 * 1024
	first.at = base
	m, _ = step(m, first)
	// Second sample one minute later: +600 MiB → CRITICAL rate, even though size < deadweight.
	second := sampleLoaded()
	second.report.TotalBytes = 610 * 1024 * 1024
	second.plan.TotalBytes = 610 * 1024 * 1024
	second.at = base.Add(time.Minute)
	m, _ = step(m, second)

	if m.assessment.Level != monitor.RiskCritical {
		t.Fatalf("assessment level = %v, want RiskCritical", m.assessment.Level)
	}
	view := m.View()
	if !strings.Contains(view, "Risk:") {
		t.Errorf("dashboard should show a Risk line:\n%s", view)
	}
	if m.bannerState() != bannerActionable {
		t.Errorf("high risk + idle should be actionable, got %v", m.bannerState())
	}
}
