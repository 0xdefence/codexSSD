// internal/cleaner/tool_test.go
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/tool"
)

func writeAged(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := now.Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCleanRestoreRoundTripNestedPaths(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	transcript := filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl")
	writeAged(t, transcript, 40*24*time.Hour, now)

	plan, err := PlanTool(tool.Claude(), dir, now, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Name != "projects/-Users-jo-app/s1.jsonl" {
		t.Fatalf("plan items = %+v, want the nested transcript by relative name", plan.Items)
	}
	if plan.Tool != "claude" {
		t.Errorf("plan.Tool = %q, want claude", plan.Tool)
	}

	backupDir, err := plan.Apply(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Error("transcript still at original path after Apply; want moved aside")
	}
	movedTo := filepath.Join(backupDir, "projects", "-Users-jo-app", "s1.jsonl")
	if _, err := os.Stat(movedTo); err != nil {
		t.Errorf("moved transcript not found at %s: %v", movedTo, err)
	}

	if err := Restore(backupDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("transcript not restored to original path: %v", err)
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Error("backup dir should be gone after a full restore")
	}
}

func TestApplyRefusesFileOutsideProfileAllowList(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	forbidden := filepath.Join(dir, "memory", "MEMORY.md")
	writeAged(t, forbidden, 90*24*time.Hour, now)

	plan := Plan{
		Tool:       "claude",
		CodexDir:   dir,
		BackupRoot: filepath.Join(dir, BackupDirName),
		Items:      []PlanItem{{Name: "memory/MEMORY.md", Path: forbidden, Size: 4}},
	}
	if _, err := plan.Apply(now); err == nil {
		t.Fatal("Apply moved a NeverTouch file; want refusal")
	}
	if _, err := os.Stat(forbidden); err != nil {
		t.Errorf("forbidden file was disturbed: %v", err)
	}
}

func TestManifestWithoutToolFieldRestoresAsCodex(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeAged(t, filepath.Join(dir, "logs_2.sqlite"), time.Hour, now)

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatal(err)
	}
	backupDir, err := plan.Apply(now)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-multi-tool manifest: strip the tool field.
	m, err := readManifest(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	m.Tool = ""
	if err := writeManifest(backupDir, m); err != nil {
		t.Fatal(err)
	}
	if err := Restore(backupDir); err != nil {
		t.Fatalf("Restore of legacy manifest failed: %v", err)
	}
}
