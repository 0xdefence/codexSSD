package tool

import (
	"path/filepath"
	"strings"
	"testing"
)

// NOTE: this test intentionally does not import internal/codex (see the
// Interfaces note — codex will import tool from Task 3 onward). The expected
// literals below ARE the documented Codex allow-list.
func TestCodexProfileValues(t *testing.T) {
	p := Codex()
	if p.Name != "codex" || p.DisplayName != "Codex" || p.DirName != ".codex" {
		t.Errorf("Codex() = %+v, want name codex / display Codex / dir .codex", p)
	}
	want := []string{"logs_2.sqlite", "logs_2.sqlite-wal", "logs_2.sqlite-shm"}
	if len(p.OwnFixedFiles) != len(want) {
		t.Fatalf("OwnFixedFiles = %v, want %v", p.OwnFixedFiles, want)
	}
	for i, name := range want {
		if p.OwnFixedFiles[i] != name {
			t.Errorf("OwnFixedFiles[%d] = %q, want %q", i, p.OwnFixedFiles[i], name)
		}
	}
}

func TestByName(t *testing.T) {
	if _, err := ByName("codex"); err != nil {
		t.Errorf("ByName(codex) error = %v, want nil", err)
	}
	if _, err := ByName("clippy"); err == nil || !strings.Contains(err.Error(), "clippy") {
		t.Errorf("ByName(clippy) error = %v, want unknown-tool error naming clippy", err)
	}
}

func TestDirIsUnderHome(t *testing.T) {
	dir, err := Codex().Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if filepath.Base(dir) != ".codex" {
		t.Errorf("Dir() = %q, want basename .codex", dir)
	}
}
