package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanLogsMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	report := ScanLogs(dir)

	if report.DirExists {
		t.Errorf("DirExists = true, want false for %q", dir)
	}
	if report.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", report.TotalBytes)
	}
	if len(report.Files) != len(LogFileNames) {
		t.Fatalf("len(Files) = %d, want %d", len(report.Files), len(LogFileNames))
	}
	for _, f := range report.Files {
		if f.Exists {
			t.Errorf("file %q reported as existing in a missing dir", f.Name)
		}
	}
}

func TestScanLogsReportsSizesAndTotal(t *testing.T) {
	dir := t.TempDir()

	// logs_2.sqlite = 100 bytes, logs_2.sqlite-wal = 50 bytes, -shm absent.
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)

	report := ScanLogs(dir)

	if !report.DirExists {
		t.Fatalf("DirExists = false, want true for %q", dir)
	}
	if report.TotalBytes != 150 {
		t.Errorf("TotalBytes = %d, want 150", report.TotalBytes)
	}

	byName := map[string]LogFile{}
	for _, f := range report.Files {
		byName[f.Name] = f
	}

	if f := byName["logs_2.sqlite"]; !f.Exists || f.Size != 100 {
		t.Errorf("logs_2.sqlite = %+v, want exists with size 100", f)
	}
	if f := byName["logs_2.sqlite-wal"]; !f.Exists || f.Size != 50 {
		t.Errorf("logs_2.sqlite-wal = %+v, want exists with size 50", f)
	}
	if f := byName["logs_2.sqlite-shm"]; f.Exists || f.Size != 0 {
		t.Errorf("logs_2.sqlite-shm = %+v, want absent with size 0", f)
	}
}

// writeFile creates a file of exactly n bytes for testing.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}
