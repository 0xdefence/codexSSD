package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
