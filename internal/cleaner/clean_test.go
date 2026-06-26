package cleaner

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a file of exactly n bytes for testing.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}

func TestPlanCodexLogsEmpty(t *testing.T) {
	dir := t.TempDir() // no log files inside

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("Empty() = false, want true for %+v", plan)
	}
	if plan.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", plan.TotalBytes)
	}
}

func TestPlanCodexLogsListsPresentFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)
	// -shm intentionally absent

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	if plan.Empty() {
		t.Fatal("Empty() = true, want false")
	}
	if len(plan.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 (%+v)", len(plan.Items), plan.Items)
	}
	if plan.TotalBytes != 150 {
		t.Errorf("TotalBytes = %d, want 150", plan.TotalBytes)
	}
	wantRoot := filepath.Join(dir, BackupDirName)
	if plan.BackupRoot != wantRoot {
		t.Errorf("BackupRoot = %q, want %q", plan.BackupRoot, wantRoot)
	}
}
