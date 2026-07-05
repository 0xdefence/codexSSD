package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces the ascii color profile so every View()/render in this
// package's tests produces deterministic plain text (no ANSI escapes) to assert
// against. termenv is already a transitive dependency — no new module is added.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}
