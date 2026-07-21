package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/tool"
)

// claudeLoadedMsg returns a sample loadedClaudeMsg: not running, supported,
// one cleanable file, no backups. Mirrors sampleLoaded()'s role for Codex.
func claudeLoadedMsg() loadedClaudeMsg {
	return loadedClaudeMsg{
		dir:       "/home/u/.claude",
		running:   false,
		supported: true,
		cleanable: []tool.FoundFile{{Rel: "projects/p/s.jsonl", Size: 6 * 1024 * 1024}},
		plan: cleaner.Plan{
			Tool:       "claude",
			Items:      []cleaner.PlanItem{{Name: "projects/p/s.jsonl", Size: 6 * 1024 * 1024}},
			TotalBytes: 6 * 1024 * 1024,
		},
	}
}

// claudeLoadedWithBackup is claudeLoadedMsg with one recoverable backup.
func claudeLoadedWithBackup() loadedClaudeMsg {
	msg := claudeLoadedMsg()
	msg.backups = []cleaner.Backup{{
		Dir: "/home/u/.claude/codexssd-backups/20260626-100000",
		Manifest: cleaner.Manifest{
			Tool:      "claude",
			MovedAt:   time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC),
			HoldUntil: time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC),
			Items: []cleaner.ManifestItem{
				{Name: "projects/p/s.jsonl", OriginalPath: "/Users/you/.claude/projects/p/s.jsonl", Size: 6 * 1024 * 1024},
			},
		},
	}}
	return msg
}

// dashboardWithClaude builds a Model that already has a Claude snapshot loaded
// and is sitting on stateClaude — bypassing the "l" keypress (which would
// dispatch a real loadClaudeCmd) so tests can drive the Claude screen
// hermetically from a known msg.
func dashboardWithClaude(t *testing.T, msg loadedClaudeMsg) Model {
	t.Helper()
	m, _ := step(New(config.Default()), msg)
	m.state = stateClaude
	m.claudeLoaded = true
	return m
}

// --- Dashboard panel ---

func TestClaudeDashboardPanelShowsCleanableSummary(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, claudeLoadedMsg())
	m.width = 100
	out := m.View()
	for _, want := range []string{"/home/u/.claude", "1 stale file(s)", "6.0 MiB cleanable", "not running"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q; got:\n%s", want, out)
		}
	}
}

func TestClaudeDashboardPanelNothingStale(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	msg := claudeLoadedMsg()
	msg.cleanable = nil
	msg.plan = cleaner.Plan{}
	m, _ = step(m, msg)
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "0 stale file(s), 0 B cleanable") {
		t.Errorf("dashboard missing zero-cleanable summary; got:\n%s", out)
	}
}

func TestClaudeDashboardPanelRunningWithPID(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	msg := claudeLoadedMsg()
	msg.running = true
	msg.processes = []tool.Process{{PID: 4321, Name: "claude"}}
	m, _ = step(m, msg)
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "running (PID 4321)") {
		t.Errorf("dashboard missing singular PID detail; got:\n%s", out)
	}
	if strings.Contains(out, "Claude Code: running") {
		t.Errorf("Claude panel line should not repeat a tool-name prefix (the panel title already says it); got:\n%s", out)
	}

	msg.processes = []tool.Process{{PID: 4321}, {PID: 5678}}
	m, _ = step(m, msg)
	m.width = 100
	out = m.View()
	if !strings.Contains(out, "running (2 processes, PIDs 4321, 5678)") {
		t.Errorf("dashboard missing plural PID detail; got:\n%s", out)
	}
}

func TestClaudeDashboardPanelLoadError(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, loadedClaudeMsg{loadErr: errors.New("permission denied")})
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "Could not read Claude Code's folder") {
		t.Errorf("dashboard missing Claude load-error text; got:\n%s", out)
	}
}

func TestClaudeFooterAndHelpMentionClaudeKey(t *testing.T) {
	m := New(config.Default())
	if !strings.Contains(m.footer(), "l claude") {
		t.Errorf("footer should mention the l key; got %q", m.footer())
	}
	if !strings.Contains(m.renderHelp(), "l    open the Claude Code screen") {
		t.Errorf("help screen should document the l key; got:\n%s", m.renderHelp())
	}
}

// --- Claude screen: navigation and lazy load ---

