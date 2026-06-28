package cleaner

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/trash"
)

// moveToTrash is a seam so tests can stub trashing without touching the real
// OS Trash. It defaults to the real implementation.
var moveToTrash = trash.Move

// Expired returns the backups whose hold has elapsed (now at or after HoldUntil).
func Expired(backups []Backup, now time.Time) []Backup {
	var out []Backup
	for _, b := range backups {
		if !now.Before(b.Manifest.HoldUntil) { // now >= HoldUntil
			out = append(out, b)
		}
	}
	return out
}

// Release moves a single backup directory into the OS Trash.
//
// SAFETY: a move, never a delete; refuses any path not directly under a
// codexssd-backups/ directory.
func Release(backupDir string) error {
	if !isBackupDir(backupDir) {
		return fmt.Errorf("refusing to release non-backup path: %s", backupDir)
	}
	return moveToTrash(backupDir)
}

// ReleaseExpired moves every expired backup under codexDir into the Trash and
// returns the released backup ids. If the platform's Trash is unsupported, the
// first Release returns trash.ErrUnsupported and nothing is hard-deleted.
func ReleaseExpired(codexDir string, now time.Time) ([]string, error) {
	backups, err := ListBackups(codexDir)
	if err != nil {
		return nil, err
	}
	var released []string
	for _, b := range Expired(backups, now) {
		if err := Release(b.Dir); err != nil {
			return released, err
		}
		released = append(released, filepath.Base(b.Dir))
	}
	return released, nil
}

// isBackupDir reports whether dir sits directly inside a codexssd-backups/ dir.
func isBackupDir(dir string) bool {
	return filepath.Base(filepath.Dir(dir)) == BackupDirName
}
