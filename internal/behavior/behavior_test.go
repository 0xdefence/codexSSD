package behavior

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObserveRecordsOnlyNewEntriesWhileAgentRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.jsonl")
	tr := NewTracker("codex", path, []string{"logs_2.sqlite", "sessions"})
	now := time.Now()

	// Nothing new → no events, no file writes.
	if evs := tr.Observe([]string{"logs_2.sqlite", "sessions"}, true, now); len(evs) != 0 {
		t.Errorf("Observe with no change = %v, want none", evs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("provenance file written with nothing to record; must stay low-write")
	}

	// New entry while the agent runs → exactly one event, one JSONL line.
	evs := tr.Observe([]string{"logs_2.sqlite", "sessions", "cache-v2"}, true, now)
	if len(evs) != 1 || evs[0].Entry != "cache-v2" || evs[0].Tool != "codex" {
		t.Fatalf("Observe = %+v, want one cache-v2 event", evs)
	}
	// Same listing again → already seen, no duplicate.
	if evs := tr.Observe([]string{"logs_2.sqlite", "sessions", "cache-v2"}, true, now); len(evs) != 0 {
		t.Errorf("re-observing same entry = %v, want none", evs)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"cache-v2"`) {
		t.Errorf("provenance = %q, want exactly one line naming cache-v2", string(data))
	}
}

func TestObserveIgnoresAppearancesWhileAgentStopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.jsonl")
	tr := NewTracker("codex", path, []string{"a"})
	// Agent not running: we did NOT watch the agent make this, so it is not
	// evidence — record nothing (but remember it, so it isn't misattributed later).
	if evs := tr.Observe([]string{"a", "b"}, false, time.Now()); len(evs) != 0 {
		t.Errorf("Observe while stopped = %v, want none", evs)
	}
	if evs := tr.Observe([]string{"a", "b"}, true, time.Now()); len(evs) != 0 {
		t.Errorf("entry first seen while stopped later attributed to agent: %v", evs)
	}
}

func TestLoadMissingFileIsNil(t *testing.T) {
	evs, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || evs != nil {
		t.Errorf("Load(missing) = %v, %v; want nil, nil", evs, err)
	}
}

// TestLoadSkipsOnlyBadLine pins the "unparseable lines skipped" contract
// literally: a single corrupted line must not take the rest of the history
// down with it. A naive json.Decoder loop that breaks on the first decode
// error would truncate everything after the bad line — this must not happen.
func TestLoadSkipsOnlyBadLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.jsonl")
	content := `{"time":"2026-07-18T10:00:00Z","tool":"codex","entry":"a"}` + "\n" +
		"not json at all\n" +
		`{"time":"2026-07-18T11:00:00Z","tool":"codex","entry":"b"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	evs, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(evs) != 2 || evs[0].Entry != "a" || evs[1].Entry != "b" {
		t.Fatalf("Load = %+v, want events for both a and b, garbage line skipped", evs)
	}
}
