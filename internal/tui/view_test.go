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
