package tui

import (
	"strings"
	"testing"

	"github.com/0xdefence/codexssd/internal/config"
)

// dashboardAfterLoad returns a model that has received one sampleLoaded() frame.
func dashboardAfterLoad(t *testing.T) Model {
	t.Helper()
	m, _ := step(New(config.Default()), sampleLoaded())
	return m
}

func TestDashboardShowsKeyFacts(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 100
	out := m.View()
	for _, want := range []string{
		"/home/u/.codex",    // folder path
		"logs_2.sqlite",     // a file name
		"Total",             // total row label
		"c tidy",            // a keybinding in the status bar
		"the disk watchdog", // the logo subtitle
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q; got:\n%s", want, out)
		}
	}
}

func TestDashboardNarrowRendersWithoutPanic(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 30 // below the logo's block width → compact logo, single column
	out := m.View()
	if !strings.Contains(out, "codexSSD") { // compact logo
		t.Errorf("narrow dashboard should use the compact logo; got:\n%s", out)
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("narrow dashboard should still show the logs; got:\n%s", out)
	}
}

func TestDashboardShowsRiskPanel(t *testing.T) {
	// A single load frame evaluates to LOW (rate needs two samples). The Risk
	// panel and its level label must still appear.
	m := dashboardAfterLoad(t)
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "Risk") {
		t.Errorf("dashboard should show a Risk panel; got:\n%s", out)
	}
	if !strings.Contains(out, "LOW") {
		t.Errorf("dashboard should show the LOW risk label; got:\n%s", out)
	}
}

func TestConfirmCleanScreenStyled(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 90
	m.state = stateConfirmClean
	out := m.View()
	if !strings.Contains(out, "codexSSD") { // compact logo header
		t.Errorf("confirm screen should show the logo header; got:\n%s", out)
	}
	if !strings.Contains(out, "y yes") || !strings.Contains(out, "n no") {
		t.Errorf("confirm screen should show its keys; got:\n%s", out)
	}
}

func TestRestoreListHighlightsSelection(t *testing.T) {
	m, _ := step(New(config.Default()), loadedWithBackup())
	m.width = 90
	m.state = stateRestoreList
	m.selected = 0
	out := m.View()
	// The selected backup id should appear; no reliance on the old "> " prefix.
	if !strings.Contains(out, filepathBase(m.backups[0].Dir)) {
		t.Errorf("restore list should show the backup id; got:\n%s", out)
	}
	if !strings.Contains(out, "choose") {
		t.Errorf("restore list should show its keys; got:\n%s", out)
	}
}

func TestResultAndBlockedScreensStyled(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 90

	m.state = stateResult
	m.resultMsg = "Tidied 9.5 GiB of Codex logs aside."
	if out := m.View(); !strings.Contains(out, "Tidied 9.5 GiB") {
		t.Errorf("result screen missing its message; got:\n%s", out)
	}

	m.state = stateBlocked
	m.blockedReason = "Codex appears to be running."
	if out := m.View(); !strings.Contains(out, "Codex appears to be running.") {
		t.Errorf("blocked screen missing its reason; got:\n%s", out)
	}
}
