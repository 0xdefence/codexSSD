package tool

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAged(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := now.Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func testProfile() Profile {
	return Profile{
		Name:          "testtool",
		DirName:       ".testtool",
		OwnFixedFiles: []string{"logs.db"},
		OwnStaleGlobs: []string{"projects/*/*.jsonl"},
		NeverTouch:    []string{"memory"},
	}
}

func TestCleanablePathsFixedAndStaleGlobs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := 30 * 24 * time.Hour
	writeAged(t, filepath.Join(dir, "logs.db"), time.Minute, now)                         // fixed: age-exempt
	writeAged(t, filepath.Join(dir, "projects", "p1", "old.jsonl"), 40*24*time.Hour, now) // stale: cleanable
	writeAged(t, filepath.Join(dir, "projects", "p1", "new.jsonl"), time.Hour, now)       // fresh: NOT cleanable
	writeAged(t, filepath.Join(dir, "secrets.txt"), 90*24*time.Hour, now)                 // unlisted: NOT cleanable

	got := testProfile().CleanablePaths(dir, now, stale)

	rels := map[string]bool{}
	for _, f := range got {
		rels[f.Rel] = true
	}
	if len(got) != 2 || !rels["logs.db"] || !rels["projects/p1/old.jsonl"] {
		t.Errorf("CleanablePaths = %v, want exactly logs.db + projects/p1/old.jsonl", got)
	}
}

func TestCleanablePathsNeverTouchWins(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	p := testProfile()
	p.OwnStaleGlobs = append(p.OwnStaleGlobs, "memory/*.md") // even an allow-listed glob…
	writeAged(t, filepath.Join(dir, "memory", "fact.md"), 90*24*time.Hour, now)

	if got := p.CleanablePaths(dir, now, 24*time.Hour); len(got) != 0 {
		t.Errorf("CleanablePaths returned %v from a NeverTouch prefix; want none", got)
	}
}

func TestCleanablePathsSkipsRecyclingBin(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	p := Profile{Name: "t", OwnStaleGlobs: []string{"*/manifest.json"}}
	writeAged(t, filepath.Join(dir, BackupDirName, "manifest.json"), 90*24*time.Hour, now)

	if got := p.CleanablePaths(dir, now, 24*time.Hour); len(got) != 0 {
		t.Errorf("CleanablePaths returned %v from the recycling bin; want none", got)
	}
}

func TestAllows(t *testing.T) {
	dir := t.TempDir()
	p := testProfile()
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(dir, "logs.db"), true},
		{filepath.Join(dir, "projects", "p1", "a.jsonl"), true}, // staleness is a clean-time gate, not re-checked here
		{filepath.Join(dir, "memory", "fact.md"), false},
		{filepath.Join(dir, "secrets.txt"), false},
		{filepath.Join(dir, "..", "outside.jsonl"), false},
		{"/somewhere/else/logs.db", false},
	}
	for _, c := range cases {
		if got := p.Allows(dir, c.path); got != c.want {
			t.Errorf("Allows(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestScanDirSize(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(rel string, size int) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("projects/a/sess.jsonl", 100)
	writeFile("shell-snapshots/snap1", 40)
	writeFile("settings.json", 10)
	// The recycling bin must NOT count: our own tidies are not agent writes.
	writeFile(BackupDirName+"/20260101-000000/big.jsonl", 5000)

	if got := ScanDirSize(dir); got != 150 {
		t.Fatalf("ScanDirSize = %d, want 150 (backups excluded)", got)
	}
	if got := ScanDirSize(filepath.Join(dir, "no-such-dir")); got != 0 {
		t.Fatalf("ScanDirSize(missing) = %d, want 0", got)
	}
}
