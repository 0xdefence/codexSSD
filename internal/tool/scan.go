package tool

import (
	"io/fs"
	"os"
	slashpath "path"
	"path/filepath"
	"strings"
	"time"
)

// FoundFile is one file a profile may act on right now.
type FoundFile struct {
	Rel  string // dir-relative, slash-separated (becomes the backup item name)
	Path string
	Size int64
}

// CleanablePaths returns the files under dir this profile may move aside NOW:
// every existing fixed file, plus every stale-glob match at least staleAfter
// old. NeverTouch prefixes and the recycling bin always win.
//
// SAFETY: read-only (Stat/Glob only). The stale gate exists because glob-listed
// files (e.g. session transcripts) may still be wanted by the tool while fresh;
// fixed files (Codex's runaway logs) are cleanable at any age by design.
func (p Profile) CleanablePaths(dir string, now time.Time, staleAfter time.Duration) []FoundFile {
	var out []FoundFile
	add := func(path string, info os.FileInfo) {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if p.offLimits(rel) {
			return
		}
		out = append(out, FoundFile{Rel: rel, Path: path, Size: info.Size()})
	}

	for _, name := range p.OwnFixedFiles {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			add(path, info)
		}
	}
	for _, g := range p.OwnStaleGlobs {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(g)))
		if err != nil {
			continue // a malformed pattern must never brick a command
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}
			if now.Sub(info.ModTime()) < staleAfter {
				continue
			}
			add(m, info)
		}
	}
	return out
}

// Allows is the safety gate the cleaner re-checks every move against: path must
// resolve inside dir and match the allow-list. Staleness is NOT re-checked here
// — it gates what gets planned, while Allows also validates restores of files
// that were already moved aside.
func (p Profile) Allows(dir, path string) bool {
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || p.offLimits(rel) {
		return false
	}
	for _, name := range p.OwnFixedFiles {
		if rel == name {
			return true
		}
	}
	for _, g := range p.OwnStaleGlobs {
		if ok, _ := slashpath.Match(g, rel); ok {
			return true
		}
	}
	return false
}

// offLimits reports whether rel is under a NeverTouch prefix or the bin.
func (p Profile) offLimits(rel string) bool {
	for _, nt := range append([]string{BackupDirName}, p.NeverTouch...) {
		if rel == nt || strings.HasPrefix(rel, nt+"/") {
			return true
		}
	}
	return false
}

// ScanDirSize returns the total size in bytes of all regular files under dir,
// excluding the codexssd-backups recycling bin — so the tool's own tidies are
// never mistaken for agent write activity. A missing or unreadable dir (or any
// unreadable entry) contributes 0 rather than an error: this feeds a live
// monitor, which must degrade gracefully, never fail.
//
// SAFETY: read-only (WalkDir/Stat only).
func ScanDirSize(dir string) int64 {
	var total int64
	root := filepath.Clean(dir)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries; never fatal
		}
		if d.IsDir() {
			if d.Name() == BackupDirName && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
