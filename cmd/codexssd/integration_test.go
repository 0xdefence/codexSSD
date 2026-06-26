package main

import (
	"os"
	"path/filepath"
	"testing"
)

// These are hermetic end-to-end tests of the clean -> restore flow. They run on
// ANY machine regardless of whether a real Codex is running, because the
// process-check (`isCodexRunning`) is stubbed deterministically. Each test
// drives the real command functions against a throwaway $HOME containing fake
// Codex logs plus a non-Codex file that must never be touched.

// withCodexRunning substitutes the process-check for the duration of a test.
func withCodexRunning(t *testing.T, running bool) {
	t.Helper()
	prev := isCodexRunning
	isCodexRunning = func() (bool, error) { return running, nil }
	t.Cleanup(func() { isCodexRunning = prev })
}

// silenceOutput sends the commands' user-facing stdout/stderr to /dev/null so
// the test output stays pristine.
func silenceOutput(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	outPrev, errPrev := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	t.Cleanup(func() {
		os.Stdout, os.Stderr = outPrev, errPrev
		devnull.Close()
	})
}

// setupSandbox creates a throwaway $HOME with fake Codex logs and one non-Codex
// file, and returns the path to the sandbox's .codex directory.
func setupSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(codexDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("logs_2.sqlite", "fake-db")
	write("logs_2.sqlite-wal", "fake-wal")
	write("important-user-file.txt", "DO NOT TOUCH") // NOT a Codex log
	return codexDir
}

// backupDirs returns the timestamped backup directories under the codex dir.
func backupDirs(t *testing.T, codexDir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(codexDir, "codexssd-backups", "*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var dirs []string
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.IsDir() {
			dirs = append(dirs, m)
		}
	}
	return dirs
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Codex IS running -> clean --yes must refuse and move nothing.
func TestCleanRefusesWhileCodexRunning(t *testing.T) {
	silenceOutput(t)
	codexDir := setupSandbox(t)
	withCodexRunning(t, true)

	if code := cmdClean([]string{"--yes"}); code == 0 {
		t.Fatalf("clean --yes returned 0 while Codex running; want a non-zero refusal")
	}
	for _, name := range []string{"logs_2.sqlite", "logs_2.sqlite-wal"} {
		if !exists(filepath.Join(codexDir, name)) {
			t.Errorf("%s was moved while Codex was running", name)
		}
	}
	if exists(filepath.Join(codexDir, "codexssd-backups")) {
		t.Errorf("a backup directory was created while Codex was running")
	}
}

// Codex is NOT running -> clean moves the logs aside (leaving non-Codex files
// alone), and restore brings them back.
func TestCleanThenRestoreWhenCodexNotRunning(t *testing.T) {
	silenceOutput(t)
	codexDir := setupSandbox(t)
	withCodexRunning(t, false)

	if code := cmdClean([]string{"--yes"}); code != 0 {
		t.Fatalf("clean --yes returned %d; want 0", code)
	}
	for _, name := range []string{"logs_2.sqlite", "logs_2.sqlite-wal"} {
		if exists(filepath.Join(codexDir, name)) {
			t.Errorf("%s was not moved aside by clean --yes", name)
		}
	}
	// The allow-list: the non-Codex file must be untouched.
	if b, err := os.ReadFile(filepath.Join(codexDir, "important-user-file.txt")); err != nil || string(b) != "DO NOT TOUCH" {
		t.Errorf("non-Codex file was disturbed: content=%q err=%v", string(b), err)
	}
	backups := backupDirs(t, codexDir)
	if len(backups) != 1 {
		t.Fatalf("want exactly 1 backup, got %d", len(backups))
	}
	if !exists(filepath.Join(backups[0], "manifest.json")) {
		t.Errorf("backup is missing its manifest.json")
	}

	// Restore brings the logs back with their original contents.
	id := filepath.Base(backups[0])
	if code := cmdRestore([]string{id}); code != 0 {
		t.Fatalf("restore %s returned %d; want 0", id, code)
	}
	if b, err := os.ReadFile(filepath.Join(codexDir, "logs_2.sqlite")); err != nil || string(b) != "fake-db" {
		t.Errorf("logs_2.sqlite not restored correctly: content=%q err=%v", string(b), err)
	}
	if exists(backups[0]) {
		t.Errorf("backup directory was not removed after a clean restore")
	}
}

// Codex IS running -> restore must refuse and bring nothing back (restore writes
// to the live log location).
func TestRestoreRefusesWhileCodexRunning(t *testing.T) {
	silenceOutput(t)
	codexDir := setupSandbox(t)

	// Move logs aside first, with Codex not running.
	withCodexRunning(t, false)
	if code := cmdClean([]string{"--yes"}); code != 0 {
		t.Fatalf("setup clean --yes returned %d; want 0", code)
	}
	backups := backupDirs(t, codexDir)
	if len(backups) != 1 {
		t.Fatalf("want exactly 1 backup, got %d", len(backups))
	}
	id := filepath.Base(backups[0])

	// Codex starts up again -> restore must refuse.
	withCodexRunning(t, true)
	if code := cmdRestore([]string{id}); code == 0 {
		t.Fatalf("restore returned 0 while Codex running; want a non-zero refusal")
	}
	if exists(filepath.Join(codexDir, "logs_2.sqlite")) {
		t.Errorf("logs_2.sqlite was restored while Codex was running")
	}
}
