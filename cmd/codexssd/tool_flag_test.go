package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempHome points $HOME at a temp dir so ~/.claude and ~/.codex resolve
// inside the test sandbox.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeAgedFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestStatusRejectsUnknownTool(t *testing.T) {
	withTempHome(t)
	if code := run([]string{"status", "--tool", "clippy"}); code != 2 {
		t.Errorf("status --tool clippy exit = %d, want 2", code)
	}
}

func TestStatusClaudeRunsCleanly(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl"), 40*24*time.Hour)
	if code := run([]string{"status", "--tool", "claude"}); code != 0 {
		t.Errorf("status --tool claude exit = %d, want 0", code)
	}
}

func TestCleanClaudeDryRunTouchesNothing(t *testing.T) {
	home := withTempHome(t)
	transcript := filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl")
	writeAgedFile(t, transcript, 40*24*time.Hour)
	if code := run([]string{"clean", "--tool", "claude"}); code != 0 {
		t.Errorf("clean --tool claude (dry run) exit = %d, want 0", code)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("dry run moved the transcript: %v", err)
	}
}
