package trash

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMoveIntoCollisionSafe(t *testing.T) {
	dir := t.TempDir()
	mk := func(content string) string {
		p := filepath.Join(t.TempDir(), "x.txt")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p1, err := moveInto(dir, mk("1"))
	if err != nil {
		t.Fatalf("moveInto: %v", err)
	}
	p2, err := moveInto(dir, mk("2"))
	if err != nil {
		t.Fatalf("moveInto: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("collision not handled: both went to %s", p1)
	}
	for _, p := range []string{p1, p2} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestMoveSendsToOSTrash(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("trash unsupported on this OS")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))

	src := filepath.Join(t.TempDir(), "logs_2.sqlite")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest, err := Move(src)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source should be gone after Move")
	}
	var landed string
	if runtime.GOOS == "darwin" {
		landed = filepath.Join(home, ".Trash", "logs_2.sqlite")
	} else {
		landed = filepath.Join(home, "xdg", "Trash", "files", "logs_2.sqlite")
	}
	if dest != landed {
		t.Errorf("Move dest = %q, want %q", dest, landed)
	}
	if _, err := os.Stat(landed); err != nil {
		t.Errorf("expected file in trash at %s: %v", landed, err)
	}
}
