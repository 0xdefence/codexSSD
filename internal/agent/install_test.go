package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWritesFile(t *testing.T) {
	dir := t.TempDir()
	path, foreign, err := Install(dir, ProfileBalanced, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if foreign {
		t.Error("replacedForeign should be false on a fresh write")
	}
	if path != filepath.Join(dir, FileName) {
		t.Errorf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(data), marker) {
		t.Error("written file should carry the generated marker")
	}
}

func TestInstallRefusesExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Install(dir, ProfileBalanced, false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Second install without --force must refuse and leave the file intact.
	before, _ := os.ReadFile(filepath.Join(dir, FileName))
	_, _, err := Install(dir, ProfileStrict, false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(before) != string(after) {
		t.Error("refused install must not change the file")
	}
}

func TestInstallForceOverwritesOwnFile(t *testing.T) {
	dir := t.TempDir()
	Install(dir, ProfileBalanced, false)
	_, foreign, err := Install(dir, ProfileStrict, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if foreign {
		t.Error("overwriting our own generated file should not report foreign")
	}
}

func TestInstallForceReportsForeign(t *testing.T) {
	dir := t.TempDir()
	// A hand-written AGENTS.md (no marker).
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("# my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, foreign, err := Install(dir, ProfileBalanced, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if !foreign {
		t.Error("force over a non-CodexSSD file should report replacedForeign=true")
	}
}
