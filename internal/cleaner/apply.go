package cleaner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RetentionDays is how long a moved-aside backup is held before it becomes
// eligible for release (auto-release itself is a later phase).
const RetentionDays = 14

const (
	manifestName    = "manifest.json"
	timestampLayout = "20060102-150405"
)

// ManifestItem records one moved file and where it came from.
type ManifestItem struct {
	Name         string `json:"name"`
	OriginalPath string `json:"original_path"`
	Size         int64  `json:"size_bytes"`
}

// Manifest is the receipt written into each backup directory. It makes the move
// recoverable and lets a later phase release the backup after HoldUntil.
type Manifest struct {
	Tool      string         `json:"tool,omitempty"` // "" means codex (legacy)
	MovedAt   time.Time      `json:"moved_at"`
	HoldUntil time.Time      `json:"hold_until"`
	Items     []ManifestItem `json:"items"`
}

// Apply moves the planned files aside with the default 14-day hold.
func (p Plan) Apply(now time.Time) (string, error) {
	return p.ApplyWithHold(now, RetentionDays*24*time.Hour)
}

// ApplyWithHold moves the planned files into a new timestamped backup directory
// and writes a manifest whose HoldUntil is now+hold. It returns the backup
// directory path.
//
// SAFETY: MOVES via os.Rename only — it never deletes a file. Every item is
// re-checked against the owning profile's allow-list before being moved. On any
// move (or MkdirAll) failure, files already moved this call are moved back
// (rollback) so a torn database is never left behind. `now` is injected so the
// directory name is deterministic/testable.
func (p Plan) ApplyWithHold(now time.Time, hold time.Duration) (string, error) {
	if p.Empty() {
		return "", errors.New("nothing to move aside")
	}
	prof, err := profileFor(p.Tool)
	if err != nil {
		return "", err
	}
	toolDir := filepath.Dir(p.BackupRoot)
	for _, it := range p.Items {
		if !prof.Allows(toolDir, it.Path) {
			return "", fmt.Errorf("refusing to move file outside %s's own-file allow-list: %s", prof.DisplayName, it.Path)
		}
	}

	dest := filepath.Join(p.BackupRoot, now.Format(timestampLayout))
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", err
	}

	manifest := Manifest{
		Tool:      p.Tool,
		MovedAt:   now,
		HoldUntil: now.Add(hold),
	}
	// moved tracks (from, to) pairs for rollback on failure.
	var moved [][2]string
	rollback := func() {
		for _, mv := range moved {
			_ = os.Rename(mv[1], mv[0]) // best-effort move back
		}
		// MkdirAll may have created nested subdirs under dest (e.g. for a
		// Claude transcript's projects/<slug>/ parent) before a later item
		// failed; a bare os.Remove(dest) only clears dest itself and silently
		// no-ops when it's non-empty, orphaning those subdirs with no
		// manifest. removeEmptyDirs walks bottom-up so every now-empty
		// subdir it created is cleared too — still os.Remove only, never
		// os.RemoveAll, so anything unexpectedly non-empty survives.
		removeEmptyDirs(dest)
	}

	for _, it := range p.Items {
		target := filepath.Join(dest, filepath.FromSlash(it.Name))
		// Nested own-files (e.g. Claude transcripts) keep their relative
		// structure inside the backup so restore is a pure mirror image.
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			rollback()
			return "", err
		}
		if err := os.Rename(it.Path, target); err != nil {
			rollback()
			return "", fmt.Errorf("moving %s: %w", it.Name, err)
		}
		moved = append(moved, [2]string{it.Path, target})
		manifest.Items = append(manifest.Items, ManifestItem{
			Name:         it.Name,
			OriginalPath: it.Path,
			Size:         it.Size,
		})
	}

	if err := writeManifest(dest, manifest); err != nil {
		rollback()
		return "", err
	}
	return dest, nil
}

func writeManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), data, 0o600)
}

func readManifest(dir string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}
