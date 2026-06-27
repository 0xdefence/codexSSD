// Package agent installs "please behave" rules (an AGENTS.md file) for AI coding
// agents, to reduce avoidable disk and token churn at the source.
//
// SAFETY: it writes a single new file into a user-chosen repo and never deletes
// anything. It refuses to overwrite an existing AGENTS.md unless forced, and
// marks its own files so a forced overwrite can warn when it replaces a
// hand-written one.
package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the file written into the target repository.
const FileName = "AGENTS.md"

// ErrExists is returned when an AGENTS.md already exists and force is false.
var ErrExists = errors.New("AGENTS.md already exists")

// Install writes an AGENTS.md for the given profile into dir.
//
// If the file exists and force is false, it returns ErrExists and writes
// nothing. With force, it overwrites; replacedForeign is true when the existing
// file was NOT one CodexSSD generated (i.e. likely hand-written).
func Install(dir string, p Profile, force bool) (path string, replacedForeign bool, err error) {
	path = filepath.Join(dir, FileName)
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		if !force {
			return "", false, ErrExists
		}
		replacedForeign = !isGenerated(path)
	}
	if err := os.WriteFile(path, []byte(Content(p)), 0o644); err != nil {
		return "", false, err
	}
	return path, replacedForeign, nil
}

// isGenerated reports whether the file at path was written by CodexSSD (carries
// the marker on its first line).
func isGenerated(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(data), marker)
}
