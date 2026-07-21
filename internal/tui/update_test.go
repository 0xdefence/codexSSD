package tui

import (
	"errors"
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
	"github.com/0xdefence/codexssd/internal/notify"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/visibility"
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
	origDir, origRun, origReceipt := codexDir, isCodexRunning, appendReceipt
	t.Cleanup(func() { codexDir, isCodexRunning, appendReceipt = origDir, origRun, origReceipt })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs_2.sqlite"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	codexDir = func() (string, error) { return dir, nil }
	isCodexRunning = func() (bool, error) { return false, nil }
	appendReceipt = func(recorder.Receipt) error { return nil } // never touch the real home dir in tests

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

// cleanCmd records a receipt after a successful clean.
func TestCleanCmdRecordsReceipt(t *testing.T) {
	origDir, origRun, origReceipt := codexDir, isCodexRunning, appendReceipt
	t.Cleanup(func() { codexDir, isCodexRunning, appendReceipt = origDir, origRun, origReceipt })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs_2.sqlite"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	codexDir = func() (string, error) { return dir, nil }
	isCodexRunning = func() (bool, error) { return false, nil }

	var got recorder.Receipt
	var calls int
	appendReceipt = func(r recorder.Receipt) error {
		calls++
		got = r
		return nil
	}

	msg := cleanCmd(config.Default().BinHold())()
	res, ok := msg.(cleanResultMsg)
	if !ok || res.err != nil {
		t.Fatalf("cleanCmd failed: %+v", msg)
	}
	if calls != 1 {
		t.Fatalf("appendReceipt called %d times, want 1", calls)
	}
	if got.Action != "clean" {
		t.Errorf("Action = %q, want clean", got.Action)
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

// TestReleasedMsgNoteIncludesShortenedTrashDir guards the exact release-note
// format: "released N backup(s) → <path>" with the home directory shortened
// to "~".
func TestReleasedMsgNoteIncludesShortenedTrashDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no resolvable home directory on this machine")
	}
	m := New(config.Default())
	m, _ = step(m, releasedMsg{ids: []string{"a", "b"}, trashDir: filepath.Join(home, ".Trash")})
	want := "released 2 backup(s) → " + filepath.Join("~", ".Trash")
	if m.releaseNote != want {
		t.Errorf("releaseNote = %q, want %q", m.releaseNote, want)
	}
}

// TestReleasedMsgZeroReleaseHasNoDanglingArrow guards the zero-release case:
// no note at all, and never a dangling "→ " with an empty path even if a
// trashDir somehow arrived alongside an empty ids slice.
func TestReleasedMsgZeroReleaseHasNoDanglingArrow(t *testing.T) {
	m := New(config.Default())
	m, _ = step(m, releasedMsg{trashDir: "/should/be/ignored"})
	if m.releaseNote != "" {
		t.Errorf("releaseNote = %q, want empty when nothing was released", m.releaseNote)
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
	if !strings.Contains(view, "CRITICAL") {
		t.Errorf("dashboard should show the CRITICAL risk level:\n%s", view)
	}
	if m.bannerState() != bannerActionable {
		t.Errorf("high risk + idle should be actionable, got %v", m.bannerState())
	}
}

// --- Info screen: navigation ---

func TestInfoKeyOpensInfoScreenAndDispatchesInfoCmd(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, cmd := step(m, key("i"))
	if m.state != stateInfo {
		t.Fatalf("state = %v, want stateInfo", m.state)
	}
	if m.infoLoaded {
		t.Error("infoLoaded should start false when the info screen is entered")
	}
	if cmd == nil {
		t.Fatal("pressing i should dispatch infoCmd")
	}
}

func TestInfoMsgPopulatesScreenAndSetsLoaded(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("i"))

	rep := self.Report{Mode: "low-write", StateDir: "/home/u/.codexssd", HistoryBytes: 512, Records: 2, LastAction: "restore"}
	disk := visibility.Report{Dir: "/home/u/.codex", DirExists: true, Entries: []visibility.Entry{}}
	m, cmd := step(m, infoMsg{self: rep, disk: disk})

	if !m.infoLoaded {
		t.Fatal("infoLoaded should be true after infoMsg")
	}
	if m.selfReport != rep {
		t.Errorf("selfReport = %+v, want %+v", m.selfReport, rep)
	}
	if m.diskReport.Dir != disk.Dir {
		t.Errorf("diskReport.Dir = %q, want %q", m.diskReport.Dir, disk.Dir)
	}
	if cmd != nil {
		t.Error("infoMsg should not itself dispatch another command")
	}
}

func TestInfoEscReturnsToDashboardAndReloads(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("i"))
	m, cmd := step(m, key("esc"))
	if m.state != stateDashboard {
		t.Fatalf("state = %v, want stateDashboard", m.state)
	}
	if cmd == nil {
		t.Error("esc from the info screen should trigger a reload (loadCmd)")
	}
}

// --- Info screen: infoCmd against seams (no real ~/.codex or ~/.codexssd touched) ---

func TestInfoCmdGathersSelfAndDiskReports(t *testing.T) {
	origState, origCodexDir := recorderStateDir, codexDir
	t.Cleanup(func() { recorderStateDir, codexDir = origState, origCodexDir })

	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, recorder.FileName), make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	recorderStateDir = func() (string, error) { return stateDir, nil }

	codexD := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexD, "logs_2.sqlite"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	codexDir = func() (string, error) { return codexD, nil }

	msg := infoCmd(30 * 24 * time.Hour)()
	got, ok := msg.(infoMsg)
	if !ok {
		t.Fatalf("infoCmd returned %T, want infoMsg", msg)
	}
	if got.selfErr != nil {
		t.Fatalf("selfErr = %v, want nil", got.selfErr)
	}
	if got.self.StateDir != stateDir {
		t.Errorf("self.StateDir = %q, want %q", got.self.StateDir, stateDir)
	}
	if got.self.HistoryBytes != 10 {
		t.Errorf("self.HistoryBytes = %d, want 10", got.self.HistoryBytes)
	}
	if !got.disk.DirExists {
		t.Error("disk.DirExists should be true")
	}
	if got.disk.TotalBytes != 128 {
		t.Errorf("disk.TotalBytes = %d, want 128", got.disk.TotalBytes)
	}
}

