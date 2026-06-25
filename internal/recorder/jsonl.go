// Package recorder persists CodexSSD's own session history as a simple JSONL
// file (one record per session).
//
// SAFETY / DESIGN: deliberately NOT a database. A tool that guards against
// aggressive local SQLite writes must not do the same itself — so storage is a
// lightweight append-only plain-text file, written once per session.
package recorder

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// errNotImplemented marks scaffolding that has a defined shape but no logic yet.
var errNotImplemented = errors.New("not implemented")

// DirName is CodexSSD's own state directory under the user's home directory.
const DirName = ".codexssd"

// FileName is the append-only session-history file inside DirName.
const FileName = "sessions.jsonl"

// Receipt is the single record appended at the end of a session.
type Receipt struct {
	At           time.Time `json:"at"`
	DurationSec  float64   `json:"duration_sec"`
	DiskWritten  int64     `json:"disk_written_bytes"`
	PeakMBPerMin float64   `json:"peak_mb_per_min"`
	FilesChanged int       `json:"files_changed"`
	Risk         string    `json:"risk"`
}

// Path returns the absolute path to the session-history file
// (~/.codexssd/sessions.jsonl). It does not create anything.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName, FileName), nil
}

// Append writes one Receipt as a single JSON line.
//
// DESIGN: append-only, capped history (see docs); never a database.
//
// STUB: not implemented yet.
func Append(r Receipt) error {
	return errNotImplemented
}
