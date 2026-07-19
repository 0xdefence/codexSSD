package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/tool"
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

// withToolNotRunning stubs isToolRunning (the general per-profile
// running-check seam behind toolRunning) so the claude mutating path can be
// driven deterministically in a test, regardless of whether a real Claude
// Code process happens to be running on the host — which, notably, it often
// IS in this environment, since the agent driving this very test suite is
// commonly a Claude Code session itself. Mirrors integration_test.go's
// withCodexRunning, one level up (per-profile rather than codex-only).
func withToolNotRunning(t *testing.T) {
	t.Helper()
	prev := isToolRunning
	isToolRunning = func(tool.Profile) (bool, error) { return false, nil }
	t.Cleanup(func() { isToolRunning = prev })
}

// TestCleanYesClaudeRoundTrip drives `clean --tool claude --yes` then
// `restore --tool claude <id>` end-to-end through run(), against a temp
// $HOME with one stale (cleanable) transcript and one fresh (must-not-move)
// transcript.
//
// The refuse-while-running path isn't covered here: unlike isCodexRunning,
// isToolRunning's real implementation shells out to `ps`, which a test can't
// spoof into reporting a live claude process short of stubbing the same seam
// this test already uses to force "not running" — so there's nothing left to
// exercise beyond what TestCleanRefusesWhileCodexRunning already covers for
// the shared toolRunning/refusal code path.
func TestCleanYesClaudeRoundTrip(t *testing.T) {
	withSilencedStdout(t)
	withToolNotRunning(t)
	home := withTempHome(t)

	staleTranscript := filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl")
	freshTranscript := filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s2.jsonl")
	writeAgedFile(t, staleTranscript, 40*24*time.Hour)
	writeAgedFile(t, freshTranscript, time.Hour)

	if code := run([]string{"clean", "--tool", "claude", "--yes"}); code != 0 {
		t.Fatalf("clean --tool claude --yes exit = %d, want 0", code)
	}

	if _, err := os.Stat(staleTranscript); !os.IsNotExist(err) {
		t.Errorf("stale transcript should have left its original path, err=%v", err)
	}
	if _, err := os.Stat(freshTranscript); err != nil {
		t.Errorf("fresh transcript should NOT have moved: %v", err)
	}

	claudeDir := filepath.Join(home, ".claude")
	backups, err := cleaner.ListBackups(claudeDir)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("want exactly 1 backup, got %d", len(backups))
	}
	id := filepath.Base(backups[0].Dir) // same resolution cmdRestore uses to match an id

	nested := filepath.Join(backups[0].Dir, "projects", "-Users-jo-app", "s1.jsonl")
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("stale transcript not nested under the backup at %s: %v", nested, err)
	}

	sessionsPath, err := recorder.Path()
	if err != nil {
		t.Fatalf("recorder.Path: %v", err)
	}
	sum, err := recorder.SummarizeFile(sessionsPath)
	if err != nil {
		t.Fatalf("SummarizeFile: %v", err)
	}
	if sum.Records != 1 || sum.LastAction != "clean --tool claude" {
		t.Errorf("receipt = {Records:%d LastAction:%q}, want {1 %q}", sum.Records, sum.LastAction, "clean --tool claude")
	}

	if code := run([]string{"restore", "--tool", "claude", id}); code != 0 {
		t.Fatalf("restore --tool claude %s exit = %d, want 0", id, code)
	}
	if _, err := os.Stat(staleTranscript); err != nil {
		t.Errorf("transcript not restored to %s: %v", staleTranscript, err)
	}
}
