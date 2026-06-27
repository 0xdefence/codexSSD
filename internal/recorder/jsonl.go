// Package recorder persists CodexSSD's own session history as a simple JSONL
// file (one record per session).
//
// SAFETY / DESIGN: deliberately NOT a database. A tool that guards against
// aggressive local SQLite writes must not do the same itself — storage is a
// lightweight append-only plain-text file, written once per session and trimmed
// to a record cap.
package recorder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirName is CodexSSD's own state directory under the user's home directory.
const DirName = ".codexssd"

// FileName is the append-only session-history file inside DirName.
const FileName = "sessions.jsonl"

// maxRecords caps how many session receipts are kept (oldest trimmed first).
const maxRecords = 1000

// Receipt is the single record appended at the end of a session.
type Receipt struct {
	At           time.Time `json:"at"`
	DurationSec  float64   `json:"duration_sec"`
	DiskWritten  int64     `json:"disk_written_bytes"`
	PeakMBPerMin float64   `json:"peak_mb_per_min"`
	FilesChanged int       `json:"files_changed"`
	Risk         string    `json:"risk"`
}

// Dir returns the absolute path to CodexSSD's state directory (~/.codexssd).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// Path returns the absolute path to the session-history file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Append writes one Receipt as a single JSON line, then trims to maxRecords.
//
// DESIGN: append-only JSONL, capped history; never a database.
func Append(r Receipt) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return appendTo(path, r, maxRecords)
}

// appendTo appends r to the JSONL file at path and trims to max records.
func appendTo(path string, r Receipt, max int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return trimToMax(path, max)
}

// readLines returns the non-empty lines of the file at path (none if missing).
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// trimToMax rewrites path keeping only the most-recent max lines (no-op if at
// or under the cap). This is the only non-append write, and only when needed.
func trimToMax(path string, max int) error {
	if max <= 0 {
		return nil
	}
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	if len(lines) <= max {
		return nil
	}
	kept := lines[len(lines)-max:]
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}
