// Package tool defines per-tool profiles: where each supported AI coding tool
// keeps its local data, and which of its OWN files codexssd may ever act on
// autonomously.
//
// SAFETY: a Profile's allow-list (OwnFixedFiles + OwnStaleGlobs) is the ONLY
// set of files codexssd may move aside for that tool, and NeverTouch prefixes
// win over any allow-list match. Widening a profile's list is a product
// decision, never a convenience.
package tool

import (
	"fmt"
	"os"
	"path/filepath"
)

// BackupDirName is the recycling-bin root created inside each tool's directory.
// It lives here (not in cleaner) so profiles can exclude it from scans without
// an import cycle; cleaner re-exports it.
const BackupDirName = "codexssd-backups"

// Profile describes one supported AI coding tool.
type Profile struct {
	Name          string   // CLI id, e.g. "codex"
	DisplayName   string   // human name, e.g. "Codex"
	DirName       string   // data dir under $HOME, e.g. ".codex"
	OwnFixedFiles []string // dir-relative files cleanable regardless of age
	OwnStaleGlobs []string // dir-relative globs cleanable only when stale
	NeverTouch    []string // dir-relative prefixes that are NEVER cleanable
	ProcessNames  []string // executable base names identifying the tool
	ProcessHints  []string // command-line substrings identifying the tool
}

// Codex is the founding profile. Its OwnFixedFiles ARE the canonical Codex
// allow-list from Phase 1 — internal/codex re-exports codex.LogFileNames from
// here (from Task 3 onward), so this stays the single source of truth. The
// values are literals because codex imports tool, never the reverse.
func Codex() Profile {
	return Profile{
		Name:          "codex",
		DisplayName:   "Codex",
		DirName:       ".codex", // must match codex.DirName
		OwnFixedFiles: []string{"logs_2.sqlite", "logs_2.sqlite-wal", "logs_2.sqlite-shm"},
		ProcessNames:  []string{"codex"},
		ProcessHints:  []string{"codex app-server", "codex desktop"},
	}
}

// All lists every supported profile. (Claude Code is added in a later task.)
func All() []Profile { return []Profile{Codex()} }

// ByName resolves a CLI --tool value to a profile.
func ByName(name string) (Profile, error) {
	for _, p := range All() {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("unknown tool %q (supported: codex)", name)
}

// Dir returns the tool's data directory under the user's home. Like codex.Dir,
// it does not check existence — callers decide how to handle a missing dir.
func (p Profile) Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p.DirName), nil
}
