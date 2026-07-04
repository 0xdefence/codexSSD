package self

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureSumsStateDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sessions.jsonl"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x"), make([]byte, 50), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Measure(dir)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if r.HistoryBytes != 150 {
		t.Errorf("HistoryBytes = %d, want 150", r.HistoryBytes)
	}
	if r.Mode != "low-write" {
		t.Errorf("Mode = %q, want low-write", r.Mode)
	}
	if r.StateDir != dir {
		t.Errorf("StateDir = %q, want %q", r.StateDir, dir)
	}
}

func TestMeasureIncludesHistorySummary(t *testing.T) {
	dir := t.TempDir()
	line := `{"at":"2026-07-04T12:00:00Z","action":"clean","bytes_moved":42}` + "\n"
	os.WriteFile(filepath.Join(dir, "sessions.jsonl"), []byte(line), 0o600)
	r, err := Measure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Records != 1 || r.LastAction != "clean" {
		t.Errorf("got %+v", r)
	}
}

func TestMeasureMissingDirIsZero(t *testing.T) {
	r, err := Measure(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if r.HistoryBytes != 0 {
		t.Errorf("HistoryBytes = %d, want 0", r.HistoryBytes)
	}
}