func TestClaudeKeyOpensClaudeScreenAndDispatchesLoadClaudeCmd(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, cmd := step(m, key("l"))
	if m.state != stateClaude {
		t.Fatalf("state = %v, want stateClaude", m.state)
	}
	if m.claudeLoaded {
		t.Error("claudeLoaded should start false when the Claude screen is entered")
	}
	if cmd == nil {
		t.Fatal("pressing l should dispatch loadClaudeCmd")
	}
}

func TestLoadedClaudeMsgPopulatesScreenAndSetsLoaded(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("l"))

	msg := claudeLoadedMsg()
	m, cmd := step(m, msg)
	if !m.claudeLoaded {
		t.Fatal("claudeLoaded should be true after loadedClaudeMsg")
	}
	if m.claudeDir != msg.dir {
		t.Errorf("claudeDir = %q, want %q", m.claudeDir, msg.dir)
	}
	if m.claudePlan.TotalBytes != msg.plan.TotalBytes {
		t.Errorf("claudePlan.TotalBytes = %d, want %d", m.claudePlan.TotalBytes, msg.plan.TotalBytes)
	}
	if cmd != nil {
		t.Error("loadedClaudeMsg should not itself dispatch another command")
	}
}

func TestClaudeScreenShowsLoadingBeforeDataArrives(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m.width = 100
	m, _ = step(m, key("l"))
	out := m.View()
	if !strings.Contains(out, "loading") {
		t.Errorf("Claude screen should show a loading indicator before loadedClaudeMsg arrives; got:\n%s", out)
	}
}

func TestClaudeScreenItemizesCleanableFiles(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedMsg())
	m.width = 100
	out := m.View()
	for _, want := range []string{"/home/u/.claude", "projects/p/s.jsonl", "6.0 MiB", "still in use"} {
		if !strings.Contains(out, want) {
			t.Errorf("Claude screen missing %q; got:\n%s", want, out)
		}
	}
}

func TestClaudeScreenNothingStaleMessage(t *testing.T) {
	msg := claudeLoadedMsg()
	msg.cleanable = nil
	msg.plan = cleaner.Plan{}
	m := dashboardWithClaude(t, msg)
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "Nothing stale to report right now") {
		t.Errorf("Claude screen missing 'nothing stale' message; got:\n%s", out)
	}
}

func TestClaudeEscReturnsToDashboardUnconditionally(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedMsg())
	m, cmd := step(m, key("esc"))
	if m.state != stateDashboard {
		t.Fatalf("state = %v, want stateDashboard", m.state)
	}
	if cmd != nil {
		t.Error("esc from the Claude screen should not dispatch a command — this screen never mutates anything")
	}
}

// --- Claude tidy flow (Component 4) ---

func TestClaudeCleanKeyBlockedWhenUnsupported(t *testing.T) {
	msg := claudeLoadedMsg()
	msg.supported = false
	m := dashboardWithClaude(t, msg)
	m, cmd := step(m, key("c"))
	if m.state != stateBlocked {
		t.Fatalf("state = %v, want stateBlocked", m.state)
	}
	if m.returnState != stateClaude {
		t.Errorf("returnState = %v, want stateClaude", m.returnState)
	}
	if !strings.Contains(m.blockedReason, "can't verify Claude Code is closed") {
		t.Errorf("blockedReason = %q, missing expected text", m.blockedReason)
	}
	if cmd != nil {
		t.Error("blocked early-out should not dispatch a command")
	}
}

func TestClaudeCleanKeyBlockedWhileRunning(t *testing.T) {
	msg := claudeLoadedMsg()
	msg.running = true
	m := dashboardWithClaude(t, msg)
	m, cmd := step(m, key("c"))
	if m.state != stateBlocked {
		t.Fatalf("state = %v, want stateBlocked", m.state)
	}
	if m.returnState != stateClaude {
		t.Errorf("returnState = %v, want stateClaude", m.returnState)
	}
	if !strings.Contains(m.blockedReason, "Claude Code appears to be running") {
		t.Errorf("blockedReason = %q, missing expected text", m.blockedReason)
	}
	if cmd != nil {
		t.Error("blocked early-out should not dispatch a command")
	}
}

func TestClaudeCleanKeyEmptyPlanShowsResult(t *testing.T) {
	msg := claudeLoadedMsg()
	msg.cleanable = nil
	msg.plan = cleaner.Plan{}
	m := dashboardWithClaude(t, msg)
	m, _ = step(m, key("c"))
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if m.returnState != stateClaude {
		t.Errorf("returnState = %v, want stateClaude", m.returnState)
	}
	if !strings.Contains(m.View(), "Nothing to tidy — no stale Claude Code files are present.") {
		t.Errorf("expected the Claude-specific empty-plan message; got:\n%s", m.View())
	}
}

