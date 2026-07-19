// Package tool defines per-tool profiles: where each supported AI coding tool
// keeps its local data, and which of its OWN files codexssd may ever act on
// autonomously.
package tool

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProfileRegistered(t *testing.T) {
	p, err := ByName("claude")
	if err != nil {
		t.Fatalf("ByName(claude) error = %v", err)
	}
	if p.DisplayName != "Claude Code" || p.DirName != ".claude" {
		t.Errorf("Claude profile = %+v, want display 'Claude Code', dir '.claude'", p)
	}
	if len(p.OwnFixedFiles) != 0 {
		t.Errorf("Claude has fixed files %v; transcripts must be stale-gated, never age-exempt", p.OwnFixedFiles)
	}
}

func TestClaudeCleanablePicksOnlyStaleTranscriptsAndSnapshots(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := 30 * 24 * time.Hour
	old := 40 * 24 * time.Hour

	writeAged(t, filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl"), old, now)       // cleanable
	writeAged(t, filepath.Join(dir, "projects", "-Users-jo-app", "s2.jsonl"), time.Hour, now) // fresh → keep
	writeAged(t, filepath.Join(dir, "shell-snapshots", "snap.sh"), old, now)                  // cleanable
	writeAged(t, filepath.Join(dir, "memory", "MEMORY.md"), old, now)                         // never
	writeAged(t, filepath.Join(dir, "settings.json"), old, now)                               // never
	writeAged(t, filepath.Join(dir, "todos", "t.json"), old, now)                             // never
	writeAged(t, filepath.Join(dir, "CLAUDE.md"), old, now)                                   // never

	got := Claude().CleanablePaths(dir, now, stale)
	rels := map[string]bool{}
	for _, f := range got {
		rels[f.Rel] = true
	}
	if len(got) != 2 || !rels["projects/-Users-jo-app/s1.jsonl"] || !rels["shell-snapshots/snap.sh"] {
		t.Errorf("CleanablePaths = %v, want exactly the stale transcript + snapshot", got)
	}
}

func TestClaudeAllowsNeverTouchesMemory(t *testing.T) {
	dir := t.TempDir()
	if Claude().Allows(dir, filepath.Join(dir, "memory", "fact.md")) {
		t.Error("Allows must reject memory/ even hypothetically")
	}
	if !Claude().Allows(dir, filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl")) {
		t.Error("Allows must accept a projects transcript")
	}
}