func TestInfoCmdSurfacesRecorderDirError(t *testing.T) {
	origState := recorderStateDir
	t.Cleanup(func() { recorderStateDir = origState })

	wantErr := errors.New("no home dir")
	recorderStateDir = func() (string, error) { return "", wantErr }

	msg := infoCmd(time.Hour)()
	got, ok := msg.(infoMsg)
	if !ok {
		t.Fatalf("infoCmd returned %T, want infoMsg", msg)
	}
	if !errors.Is(got.selfErr, wantErr) {
		t.Errorf("selfErr = %v, want %v", got.selfErr, wantErr)
	}
}

// --- Notifications: escalation logic and firing ---

func TestEscalatedToAlarming(t *testing.T) {
	cases := []struct {
		name          string
		last, current monitor.Risk
		want          bool
	}{
		{"low to medium is not alarming", monitor.RiskLow, monitor.RiskMedium, false},
		{"low to high is an escalation", monitor.RiskLow, monitor.RiskHigh, true},
		{"low to critical is an escalation", monitor.RiskLow, monitor.RiskCritical, true},
		{"medium to high is an escalation", monitor.RiskMedium, monitor.RiskHigh, true},
		{"high to critical is an escalation", monitor.RiskHigh, monitor.RiskCritical, true},
		{"high to high (no change) is not alarming", monitor.RiskHigh, monitor.RiskHigh, false},
		{"critical to high (de-escalation) is not alarming", monitor.RiskCritical, monitor.RiskHigh, false},
		{"critical to low (de-escalation) is not alarming", monitor.RiskCritical, monitor.RiskLow, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := escalatedToAlarming(c.last, c.current); got != c.want {
				t.Errorf("escalatedToAlarming(%v, %v) = %v, want %v", c.last, c.current, got, c.want)
			}
		})
	}
}

func TestLoadedMsgDispatchesNotifyCmdOnEscalation(t *testing.T) {
	origNotify := notifyFn
	t.Cleanup(func() { notifyFn = origNotify })

	called := make(chan struct{}, 1)
	notifyFn = func(title, body string) error { called <- struct{}{}; return nil }

	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	m := New(config.Default())
	m, _ = step(m, loadedAt(base, 10*1024*1024)) // baseline: LOW
	m, cmd := step(m, loadedAt(base.Add(time.Minute), 610*1024*1024))

	if m.assessment.Level != monitor.RiskCritical {
		t.Fatalf("assessment level = %v, want RiskCritical (precondition for this test)", m.assessment.Level)
	}
	if cmd == nil {
		t.Fatal("escalation into CRITICAL should dispatch a notify command")
	}
	cmd() // run the returned tea.Cmd, which fires notifyFn in its own goroutine

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("notifyFn was never invoked by the dispatched command")
	}
}

func TestLoadedMsgNoNotifyCmdWithoutEscalation(t *testing.T) {
	origNotify := notifyFn
	t.Cleanup(func() { notifyFn = origNotify })
	notifyFn = func(string, string) error { t.Error("notifyFn should not be called"); return nil }

	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	m := New(config.Default())
	// Flat, low-activity samples: never crosses into HIGH/CRITICAL.
	m, cmd := step(m, loadedAt(base, 10*1024*1024))
	if cmd != nil {
		t.Error("the baseline load should not dispatch a notify command")
	}
	m, cmd = step(m, loadedAt(base.Add(time.Minute), 11*1024*1024))
	if cmd != nil {
		t.Error("a quiet load should not dispatch a notify command")
	}
}

func TestLoadedMsgNoNotifyCmdWhenConfigDisabled(t *testing.T) {
	origNotify := notifyFn
	t.Cleanup(func() { notifyFn = origNotify })
	notifyFn = func(string, string) error {
		t.Error("notifyFn should not be called when notifications are disabled")
		return nil
	}

	cfg := config.Default()
	cfg.Notifications = false
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	m := New(cfg)
	m, _ = step(m, loadedAt(base, 10*1024*1024))
	m, cmd := step(m, loadedAt(base.Add(time.Minute), 610*1024*1024))
	if m.assessment.Level != monitor.RiskCritical {
		t.Fatalf("assessment level = %v, want RiskCritical (precondition for this test)", m.assessment.Level)
	}
	if cmd != nil {
		t.Error("notifications disabled in config: loadedMsg should not dispatch a notify command")
	}
}

func TestNotifyCmdSwallowsErrorsIncludingUnsupported(t *testing.T) {
	origNotify := notifyFn
	t.Cleanup(func() { notifyFn = origNotify })

	done := make(chan struct{}, 1)
	notifyFn = func(title, body string) error { done <- struct{}{}; return notify.ErrUnsupported }

	cmd := notifyCmd(monitor.Assessment{Level: monitor.RiskHigh, Reasons: []string{"writing 200 MB/min"}})
	if msg := cmd(); msg != nil {
		t.Errorf("notifyCmd's returned message should be nil (discarded), got %v", msg)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notifyFn was not invoked")
	}
}
