package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedTime() time.Time {
	return time.Date(2026, 6, 26, 14, 30, 0, 0, time.UTC)
}

func TestApplyMovesFilesAndWritesManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}

	dest, err := plan.Apply(fixedTime())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Originals are gone from the Codex dir.
	if _, err := os.Stat(filepath.Join(dir, "logs_2.sqlite")); !os.IsNotExist(err) {
		t.Errorf("logs_2.sqlite still present in codex dir")
	}

	// Files exist in the timestamped backup dir.
	wantDest := filepath.Join(dir, BackupDirName, "20260626-143000")
	if dest != wantDest {
		t.Errorf("dest = %q, want %q", dest, wantDest)
	}
	info, err := os.Stat(filepath.Join(dest, "logs_2.sqlite"))
	if err != nil || info.Size() != 100 {
		t.Errorf("moved logs_2.sqlite = %v (err %v), want size 100", info, err)
	}

	// Manifest is present and correct.
	m, err := readManifest(dest)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if len(m.Items) != 2 {
		t.Fatalf("manifest items = %d, want 2", len(m.Items))
	}
	if !m.MovedAt.Equal(fixedTime()) {
		t.Errorf("MovedAt = %v, want %v", m.MovedAt, fixedTime())
	}
	if !m.HoldUntil.Equal(fixedTime().AddDate(0, 0, RetentionDays)) {
		t.Errorf("HoldUntil = %v, want +%d days", m.HoldUntil, RetentionDays)
	}
	if m.Items[0].OriginalPath != filepath.Join(dir, "logs_2.sqlite") {
		t.Errorf("OriginalPath = %q", m.Items[0].OriginalPath)
	}
}

func TestApplyRefusesNonCodexFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "important.txt"), 10)

	// Hand-craft a malicious plan pointing at a non-Codex file.
	plan := Plan{
		CodexDir:   dir,
		BackupRoot: filepath.Join(dir, BackupDirName),
		Items:      []PlanItem{{Name: "important.txt", Path: filepath.Join(dir, "important.txt"), Size: 10}},
		TotalBytes: 10,
	}

	if _, err := plan.Apply(fixedTime()); err == nil {
		t.Fatal("Apply succeeded on a non-Codex file, want error")
	}
	// The file must be untouched.
	if _, err := os.Stat(filepath.Join(dir, "important.txt")); err != nil {
		t.Errorf("non-Codex file was moved/removed: %v", err)
	}
}

func TestApplyEmptyPlanErrors(t *testing.T) {
	plan := Plan{CodexDir: t.TempDir()}
	if _, err := plan.Apply(fixedTime()); err == nil {
		t.Error("Apply on empty plan succeeded, want error")
	}
}

func TestApplyRollsBackOnMoveFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}

	// Force the SECOND move to fail: pre-create a non-empty directory exactly
	// where logs_2.sqlite-wal would be moved, so os.Rename onto it errors after
	// the first file has already moved.
	dest := filepath.Join(dir, BackupDirName, "20260626-143000")
	blocker := filepath.Join(dest, "logs_2.sqlite-wal")
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	writeFile(t, filepath.Join(blocker, "keep"), 1) // make it non-empty so rename can't replace it

	if _, err := plan.Apply(fixedTime()); err == nil {
		t.Fatal("Apply succeeded despite a blocked destination, want error")
	}

	// Rollback must have returned the first file to the Codex dir; the second
	// was never moved — so both originals are present and intact.
	if info, err := os.Stat(filepath.Join(dir, "logs_2.sqlite")); err != nil || info.Size() != 100 {
		t.Errorf("logs_2.sqlite not rolled back: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "logs_2.sqlite-wal")); err != nil || info.Size() != 50 {
		t.Errorf("logs_2.sqlite-wal should be untouched: info=%v err=%v", info, err)
	}
}

func TestApplyWithHoldSetsManifestHoldUntil(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "logs_2.sqlite"), []byte("x"), 0o600)
	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	dest, err := plan.ApplyWithHold(now, 3*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(dest)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(3 * 24 * time.Hour); !m.HoldUntil.Equal(want) {
		t.Errorf("HoldUntil = %v, want %v", m.HoldUntil, want)
	}
}
