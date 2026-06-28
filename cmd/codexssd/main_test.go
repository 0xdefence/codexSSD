package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdefence/codexssd/internal/cleaner"
)

func TestRenderPlanEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderPlan(&buf, cleaner.Plan{CodexDir: "/x/.codex"}, false, true)
	out := buf.String()
	if !strings.Contains(out, "Nothing to move aside") {
		t.Errorf("empty plan output missing message:\n%s", out)
	}
}

func TestRenderPlanWithItemsAndSafety(t *testing.T) {
	var buf bytes.Buffer
	p := cleaner.Plan{
		CodexDir:   "/x/.codex",
		BackupRoot: "/x/.codex/codexssd-backups",
		Items: []cleaner.PlanItem{
			{Name: "logs_2.sqlite", Path: "/x/.codex/logs_2.sqlite", Size: 1024},
		},
		TotalBytes: 1024,
	}

	// Codex running -> output must warn it is not safe to clean.
	renderPlan(&buf, p, true, true)
	out := buf.String()
	if !strings.Contains(out, "logs_2.sqlite") || !strings.Contains(out, "1.0 KiB") {
		t.Errorf("plan output missing file/size:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "running") {
		t.Errorf("plan output should warn Codex is running:\n%s", out)
	}

	// Codex not running -> output must say it is safe.
	buf.Reset()
	renderPlan(&buf, p, false, true)
	if !strings.Contains(buf.String(), "--yes") {
		t.Errorf("plan output should tell the user to run --yes:\n%s", buf.String())
	}
}

func TestRenderBackupsEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderBackups(&buf, nil)
	if !strings.Contains(buf.String(), "No backups") {
		t.Errorf("empty backups output missing message:\n%s", buf.String())
	}
}

func TestRenderBackupsLists(t *testing.T) {
	var buf bytes.Buffer
	backups := []cleaner.Backup{
		{
			Dir: "/x/.codex/codexssd-backups/20260626-143000",
			Manifest: cleaner.Manifest{
				Items: []cleaner.ManifestItem{
					{Name: "logs_2.sqlite", OriginalPath: "/x/.codex/logs_2.sqlite", Size: 2048},
				},
			},
		},
	}
	renderBackups(&buf, backups)
	out := buf.String()
	if !strings.Contains(out, "20260626-143000") {
		t.Errorf("backups output missing id:\n%s", out)
	}
	if !strings.Contains(out, "2.0 KiB") {
		t.Errorf("backups output missing total size:\n%s", out)
	}
}

// withSilencedStdout sends stdout/stderr to /dev/null for the test.
func withSilencedStdout(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("devnull: %v", err)
	}
	o, e := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	t.Cleanup(func() { os.Stdout, os.Stderr = o, e; devnull.Close() })
}

func TestInstallAgentWritesToDir(t *testing.T) {
	withSilencedStdout(t)
	dir := t.TempDir()
	if code := cmdInstallAgent([]string{dir}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md not written: %v", err)
	}
}

func TestInstallAgentPrintWritesNothing(t *testing.T) {
	withSilencedStdout(t)
	dir := t.TempDir()
	if code := cmdInstallAgent([]string{"--print", dir}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("--print should not write a file")
	}
}

func TestInstallAgentUnknownProfile(t *testing.T) {
	withSilencedStdout(t)
	if code := cmdInstallAgent([]string{"--profile", "bogus", t.TempDir()}); code != 2 {
		t.Errorf("exit = %d, want 2 for unknown profile", code)
	}
}

func TestInstallAgentRefusesExisting(t *testing.T) {
	withSilencedStdout(t)
	dir := t.TempDir()
	if code := cmdInstallAgent([]string{dir}); code != 0 {
		t.Fatalf("first exit = %d, want 0", code)
	}
	if code := cmdInstallAgent([]string{dir}); code != 1 {
		t.Errorf("second exit = %d, want 1 (refuse existing)", code)
	}
}

func TestSelfRuns(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdSelf(nil); code != 0 {
		t.Errorf("self exit = %d, want 0", code)
	}
}

func TestSelfJSON(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdSelf([]string{"--json"}); code != 0 {
		t.Errorf("self --json exit = %d, want 0", code)
	}
}