func TestClaudeCleanKeyOpensConfirmWithCorrectTotal(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedMsg())
	m, _ = step(m, key("c"))
	if m.state != stateClaudeConfirmClean {
		t.Fatalf("state = %v, want stateClaudeConfirmClean", m.state)
	}
	out := m.View()
	if !strings.Contains(out, "6.0 MiB") {
		t.Errorf("confirm screen missing correct total; got:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code") {
		t.Errorf("confirm screen should mention Claude Code; got:\n%s", out)
	}
}

func TestClaudeConfirmCleanNoReturnsToClaudeScreen(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedMsg())
	m, _ = step(m, key("c"))
	m, _ = step(m, key("n"))
	if m.state != stateClaude {
		t.Errorf("state = %v, want stateClaude (not stateDashboard)", m.state)
	}
}

func TestClaudeConfirmCleanYesDispatchesAndResult(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedMsg())
	m, _ = step(m, key("c"))
	m, cmd := step(m, key("y"))
	if m.state != stateCleaning {
		t.Fatalf("state = %v, want stateCleaning", m.state)
	}
	if m.workingLabel != "Tidying Claude Code's stale files aside…" {
		t.Errorf("workingLabel = %q, want the Claude-specific label", m.workingLabel)
	}
	if m.returnState != stateClaude {
		t.Errorf("returnState = %v, want stateClaude", m.returnState)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch claudeCleanCmd")
	}
	if !strings.Contains(m.View(), "Tidying Claude Code's stale files aside…") {
		t.Errorf("working screen missing the Claude-specific label; got:\n%s", m.View())
	}

	m, _ = step(m, cleanResultMsg{dest: "/b/20260626-100000", movedBytes: 6 * 1024 * 1024})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if !strings.Contains(m.View(), "Claude Code's stale files aside") {
		t.Errorf("result screen missing Claude-specific wording; got:\n%s", m.View())
	}

	// returnState must send esc back to stateClaude (not stateDashboard) with
	// a Claude reload (loadClaudeCmd), not a Codex one (loadCmd).
	m, cmd2 := step(m, key("esc"))
	if m.state != stateClaude {
		t.Fatalf("state = %v, want stateClaude", m.state)
	}
	if cmd2 == nil {
		t.Fatal("esc should dispatch a reload command")
	}
	if _, ok := cmd2().(loadedClaudeMsg); !ok {
		t.Errorf("esc from a Claude result screen should reload via loadClaudeCmd, got a command producing %T", cmd2())
	}
}

// TestClaudeConfirmCleanCatchesRunStateFlip guards the authoritative
// re-check: claudeCleanCmd must never trust the claudeRunning captured when
// the screen was entered — it re-checks isClaudeRunning() itself right before
// applying, so a run-state flip between screen-entry and confirm is caught.
func TestClaudeConfirmCleanCatchesRunStateFlip(t *testing.T) {
	origRun, origApply := isClaudeRunning, applyPlan
	t.Cleanup(func() { isClaudeRunning, applyPlan = origRun, origApply })

	isClaudeRunning = func() (bool, error) { return false, nil }
	applied := false
	applyPlan = func(p cleaner.Plan, hold time.Duration) (string, int64, error) {
		applied = true
		return "/b/x", p.TotalBytes, nil
	}

	m := dashboardWithClaude(t, claudeLoadedMsg())
	m, _ = step(m, key("c")) // -> stateClaudeConfirmClean, while (still) not running

	isClaudeRunning = func() (bool, error) { return true, nil } // Claude Code started in the meantime

	m, cmd := step(m, key("y"))
	if m.state != stateCleaning {
		t.Fatalf("state = %v, want stateCleaning (optimistic transition before the command runs)", m.state)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch claudeCleanCmd")
	}
	msg := cmd()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("claudeCleanCmd returned %T after a run-state flip, want blockedMsg", msg)
	}
	if applied {
		t.Error("applyPlan was called despite the run-state flip — the re-check gate was bypassed")
	}
}

