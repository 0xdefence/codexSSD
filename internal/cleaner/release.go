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

// Release moves a single backup directory into the OS Trash and reports the
// destination it landed at.
//
// SAFETY: a move, never a delete; refuses any path not directly under a
// codexssd-backups/ directory.
func Release(backupDir string) (dest string, err error) {
	if !isBackupDir(backupDir) {
		return "", fmt.Errorf("refusing to release non-backup path: %s", backupDir)
	}
	return moveToTrash(backupDir)
}

// ReleaseExpired moves every expired backup under codexDir into the Trash and
// returns the released backup ids, plus trashDir — the directory of the last
// successful release's destination (empty if nothing was released). If the
// platform's Trash is unsupported, the first Release returns
// trash.ErrUnsupported and nothing is hard-deleted.
func ReleaseExpired(codexDir string, now time.Time) (released []string, trashDir string, err error) {
	backups, err := ListBackups(codexDir)
	if err != nil {
		return nil, "", err
	}
	for _, b := range Expired(backups, now) {
		dest, err := Release(b.Dir)
		if err != nil {
			return released, trashDir, err
		}
		released = append(released, filepath.Base(b.Dir))
		trashDir = filepath.Dir(dest)
	}
	return released, trashDir, nil
}

// isBackupDir reports whether dir sits directly inside a codexssd-backups/ dir.
func isBackupDir(dir string) bool {
	return filepath.Base(filepath.Dir(dir)) == BackupDirName
}
