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

// Restore moves every file recorded in backupDir's manifest back to its original
// path, then removes the now-empty backup directory.
//
// SAFETY: moves via os.Rename only; refuses any recorded path that is not a known
// Codex log; refuses to overwrite a file that already exists at the destination
// (so a fresh live log is never clobbered). On partial failure it rolls back.
func Restore(backupDir string) error {
	m, err := readManifest(backupDir)
	if err != nil {
		return err
	}
	// Pre-flight: validate every item before moving anything.
	for _, it := range m.Items {
		if !isCodexLog(it.OriginalPath) {
			return fmt.Errorf("refusing to restore non-Codex file: %s", it.OriginalPath)
		}
		if _, err := os.Stat(it.OriginalPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", it.OriginalPath)
		}
	}

	var moved [][2]string // (from, to) for rollback
	for _, it := range m.Items {
		src := filepath.Join(backupDir, it.Name)
		if err := os.Rename(src, it.OriginalPath); err != nil {
			for _, mv := range moved {
				_ = os.Rename(mv[1], mv[0])
			}
			return fmt.Errorf("restoring %s: %w", it.Name, err)
		}
		moved = append(moved, [2]string{src, it.OriginalPath})
	}

	// Clean up the now-empty backup directory (manifest + dir only; never a log).
	_ = os.Remove(filepath.Join(backupDir, manifestName))
	_ = os.Remove(backupDir)
	return nil
}