// TestClaudeCleanCmdGateRefusesWhileRunning is the seam-level counterpart:
// claudeCleanCmd itself must never call applyPlan while Claude Code is running.
func TestClaudeCleanCmdGateRefusesWhileRunning(t *testing.T) {
	origRun, origApply := isClaudeRunning, applyPlan
	t.Cleanup(func() { isClaudeRunning, applyPlan = origRun, origApply })

	applied := false
	isClaudeRunning = func() (bool, error) { return true, nil }
	applyPlan = func(p cleaner.Plan, hold time.Duration) (string, int64, error) { applied = true; return "", 0, nil }

	msg := claudeCleanCmd(config.Default().BinHold(), config.Default().StaleAfter())()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("claudeCleanCmd returned %T, want blockedMsg", msg)
	}
	if applied {
		t.Error("applyPlan was called while Claude Code running — gate bypassed")
	}
}

// TestClaudeCleanCmdMovesStaleFilesWhenNotRunning exercises claudeCleanCmd
// against a real (temp-dir) filesystem, confirming it actually moves stale
// Claude Code files aside via the same generic applyPlan seam Codex uses.
func TestClaudeCleanCmdMovesStaleFilesWhenNotRunning(t *testing.T) {
	origDir, origRun, origReceipt := claudeDir, isClaudeRunning, appendReceipt
	t.Cleanup(func() { claudeDir, isClaudeRunning, appendReceipt = origDir, origRun, origReceipt })

	dir := t.TempDir()
	projDir := filepath.Join(dir, "projects", "myproj")
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(projDir, "session.jsonl")
	if err := os.WriteFile(staleFile, make([]byte, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(staleFile, old, old); err != nil {
		t.Fatal(err)
	}

	claudeDir = func() (string, error) { return dir, nil }
	isClaudeRunning = func() (bool, error) { return false, nil }
	appendReceipt = func(recorder.Receipt) error { return nil }

	msg := claudeCleanCmd(config.Default().BinHold(), 24*time.Hour)()
	res, ok := msg.(cleanResultMsg)
	if !ok {
		t.Fatalf("claudeCleanCmd returned %T, want cleanResultMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("clean failed: %v", res.err)
	}
	if res.movedBytes != 64 {
		t.Errorf("movedBytes = %d, want 64", res.movedBytes)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Error("stale file was not moved aside")
	}
}

// --- Claude restore flow (Component 5) ---

func TestClaudeRestoreKeyEmptyBackupsShowsResult(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedMsg()) // no backups
	m, _ = step(m, key("r"))
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if m.returnState != stateClaude {
		t.Errorf("returnState = %v, want stateClaude", m.returnState)
	}
	if !strings.Contains(m.View(), "No Claude Code backups to restore") {
		t.Errorf("expected a Claude-specific 'no backups' message; got:\n%s", m.View())
	}
}

func TestClaudeRestoreKeyOpensListWhenBackupsExist(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedWithBackup())
	m, _ = step(m, key("r"))
	if m.state != stateClaudeRestoreList {
		t.Fatalf("state = %v, want stateClaudeRestoreList", m.state)
	}
	if !strings.Contains(m.View(), "20260626-100000") {
		t.Errorf("restore list missing backup id; got:\n%s", m.View())
	}
}

func TestClaudeRestoreConfirmShowsOriginalPaths(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedWithBackup())
	m, _ = step(m, key("r"))
	m, _ = step(m, key("enter"))
	if m.state != stateClaudeConfirmRestore {
		t.Fatalf("state = %v, want stateClaudeConfirmRestore", m.state)
	}
	out := m.View()
	for _, want := range []string{"20260626-100000", "/Users/you/.claude/projects/p/s.jsonl", "Claude Code folder"} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm restore screen missing %q; got:\n%s", want, out)
		}
	}
}

func TestClaudeConfirmRestoreNoReturnsToClaudeScreen(t *testing.T) {
	m := dashboardWithClaude(t, claudeLoadedWithBackup())
	m, _ = step(m, key("r"))
	m, _ = step(m, key("enter"))
	m, _ = step(m, key("n"))
	if m.state != stateClaude {
		t.Errorf("state = %v, want stateClaude (not stateDashboard)", m.state)
	}
}

