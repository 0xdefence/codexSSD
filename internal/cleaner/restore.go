package cleaner

import (
	"fmt"
	"os"
	"path/filepath"
)

// Backup is one moved-aside set, identified by its directory and manifest.
type Backup struct {
	Dir      string   `json:"dir"`
	Manifest Manifest `json:"manifest"`
}

// ListBackups returns the recoverable backups under codexDir's recycling bin,
// newest-last (directory names are timestamped, so lexical order is chronological).
// Directories without a readable manifest are skipped.
func ListBackups(codexDir string) ([]Backup, error) {
	root := filepath.Join(codexDir, BackupDirName)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var backups []Backup
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		m, err := readManifest(dir)
		if err != nil {
			continue // not a valid backup; ignore
		}
		backups = append(backups, Backup{Dir: dir, Manifest: m})
	}
	return backups, nil
}

// Restore moves every file in backupDir's manifest back to its original path,
// then removes the emptied backup directory.
//
// SAFETY: renames only; every path re-validated against the owning tool's
// allow-list; never overwrites an existing file; rolls back on ANY partial
// failure (a failed Rename or a failed MkdirAll for a later item's parent
// dir both undo every item already restored this call).
func Restore(backupDir string) error {
	m, err := readManifest(backupDir)
	if err != nil {
		return err
	}
	prof, err := profileFor(m.Tool)
	if err != nil {
		return err
	}
	// The bin lives at <toolDir>/codexssd-backups/<timestamp>.
	toolDir := filepath.Dir(filepath.Dir(backupDir))
	for _, it := range m.Items {
		if !prof.Allows(toolDir, it.OriginalPath) {
			return fmt.Errorf("refusing to restore file outside %s's own-file allow-list: %s", prof.DisplayName, it.OriginalPath)
		}
		if _, err := os.Stat(it.OriginalPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", it.OriginalPath)
		}
	}

	var moved [][2]string // (from, to) for rollback
	// rollback undoes every restore completed so far in this call, so a
	// failure partway through never leaves an item restored-but-still-listed
	// in the manifest (which would make a retry refuse as "already exists").
	rollback := func() {
		for _, mv := range moved {
			_ = os.Rename(mv[1], mv[0])
		}
	}
	for _, it := range m.Items {
		src := filepath.Join(backupDir, filepath.FromSlash(it.Name))
		// The original parent may have been tidied away since the move.
		if err := os.MkdirAll(filepath.Dir(it.OriginalPath), 0o700); err != nil {
			rollback()
			return fmt.Errorf("restoring %s: %w", it.Name, err)
		}
		if err := os.Rename(src, it.OriginalPath); err != nil {
			rollback()
			return fmt.Errorf("restoring %s: %w", it.Name, err)
		}
		moved = append(moved, [2]string{src, it.OriginalPath})
	}

	// Tidy the emptied backup: manifest, then now-empty dirs bottom-up.
	// os.Remove refuses non-empty dirs, so an unexpectedly present file makes
	// this a no-op rather than a delete — deliberately NOT os.RemoveAll.
	_ = os.Remove(filepath.Join(backupDir, manifestName))
	removeEmptyDirs(backupDir)
	return nil
}

// removeEmptyDirs removes dir and any now-empty subdirectories, deepest first.
func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			removeEmptyDirs(filepath.Join(dir, e.Name()))
		}
	}
	_ = os.Remove(dir) // fails (harmlessly) if anything remains
}
