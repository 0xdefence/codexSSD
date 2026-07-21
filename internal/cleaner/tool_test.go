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

// TestRestoreRollsBackOnMkdirAllFailure covers the reviewer-found defect where
// a failed MkdirAll partway through Restore's move loop returned immediately
// without undoing items already restored this call — leaving them at their
// original path while the manifest still listed them, so a retry refused with
// "refusing to overwrite existing file" and the backup was stuck forever.
func TestRestoreRollsBackOnMkdirAllFailure(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	item1 := filepath.Join(dir, "projects", "proj-a", "s1.jsonl")
	item2 := filepath.Join(dir, "projects", "proj-b", "s2.jsonl")
	writeAged(t, item1, 40*24*time.Hour, now)
	writeAged(t, item2, 40*24*time.Hour, now)

	// Hand-craft the plan so item order (and therefore manifest/restore order)
	// is deterministic: item1 must succeed and get rolled back when item2 fails.
	plan := Plan{
		Tool:       "claude",
		CodexDir:   dir,
		BackupRoot: filepath.Join(dir, BackupDirName),
		Items: []PlanItem{
			{Name: "projects/proj-a/s1.jsonl", Path: item1, Size: 4},
			{Name: "projects/proj-b/s2.jsonl", Path: item2, Size: 4},
		},
	}
	backupDir, err := plan.Apply(now)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Apply only moves the file itself, so item2's original parent dir
	// ("projects/proj-b") is left behind, empty. Replace it with a plain file
	// so Restore's MkdirAll to recreate that parent fails with ENOTDIR.
	blockerPath := filepath.Join(dir, "projects", "proj-b")
	if err := os.Remove(blockerPath); err != nil {
		t.Fatalf("removing empty original parent dir: %v", err)
	}
	if err := os.WriteFile(blockerPath, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("planting blocker file: %v", err)
	}

	if err := Restore(backupDir); err == nil {
		t.Fatal("Restore succeeded despite a blocked parent dir; want error")
	}

	// item1 must have been rolled back into the backup, NOT left at its
	// original path (which would desync it from the still-full manifest).
	if _, err := os.Stat(item1); !os.IsNotExist(err) {
		t.Error("item1 left at its original path after item2 failed; want rollback")
	}
	backedItem1 := filepath.Join(backupDir, "projects", "proj-a", "s1.jsonl")
	if _, err := os.Stat(backedItem1); err != nil {
		t.Errorf("item1 not rolled back into the backup dir: %v", err)
	}

	// Clear the blocker and retry: the backup must still be fully usable.
	if err := os.Remove(blockerPath); err != nil {
		t.Fatalf("removing blocker: %v", err)
	}
	if err := Restore(backupDir); err != nil {
		t.Fatalf("retry Restore after clearing blocker: %v", err)
	}
	if _, err := os.Stat(item1); err != nil {
		t.Errorf("item1 not restored on retry: %v", err)
	}
	if _, err := os.Stat(item2); err != nil {
		t.Errorf("item2 not restored on retry: %v", err)
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Error("backup dir should be gone after a full retry restore")
	}
}

// TestApplyRollbackRemovesOrphanedNestedDirs covers the reviewer-found defect
// where Apply's rollback moved files back but only called os.Remove(dest),
// which silently no-ops when MkdirAll created nested subdirectories under dest
// for an earlier, successfully-moved item — orphaning empty directories with
// no manifest, invisible to ListBackups.
func TestApplyRollbackRemovesOrphanedNestedDirs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	item1 := filepath.Join(dir, "projects", "proj-a", "s1.jsonl")
	item2 := filepath.Join(dir, "projects", "proj-b", "s2.jsonl")
	writeAged(t, item1, 40*24*time.Hour, now)
	writeAged(t, item2, 40*24*time.Hour, now)

	backupRoot := filepath.Join(dir, BackupDirName)
	plan := Plan{
		Tool:       "claude",
		CodexDir:   dir,
		BackupRoot: backupRoot,
		Items: []PlanItem{
			{Name: "projects/proj-a/s1.jsonl", Path: item1, Size: 4},
			{Name: "projects/proj-b/s2.jsonl", Path: item2, Size: 4},
		},
	}

	// Force item2's Rename to fail (same trick as TestApplyRollsBackOnMoveFailure):
	// pre-create a non-empty directory exactly where it would land, so item1
	// moves successfully (creating dest/projects/proj-a via MkdirAll) before
	// item2's move fails and triggers rollback.
	dest := filepath.Join(backupRoot, now.Format(timestampLayout))
	blocker := filepath.Join(dest, "projects", "proj-b", "s2.jsonl")
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	writeFile(t, filepath.Join(blocker, "keep"), 1) // non-empty so rename can't replace it

	if _, err := plan.Apply(now); err == nil {
		t.Fatal("Apply succeeded despite a blocked destination, want error")
	}

	// item1 must be rolled back to its original path...
	if _, err := os.Stat(item1); err != nil {
		t.Errorf("item1 not rolled back to original path: %v", err)
	}
	// ...and the now-empty nested dir MkdirAll created for it under dest must
	// be gone too, not left as an orphan with no manifest.
	if _, err := os.Stat(filepath.Join(dest, "projects", "proj-a")); !os.IsNotExist(err) {
		t.Error("orphan empty dir left under dest after rollback")
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
