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

func TestPanelTruncatesWideBody(t *testing.T) {
	// A body line wider than the inner width must be truncated, not overflow.
	out := panel("Codex folder", "/a/very/long/path/that/definitely/exceeds/the/inner/width/aaaaaa", 30)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 30 {
			t.Errorf("panel line width = %d, want 30: %q", w, line)
		}
	}
}

func TestPanelTruncatesLongTitle(t *testing.T) {
	// A title wider than the inner width must not push the top border past width.
	out := panel("a really long panel title that overflows the box", "body", 20)
	top := strings.SplitN(out, "\n", 2)[0]
	if w := lipgloss.Width(top); w != 20 {
		t.Errorf("panel top line width = %d, want 20: %q", w, top)
	}
}

func TestTruncateCellsAddsEllipsis(t *testing.T) {
	// A body line much wider than the panel inner width must be truncated with
	// a visible ellipsis cue, while every rendered line stays exactly the
	// outer width (the truncation must still respect the width contract).
	out := panel("Codex folder", "/a/very/long/path/that/definitely/exceeds/the/inner/width/aaaaaa", 30)
	found := false
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 30 {
			t.Errorf("panel line width = %d, want 30: %q", w, line)
		}
		if strings.Contains(line, "…") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected truncated line to contain an ellipsis, got:\n%s", out)
	}
}

func TestStatusBarStaysOneLine(t *testing.T) {
	// Overflowing content must be truncated so the bar stays exactly one line
	// at the given width — otherwise Render word-wraps and misaligns the fixed
	// last row of later screens.
	out := statusBar("a very long set of key hints that overflows", "watching status that is also long", 30)
	if lipgloss.Width(out) != 30 {
		t.Errorf("status bar width = %d, want 30", lipgloss.Width(out))
	}
	if n := strings.Count(out, "\n"); n != 0 {
		t.Errorf("status bar must be one line, got %d newlines: %q", n, out)
	}
}
