package cleaner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpiredFilterBoundary(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	backups := []Backup{
		{Dir: "past", Manifest: Manifest{HoldUntil: now.Add(-time.Hour)}},
		{Dir: "exactly", Manifest: Manifest{HoldUntil: now}}, // boundary → released
		{Dir: "future", Manifest: Manifest{HoldUntil: now.Add(time.Hour)}},
	}
	got := Expired(backups, now)
	if len(got) != 2 {
		t.Fatalf("Expired len = %d, want 2 (past + exactly)", len(got))
	}
	if got[0].Dir != "past" || got[1].Dir != "exactly" {
		t.Errorf("Expired = %v, want [past exactly]", []string{got[0].Dir, got[1].Dir})
	}
}

// mkBackup writes a backup dir with a manifest holding until `hold`.
func mkBackup(t *testing.T, codexDir, id string, hold time.Time) string {
	t.Helper()
	bd := filepath.Join(codexDir, BackupDirName, id)
	if err := os.MkdirAll(bd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bd, "logs_2.sqlite"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeManifest(bd, Manifest{MovedAt: hold.AddDate(0, 0, -RetentionDays), HoldUntil: hold})
	return bd
}

func TestReleaseExpiredMovesOnlyExpired(t *testing.T) {
	prev := moveToTrash
	t.Cleanup(func() { moveToTrash = prev })
	var moved []string
	moveToTrash = func(p string) (string, error) {
		moved = append(moved, filepath.Base(p))
		return filepath.Join("/trash", filepath.Base(p)), nil
	}

	codexDir := t.TempDir()
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	mkBackup(t, codexDir, "20260601-000000", now.Add(-time.Hour))   // expired
	mkBackup(t, codexDir, "20260629-000000", now.Add(48*time.Hour)) // not expired

	released, trashDir, err := ReleaseExpired(codexDir, now)
	if err != nil {
		t.Fatalf("ReleaseExpired: %v", err)
	}
	if len(released) != 1 || released[0] != "20260601-000000" {
		t.Fatalf("released = %v, want [20260601-000000]", released)
	}
	if len(moved) != 1 || moved[0] != "20260601-000000" {
		t.Errorf("moved = %v, want [20260601-000000]", moved)
	}
	if want := "/trash"; trashDir != want {
		t.Errorf("trashDir = %q, want %q", trashDir, want)
	}
}

// TestReleaseExpiredTrashDirIsLastSuccessfulRelease guards the documented
// behavior for multiple expired backups: trashDir reflects the destination of
// the LAST successful Release, not the first.
func TestReleaseExpiredTrashDirIsLastSuccessfulRelease(t *testing.T) {
	prev := moveToTrash
	t.Cleanup(func() { moveToTrash = prev })
	moveToTrash = func(p string) (string, error) {
		base := filepath.Base(p)
		return filepath.Join("/trash", base, base), nil
	}

	codexDir := t.TempDir()
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	mkBackup(t, codexDir, "20260601-000000", now.Add(-2*time.Hour)) // expired
	mkBackup(t, codexDir, "20260602-000000", now.Add(-time.Hour))   // expired

	released, trashDir, err := ReleaseExpired(codexDir, now)
	if err != nil {
		t.Fatalf("ReleaseExpired: %v", err)
	}
	if len(released) != 2 {
		t.Fatalf("released = %v, want 2 backups", released)
	}
	if want := filepath.Join("/trash", "20260602-000000"); trashDir != want {
		t.Errorf("trashDir = %q, want %q (the last successful release's directory)", trashDir, want)
	}
}

func TestReleaseExpiredNothingReleasedHasEmptyTrashDir(t *testing.T) {
	codexDir := t.TempDir()
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	mkBackup(t, codexDir, "20260629-000000", now.Add(48*time.Hour)) // not expired

	released, trashDir, err := ReleaseExpired(codexDir, now)
	if err != nil {
		t.Fatalf("ReleaseExpired: %v", err)
	}
	if len(released) != 0 {
		t.Errorf("released = %v, want none", released)
	}
	if trashDir != "" {
		t.Errorf("trashDir = %q, want empty when nothing was released", trashDir)
	}
}

func TestReleaseRefusesNonBackupPath(t *testing.T) {
	prev := moveToTrash
	t.Cleanup(func() { moveToTrash = prev })
	called := false
	moveToTrash = func(string) (string, error) { called = true; return "", nil }

	if _, err := Release(filepath.Join(t.TempDir(), "not-a-backup")); err == nil {
		t.Fatal("Release should refuse a path not under codexssd-backups/")
	}
	if called {
		t.Error("moveToTrash must not be called for a refused path")
	}
}

func TestReleaseExpiredKeepsHeldOnTrashFailure(t *testing.T) {
	prev := moveToTrash
	t.Cleanup(func() { moveToTrash = prev })
	moveToTrash = func(string) (string, error) { return "", errors.New("trash unavailable") }

	codexDir := t.TempDir()
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	bd := mkBackup(t, codexDir, "20260601-000000", now.Add(-time.Hour)) // expired

	released, trashDir, err := ReleaseExpired(codexDir, now)
	if err == nil {
		t.Fatal("ReleaseExpired should surface the trash error")
	}
	if len(released) != 0 {
		t.Errorf("released = %v, want none on trash failure", released)
	}
	if trashDir != "" {
		t.Errorf("trashDir = %q, want empty on trash failure", trashDir)
	}
	if _, statErr := os.Stat(bd); statErr != nil {
		t.Errorf("backup must remain on disk (held) when trashing fails: %v", statErr)
	}
}