// TestClaudeConfirmRestoreCatchesRunStateFlip mirrors the tidy flow's
// equivalent test: claudeRestoreCmd re-checks isClaudeRunning() itself right
// before restoring, never trusting the state captured on screen-entry.
func TestClaudeConfirmRestoreCatchesRunStateFlip(t *testing.T) {
	origRun, origRestore := isClaudeRunning, restoreBackup
	t.Cleanup(func() { isClaudeRunning, restoreBackup = origRun, origRestore })

	isClaudeRunning = func() (bool, error) { return false, nil }
	called := false
	restoreBackup = func(dir string) error { called = true; return nil }

	m := dashboardWithClaude(t, claudeLoadedWithBackup())
	m, _ = step(m, key("r"))
	m, _ = step(m, key("enter")) // -> stateClaudeConfirmRestore, while (still) not running

	isClaudeRunning = func() (bool, error) { return true, nil } // Claude Code started in the meantime

	m, cmd := step(m, key("y"))
	if m.state != stateRestoring {
		t.Fatalf("state = %v, want stateRestoring (optimistic transition before the command runs)", m.state)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch claudeRestoreCmd")
	}
	msg := cmd()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("claudeRestoreCmd returned %T after a run-state flip, want blockedMsg", msg)
	}
	if called {
		t.Error("restoreBackup was called despite the run-state flip — the re-check gate was bypassed")
	}
}

// TestClaudeRestoreCmdGateRefusesWhileRunning is the seam-level counterpart.
func TestClaudeRestoreCmdGateRefusesWhileRunning(t *testing.T) {
	origRun, origRestore := isClaudeRunning, restoreBackup
	t.Cleanup(func() { isClaudeRunning, restoreBackup = origRun, origRestore })

	called := false
	isClaudeRunning = func() (bool, error) { return true, nil }
	restoreBackup = func(dir string) error { called = true; return nil }

	msg := claudeRestoreCmd("/some/backup")()
	if _, ok := msg.(blockedMsg); !ok {
		t.Fatalf("claudeRestoreCmd returned %T, want blockedMsg", msg)
	}
	if called {
		t.Error("restoreBackup was called while Claude Code running — gate bypassed")
	}
}

func TestClaudeConfirmRestoreYesDispatchesAndReturnsToClaude(t *testing.T) {
	origRun := isClaudeRunning
	t.Cleanup(func() { isClaudeRunning = origRun })
	isClaudeRunning = func() (bool, error) { return false, nil }

	m := dashboardWithClaude(t, claudeLoadedWithBackup())
	m, _ = step(m, key("r"))
	m, _ = step(m, key("enter"))
	m, cmd := step(m, key("y"))
	if m.state != stateRestoring {
		t.Fatalf("state = %v, want stateRestoring", m.state)
	}
	if m.workingLabel != "Restoring…" {
		t.Errorf("workingLabel = %q, want %q", m.workingLabel, "Restoring…")
	}
	if m.returnState != stateClaude {
		t.Errorf("returnState = %v, want stateClaude", m.returnState)
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch claudeRestoreCmd")
	}

	m, _ = step(m, restoreResultMsg{id: "20260626-100000"})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}
	if !strings.Contains(m.View(), "Claude Code folder") {
		t.Errorf("result screen missing Claude-specific wording; got:\n%s", m.View())
	}

	m, cmd2 := step(m, key("esc"))
	if m.state != stateClaude {
		t.Fatalf("state = %v, want stateClaude", m.state)
	}
	if cmd2 == nil {
		t.Fatal("esc should dispatch a reload command")
	}
	if _, ok := cmd2().(loadedClaudeMsg); !ok {
		t.Errorf("esc from a Claude result screen should reload via loadClaudeCmd, got a command producing %T", cmd2())
	}
}

// --- Existing Codex flow regression checks (Component 6's explicit ask) ---
//
// returnState/workingLabel now flow through the shared cleaning/restoring/
// result/blocked screens; these tests pin down that the EXISTING Codex
// dispatch sites still produce byte-identical behavior and text now that both
// fields are threaded through them.

func TestCodexConfirmCleanSetsReturnStateAndWorkingLabel(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("c"))
	m, cmd := step(m, key("y"))
	if m.state != stateCleaning {
		t.Fatalf("state = %v, want stateCleaning", m.state)
	}
	if m.returnState != stateDashboard {
		t.Errorf("returnState = %v, want stateDashboard", m.returnState)
	}
	if m.workingLabel != "Tidying Codex logs aside…" {
		t.Errorf("workingLabel = %q, want the exact pre-existing literal", m.workingLabel)
	}
	if !strings.Contains(m.View(), "Tidying Codex logs aside…") {
		t.Errorf("working screen missing the Codex label; got:\n%s", m.View())
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch cleanCmd")
	}
}

