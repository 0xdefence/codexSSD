package tool

import "testing"

func TestParseRSSKiB(t *testing.T) {
	// ps -o rss= output: one KiB value per line; junk lines are skipped.
	got := parseRSSKiB(" 1024\n2048\n\nnot-a-number\n")
	if got != 3072 {
		t.Fatalf("parseRSSKiB = %d, want 3072", got)
	}
}

func TestProcessMemoryNoProcesses(t *testing.T) {
	// A profile no real process matches: absence is (0, nil), not an error.
	p := Profile{Name: "definitely-not-running-xyz", ProcessNames: []string{"definitely-not-running-xyz"}}
	mem, err := ProcessMemory(p)
	if err != nil {
		t.Fatalf("ProcessMemory error: %v", err)
	}
	if mem != 0 {
		t.Fatalf("ProcessMemory = %d, want 0", mem)
	}
}
