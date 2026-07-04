package visibility

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

const staleAfter = 30 * 24 * time.Hour

func write(t *testing.T, path string, size int, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestScanMissingDir(t *testing.T) {
	r := Scan(filepath.Join(t.TempDir(), "nope"), now, staleAfter)
	if r.DirExists {
		t.Error("DirExists should be false")
	}
	if r.Entries == nil {
		t.Error("Entries must be an empty slice, not nil (JSON [] not null)")
	}
}

func TestScanAggregatesAndFlagsStale(t *testing.T) {
	dir := t.TempDir()
	fresh := now.Add(-time.Hour)
	old := now.Add(-90 * 24 * time.Hour)
	write(t, filepath.Join(dir, "logs_2.sqlite"), 100, fresh)
	write(t, filepath.Join(dir, "sessions", "a", "one.jsonl"), 300, old)
	write(t, filepath.Join(dir, "sessions", "b", "two.jsonl"), 200, old)
	write(t, filepath.Join(dir, "codexssd-backups", "20260101-000000", "logs_2.sqlite"), 50, old)

	r := Scan(dir, now, staleAfter)
	if !r.DirExists {
		t.Fatal("DirExists should be true")
	}
	if r.TotalBytes != 650 {
		t.Errorf("TotalBytes = %d, want 650", r.TotalBytes)
	}
	// Sorted by size desc: sessions (500) first.
	if r.Entries[0].Name != "sessions" || r.Entries[0].TotalBytes != 500 || r.Entries[0].FileCount != 2 {
		t.Errorf("entries[0] = %+v", r.Entries[0])
	}
	if !r.Entries[0].Stale {
		t.Error("sessions should be stale (90 days old)")
	}
	byName := map[string]Entry{}
	for _, e := range r.Entries {
		byName[e.Name] = e
	}
	if byName["logs_2.sqlite"].Stale {
		t.Error("fresh file must not be stale")
	}
	if !byName["codexssd-backups"].IsOurs {
		t.Error("the recycling bin must be marked IsOurs")
	}
}

func TestScanJSONShape(t *testing.T) {
	r := Scan(filepath.Join(t.TempDir(), "nope"), now, staleAfter)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data)[0] != '{' {
		t.Fatal("unexpected JSON")
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
}