func TestCodexConfirmRestoreSetsReturnStateAndWorkingLabel(t *testing.T) {
	m, _ := step(New(config.Default()), loadedWithBackup())
	m, _ = step(m, key("r"))
	m, _ = step(m, key("enter"))
	m, cmd := step(m, key("y"))
	if m.state != stateRestoring {
		t.Fatalf("state = %v, want stateRestoring", m.state)
	}
	if m.returnState != stateDashboard {
		t.Errorf("returnState = %v, want stateDashboard", m.returnState)
	}
	if m.workingLabel != "Restoring…" {
		t.Errorf("workingLabel = %q, want %q", m.workingLabel, "Restoring…")
	}
	if cmd == nil {
		t.Fatal("confirm-yes should dispatch restoreCmd")
	}
}

func TestCodexBlockedAndEmptyEarlyOutsSetReturnStateDashboard(t *testing.T) {
	// c: not supported
	m1, _ := step(New(config.Default()), sampleLoaded())
	m1.supported = false
	m1, _ = step(m1, key("c"))
	if m1.returnState != stateDashboard {
		t.Errorf("not-supported: returnState = %v, want stateDashboard", m1.returnState)
	}

	// c: running
	m2, _ := step(New(config.Default()), sampleLoaded())
	m2.running = true
	m2, _ = step(m2, key("c"))
	if m2.returnState != stateDashboard {
		t.Errorf("running: returnState = %v, want stateDashboard", m2.returnState)
	}

	// c: empty plan
	m3, _ := step(New(config.Default()), sampleLoaded())
	m3.plan = cleaner.Plan{}
	m3, _ = step(m3, key("c"))
	if m3.returnState != stateDashboard {
		t.Errorf("empty-plan: returnState = %v, want stateDashboard", m3.returnState)
	}

	// r: empty backups
	m4, _ := step(New(config.Default()), sampleLoaded()) // no backups
	m4, _ = step(m4, key("r"))
	if m4.returnState != stateDashboard {
		t.Errorf("empty-backups: returnState = %v, want stateDashboard", m4.returnState)
	}
}

// TestCodexResultEscReturnsToDashboardWithCodexReload pins down that, for the
// default (Codex) path, esc/enter from the result/blocked screens reload via
// loadCmd (producing loadedMsg) — never loadClaudeCmd (producing
// loadedClaudeMsg) — now that the shared handler branches on returnState.
func TestCodexResultEscReturnsToDashboardWithCodexReload(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, key("c"))
	m, _ = step(m, key("y"))
	m, _ = step(m, cleanResultMsg{dest: "/b/20260626-100000", movedBytes: 200 * 1024 * 1024})
	if m.state != stateResult {
		t.Fatalf("state = %v, want stateResult", m.state)
	}

	m, cmd := step(m, key("esc"))
	if m.state != stateDashboard {
		t.Fatalf("state = %v, want stateDashboard", m.state)
	}
	if cmd == nil {
		t.Fatal("esc should dispatch a reload command")
	}
	if _, ok := cmd().(loadedMsg); !ok {
		t.Errorf("esc from a Codex result screen should reload via loadCmd, got a command producing %T", cmd())
	}
}

// TestCodexCleanResultTextByteIdentical guards the exact wording
// cleanResultMsg renders for the Codex path (returnState's zero value),
// unchanged from before returnState/workingLabel existed.
func TestCodexCleanResultTextByteIdentical(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, cleanResultMsg{})
	if m.resultMsg != "Nothing to tidy — no Codex logs are present." {
		t.Errorf("resultMsg = %q, want unchanged Codex wording", m.resultMsg)
	}

	m, _ = step(New(config.Default()), sampleLoaded())
	m, _ = step(m, cleanResultMsg{dest: "/b/20260626-100000", movedBytes: 200 * 1024 * 1024})
	want := "Tidied 200.0 MiB of Codex logs aside.\nBackup: /b/20260626-100000\nNothing was deleted — restore any time."
	if m.resultMsg != want {
		t.Errorf("resultMsg = %q, want %q", m.resultMsg, want)
	}
}

// TestCodexRestoreResultTextByteIdentical is TestCodexCleanResultTextByteIdentical's
// restoreResultMsg counterpart.
func TestCodexRestoreResultTextByteIdentical(t *testing.T) {
	m, _ := step(New(config.Default()), sampleLoaded())
	m, _ = step(m, restoreResultMsg{id: "20260626-100000"})
	want := "Restored backup 20260626-100000 to your Codex folder."
	if m.resultMsg != want {
		t.Errorf("resultMsg = %q, want %q", m.resultMsg, want)
	}
}
