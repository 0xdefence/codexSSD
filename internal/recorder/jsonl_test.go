package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendToWritesOneLinePerCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	for i := 0; i < 3; i++ {
		if err := appendTo(path, Receipt{Risk: "LOW", DurationSec: float64(i)}, 1000); err != nil {
			t.Fatalf("appendTo: %v", err)
		}
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatalf("readLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
}

func TestAppendToTrimsToCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	for i := 0; i < 5; i++ {
		if err := appendTo(path, Receipt{DurationSec: float64(i)}, 3); err != nil {
			t.Fatalf("appendTo: %v", err)
		}
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatalf("readLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (capped)", len(lines))
	}
	// The newest (DurationSec 4) must be retained; the oldest (0,1) dropped.
	if want := `"duration_sec":4`; !strings.Contains(lines[2], want) {
		t.Errorf("last line = %q, want it to contain %q", lines[2], want)
	}
}

func TestReadLinesMissingFile(t *testing.T) {
	lines, err := readLines(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines = %d, want 0", len(lines))
	}
}

func TestSummarizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")

	s, err := SummarizeFile(path)
	if err != nil || s.Records != 0 {
		t.Fatalf("missing file: got %+v, %v; want zero summary, nil", s, err)
	}

	at := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	appendTo(path, Receipt{At: at, Action: "clean", BytesMoved: 100, BackupID: "20260704-120000"}, 10)
	appendTo(path, Receipt{At: at.Add(time.Hour), Action: "restore", BackupID: "20260704-120000"}, 10)

	s, err = SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 2 || s.LastAction != "restore" || !s.LastAt.Equal(at.Add(time.Hour)) {
		t.Errorf("got %+v", s)
	}
}

func TestSummarizeFileSkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	os.WriteFile(path, []byte("{not json}\n{\"at\":\"2026-07-04T12:00:00Z\",\"action\":\"clean\"}\n"), 0o600)
	s, err := SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 1 || s.LastAction != "clean" {
		t.Errorf("got %+v", s)
	}
}

func TestDirUnderHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/whatever-home")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != filepath.Join("/tmp/whatever-home", DirName) {
		t.Errorf("Dir = %q", dir)
	}
}
