// Package visibility produces the read-only "what's eating disk in ~/.codex"
// report — Phase 2's noticing-for-you promise.
//
// SAFETY: Scan is 100% read-only, and it only ever looks inside the directory
// it is given (in production, ~/.codex). It never reads user project trees.
// Finding something stale is only ever REPORTED — never acted on.
package visibility

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
)

type Entry struct {
	Name       string    `json:"name"`
	IsDir      bool      `json:"is_dir"`
	TotalBytes int64     `json:"total_bytes"`
	FileCount  int       `json:"file_count"`
	NewestMod  time.Time `json:"newest_mod"`
	Stale      bool      `json:"stale"`
	IsOurs     bool      `json:"is_ours"` // true for the codexssd-backups bin
	ReadError  string    `json:"read_error,omitempty"`
}

type Report struct {
	Dir        string  `json:"dir"`
	DirExists  bool    `json:"dir_exists"`
	Entries    []Entry `json:"entries"` // sorted by TotalBytes descending; [] never null
	TotalBytes int64   `json:"total_bytes"`
}

// Scan walks dir (read-only) and aggregates disk usage by top-level entry.
// Subtree read errors degrade to Entry.ReadError — the report never fails
// outright. `now` and `staleAfter` are injected for testability.
func Scan(dir string, now time.Time, staleAfter time.Duration) Report {
	r := Report{Dir: dir, Entries: []Entry{}} // [] not null in JSON
	tops, err := os.ReadDir(dir)
	if err != nil {
		return r // missing/unreadable dir: DirExists stays false
	}
	r.DirExists = true

	for _, t := range tops {
		e := Entry{Name: t.Name(), IsDir: t.IsDir(), IsOurs: t.Name() == cleaner.BackupDirName}
		walkErr := filepath.WalkDir(filepath.Join(dir, t.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			e.TotalBytes += info.Size()
			e.FileCount++
			if info.ModTime().After(e.NewestMod) {
				e.NewestMod = info.ModTime()
			}
			return nil
		})
		if walkErr != nil {
			// Keep whatever we managed to count; report the problem in place.
			e.ReadError = walkErr.Error()
		}
		e.Stale = e.FileCount > 0 && now.Sub(e.NewestMod) >= staleAfter
		r.TotalBytes += e.TotalBytes
		r.Entries = append(r.Entries, e)
	}

	sort.Slice(r.Entries, func(i, j int) bool { return r.Entries[i].TotalBytes > r.Entries[j].TotalBytes })
	return r
}
