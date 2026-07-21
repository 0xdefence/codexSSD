package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/visibility"
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

// TestRiskPanelShowsAllReasons guards against the old view.go bug where only
// Reasons[0] was rendered — every reason the risk engine surfaces must appear.
func TestRiskPanelShowsAllReasons(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 300 // wide enough that the panel never has to truncate the reason line
	m.assessment = monitor.Assessment{
		Level:        monitor.RiskHigh,
		RateMBPerMin: 120,
		Reasons:      []string{"writing 120 MB/min", "WAL file is 2048 MiB", "growing while Codex is idle"},
	}
	out := m.View()
	for _, want := range []string{"writing 120 MB/min", "WAL file is 2048 MiB", "growing while Codex is idle"} {
		if !strings.Contains(out, want) {
			t.Errorf("risk panel missing reason %q; got:\n%s", want, out)
		}
	}
}

func TestFriendlyIntervalFormatsSecondsAndMinutes(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{30 * time.Second, "30s"},
		{60 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{90 * time.Second, "1m30s"},
	}
	for _, c := range cases {
		if got := friendlyInterval(c.d); got != c.want {
			t.Errorf("friendlyInterval(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestFooterReflectsConfiguredPollInterval guards against a hardcoded "30s" —
// the footer must derive from the actual configured poll interval.
func TestFooterReflectsConfiguredPollInterval(t *testing.T) {
	cfg := config.Default()
	cfg.PollIntervalSeconds = 90 // -> "1m30s", nowhere near the old hardcoded "30s"
	m, _ := step(New(cfg), sampleLoaded())
	m.width = 100
	out := m.View()
	if !strings.Contains(out, "every 1m30s") {
		t.Errorf("footer should reflect the configured poll interval; got:\n%s", out)
	}
	if strings.Contains(out, "every 30s") {
		t.Errorf("footer should not show the old hardcoded 30s when configured for 90s; got:\n%s", out)
	}
}

func TestRiskPanelShowsSessionPeakOnceStarted(t *testing.T) {
	m := New(config.Default())
	m.width = 100
	if strings.Contains(m.View(), "session peak") {
		t.Errorf("peak line should not appear before any successful load; got:\n%s", m.View())
	}

	msg := sampleLoaded()
	msg.at = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) // a non-zero load time is what sets startedAt
	m, _ = step(m, msg)
	out := m.View()
	if !strings.Contains(out, "session peak: LOW · 0 MB/min") {
		t.Errorf("risk panel missing the session peak line; got:\n%s", out)
	}
}

func TestFooterAndHelpMentionInfoKey(t *testing.T) {
	m := New(config.Default())
	if !strings.Contains(m.footer(), "i info") {
		t.Errorf("footer should mention the i key; got %q", m.footer())
	}
	if !strings.Contains(m.renderHelp(), "i    open the info screen") {
		t.Errorf("help screen should document the i key; got:\n%s", m.renderHelp())
	}
}

func TestInfoScreenShowsLoadingBeforeDataArrives(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 100
	m.state = stateInfo
	m.infoLoaded = false
	out := m.View()
	if !strings.Contains(out, "loading") {
		t.Errorf("info screen should show a loading indicator before infoMsg arrives; got:\n%s", out)
	}
}

func TestInfoScreenRendersSettingsFootprintAndDisk(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 100
	m.state = stateInfo
	m.infoLoaded = true
	m.selfReport = self.Report{
		Mode: "low-write", StateDir: "/home/u/.codexssd",
		HistoryBytes: 4096, Records: 7, LastAction: "clean",
	}
	m.diskReport = visibility.Report{
		Dir: "/home/u/.codex", DirExists: true,
		Entries: []visibility.Entry{
			{Name: "logs_2.sqlite", TotalBytes: 200 * 1024 * 1024, FileCount: 1, NewestMod: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)},
			{Name: "old-thing", TotalBytes: 1024, FileCount: 1, Stale: true},
			{Name: "unreadable", ReadError: "permission denied"},
		},
		TotalBytes: 200*1024*1024 + 1024,
	}

	out := m.View()
	for _, want := range []string{
		"Settings",
		"CodexSSD's own footprint",
		"Disk report (~/.codex)",
		"low-write",
		"/home/u/.codexssd",
		"clean",
		"logs_2.sqlite",
		"STALE",
		"⚠",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("info screen missing %q; got:\n%s", want, out)
		}
	}
}

func TestInfoScreenSurfacesSelfMeasureError(t *testing.T) {
	m := dashboardAfterLoad(t)
	m.width = 100
	m.state = stateInfo
	m.infoLoaded = true
	m.selfErr = errors.New("permission denied")

	out := m.View()
	if !strings.Contains(out, "Could not measure") {
		t.Errorf("info screen should surface a self-measure error; got:\n%s", out)
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
