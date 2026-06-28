// Package trash moves files and directories into the operating system's Trash.
// A "released" item stays recoverable by the user; CodexSSD never hard-deletes,
// and emptying the Trash is the user's own explicit action.
//
// stdlib-only.
package trash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ErrUnsupported is returned on platforms without a known Trash location.
var ErrUnsupported = errors.New("trash not supported on this platform")

// Move moves path into the OS Trash.
func Move(path string) error {
	switch runtime.GOOS {
	case "darwin":
		dir, err := macTrashDir()
		if err != nil {
			return err
		}
		_, err = moveInto(dir, path)
		return err
	case "linux":
		return moveLinuxXDG(path)
	default:
		return ErrUnsupported
	}
}

func macTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func xdgTrashRoot() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

func moveLinuxXDG(path string) error {
	root, err := xdgTrashRoot()
	if err != nil {
		return err
	}
	filesDir := filepath.Join(root, "files")
	infoDir := filepath.Join(root, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	target, err := moveInto(filesDir, path)
	if err != nil {
		return err
	}
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", abs, time.Now().Format("2006-01-02T15:04:05"))
	return os.WriteFile(filepath.Join(infoDir, filepath.Base(target)+".trashinfo"), []byte(info), 0o600)
}

// moveInto moves path into dir, choosing a collision-safe name. Returns the
// final path. SAFETY: a plain os.Rename — a move, never a delete.
func moveInto(dir, path string) (string, error) {
	target := uniqueName(filepath.Join(dir, filepath.Base(path)))
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

func uniqueName(target string) string {
	if !pathExists(target) {
		return target
	}
	ext := filepath.Ext(target)
	stem := target[:len(target)-len(ext)]
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !pathExists(cand) {
			return cand
		}
	}
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
