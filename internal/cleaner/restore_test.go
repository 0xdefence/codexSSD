package cleaner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListBackupsNoRoot(t *testing.T) {
	backups, err := ListBackups(t.TempDir()) // no codexssd-backups dir
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("len = %d, want 0", len(backups))
	}
}

func TestRestoreMovesFilesBack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)

	plan, _ := PlanCodexLogs(dir)
	dest, err := plan.Apply(fixedTime())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Sanity: originals gone, one backup listed.
	backups, err := ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("len(backups) = %d, want 1", len(backups))
	}

	if err := Restore(dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Originals are back with correct sizes.
	info, err := os.Stat(filepath.Join(dir, "logs_2.sqlite"))
	if err != nil || info.Size() != 100 {
		t.Errorf("restored logs_2.sqlite = %v (err %v), want size 100", info, err)
	}
	// Backup directory is removed after a clean restore.
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("backup dir still present after restore")
	}
}

func TestRestoreRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)

	plan, _ := PlanCodexLogs(dir)
	dest, err := plan.Apply(fixedTime())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A fresh log appeared at the original location (Codex ran again).
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 7)

	if err := Restore(dest); err == nil {
		t.Fatal("Restore overwrote an existing file, want refusal")
	}
	// The fresh file must be untouched.
	info, _ := os.Stat(filepath.Join(dir, "logs_2.sqlite"))
	if info == nil || info.Size() != 7 {
		t.Errorf("existing file changed during refused restore: %v", info)
	}
}
