package tui

import (
	"strings"
	"testing"

	"github.com/0xdefence/codexssd/internal/config"
	"github.com/charmbracelet/lipgloss"
)

func TestEffectiveWidthFallsBackTo80(t *testing.T) {
	if got := effectiveWidth(New(config.Default())); got != 80 {
		t.Errorf("effectiveWidth with no size = %d, want 80", got)
	}
	m := New(config.Default())
	m.width = 120
	if got := effectiveWidth(m); got != 120 {
		t.Errorf("effectiveWidth = %d, want 120", got)
	}
}

func TestPanelWrapsBodyWithTitleAndBorder(t *testing.T) {
	out := panel("Risk", "● LOW", 30)
	if !strings.Contains(out, "Risk") {
		t.Errorf("panel should show its title, got:\n%s", out)
	}
	if !strings.Contains(out, "● LOW") {
		t.Errorf("panel should show its body, got:\n%s", out)
	}
	// Rounded border corners present (ascii profile keeps box-drawing runes).
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("panel should draw a rounded border, got:\n%s", out)
	}
	// Every rendered line is the full outer width.
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 30 {
			t.Errorf("panel line width = %d, want 30: %q", w, line)
		}
	}
}

func TestStatusBarPutsKeysLeftStatusRight(t *testing.T) {
	out := statusBar("q quit", "30s", 40)
	if lipgloss.Width(out) != 40 {
		t.Errorf("status bar width = %d, want 40", lipgloss.Width(out))
	}
	if !strings.HasPrefix(strings.TrimRight(out, " "), "q quit") {
		t.Errorf("keys should be left-aligned, got %q", out)
	}
	if !strings.Contains(out, "30s") {
		t.Errorf("status should appear, got %q", out)
	}
}
