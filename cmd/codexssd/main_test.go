package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/visibility"
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

// writeExpiredBackup creates an expired backup under HOME/.codex for prune tests.
func writeExpiredBackup(t *testing.T, home, id string) string {
	t.Helper()
	bd := filepath.Join(home, ".codex", "codexssd-backups", id)
	if err := os.MkdirAll(bd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bd, "logs_2.sqlite"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	m := cleaner.Manifest{MovedAt: past, HoldUntil: past, Items: []cleaner.ManifestItem{{Name: "logs_2.sqlite", OriginalPath: filepath.Join(home, ".codex", "logs_2.sqlite"), Size: 1}}}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(bd, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return bd
}

func TestPruneDryRunReleasesNothing(t *testing.T) {
	withSilencedStdout(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	bd := writeExpiredBackup(t, home, "20000101-000000")

	if code := cmdPrune([]string{"--dry-run"}); code != 0 {
		t.Fatalf("prune --dry-run exit = %d, want 0", code)
	}
	if _, err := os.Stat(bd); err != nil {
		t.Errorf("--dry-run must not move the backup: %v", err)
	}
}

func TestPruneNoBackups(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdPrune(nil); code != 0 {
		t.Errorf("prune with no backups exit = %d, want 0", code)
	}
}

func TestPruneBadFlag(t *testing.T) {
	withSilencedStdout(t)
	t.Setenv("HOME", t.TempDir())
	if code := cmdPrune([]string{"--nope"}); code != 2 {
		t.Errorf("prune bad flag exit = %d, want 2", code)
	}
}

func TestRenderVisibilityReport(t *testing.T) {
	r := visibility.Report{
		Dir: "/home/x/.codex", DirExists: true, TotalBytes: 500,
		Entries: []visibility.Entry{
			{Name: "sessions", IsDir: true, TotalBytes: 500, FileCount: 2,
				NewestMod: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Stale: true},
		},
	}
	var buf bytes.Buffer
	renderVisibility(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "sessions") || !strings.Contains(out, "March 2026") {
		t.Errorf("output missing pieces:\n%s", out)
	}
	if !strings.Contains(out, "yours to decide") {
		t.Errorf("report must end with the report-only pointer:\n%s", out)
	}
}
