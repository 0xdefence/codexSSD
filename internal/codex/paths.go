// Package codex knows where Codex keeps its files on disk and which of those
// files codexssd is allowed to reason about.
//
// SAFETY: every path produced here points at Codex's OWN data (under ~/.codex).
// codexssd never derives paths into a user's project from this package.
package codex

import (
	"os"
	"path/filepath"
)

// DirName is the directory Codex uses under the user's home directory.
const DirName = ".codex" // must match tool.Codex().DirName

// Dir returns the absolute path to the user's ~/.codex directory.
//
// It does not check whether the directory exists — callers decide how to handle
// a missing directory (see DirExists). This keeps Dir a pure, read-only lookup.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// DirExists reports whether the given path exists and is a directory.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
