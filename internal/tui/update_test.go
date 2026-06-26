package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
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
		_, cmd := step(New(), key(k))
		if cmd == nil {
			t.Fatalf("%q produced no command, want quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%q did not produce tea.QuitMsg", k)
		}
	}
}

func TestHelpToggle(t *testing.T) {
	m := New()
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
	m, _ := step(New(), tea.WindowSizeMsg{Width: 80, Height: 24})
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
		plan:      cleaner.Plan{TotalBytes: 200 * 1024 * 1024},
		backups:   nil,
	}
}

func TestLoadedPopulatesDashboard(t *testing.T) {
	m, _ := step(New(), sampleLoaded())
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
	m, _ := step(New(), msg)
	if m.deadweight() {
		t.Error("1 MiB should not count as deadweight")
	}
}

func TestLastTidyFromBackups(t *testing.T) {
	when := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	msg := sampleLoaded()
	msg.backups = []cleaner.Backup{{Dir: "/b/20260620-090000", Manifest: cleaner.Manifest{MovedAt: when}}}
	m, _ := step(New(), msg)
	got, ok := m.lastTidy()
	if !ok || !got.Equal(when) {
		t.Errorf("lastTidy = %v ok=%v, want %v", got, ok, when)
	}
}
