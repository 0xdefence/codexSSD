// Package behavior implements Phase 4's behavioral detection: while `watch`
// runs, notice new top-level entries appearing in the tool's directory and
// record that provenance — "I watched this appear during an agent session" is
// a far stronger signal than guessing by name.
//
// SAFETY: observation only — it never touches the entries it records. It
// appends one small JSONL line per NEW entry (never per poll, never a
// database), in keeping with the low-write promise.
package behavior

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/recorder"
)

// FileName is the provenance file inside CodexSSD's state dir.
const FileName = "provenance.jsonl"

// Event records one entry observed appearing while the agent ran.
type Event struct {
	Time  time.Time `json:"time"`
	Tool  string    `json:"tool"`
	Entry string    `json:"entry"`
}

// Tracker diffs successive directory listings against what it has seen.
type Tracker struct {
	tool string
	path string
	seen map[string]bool
}

// NewTracker starts from the entries present before watching began — those are
// pre-existing, so they are never attributed to the watched session.
func NewTracker(toolName, provenancePath string, initial []string) *Tracker {
	seen := make(map[string]bool, len(initial))
	for _, n := range initial {
		seen[n] = true
	}
	return &Tracker{tool: toolName, path: provenancePath, seen: seen}
}

// Observe records entries appearing for the first time. Only appearances while
// agentRunning become events — an entry first seen while the agent was stopped
// is remembered but never attributed (we did not watch it being made).
// Append failures are swallowed: provenance is best-effort and must never
// disturb the watch loop.
func (t *Tracker) Observe(names []string, agentRunning bool, now time.Time) []Event {
	var events []Event
	for _, n := range names {
		if t.seen[n] {
			continue
		}
		t.seen[n] = true
		if !agentRunning {
			continue
		}
		events = append(events, Event{Time: now, Tool: t.tool, Entry: n})
	}
	if len(events) > 0 {
		_ = appendEvents(t.path, events)
	}
	return events
}

// appendEvents writes one JSONL line per event.
func appendEvents(path string, events []Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// ProvenancePath is <state-dir>/provenance.jsonl.
func ProvenancePath() (string, error) {
	dir, err := recorder.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads recorded events; a missing file means nothing recorded (nil, nil).
// Unparseable lines are skipped — a damaged history must not brick `report`.
func Load(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var events []Event
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var e Event
		if err := dec.Decode(&e); err != nil {
			break
		}
		events = append(events, e)
	}
	return events, nil
}
