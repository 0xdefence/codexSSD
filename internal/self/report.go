// Package self reports CodexSSD's OWN footprint so the tool holds itself to the
// same standard it holds the agents it watches — and can show it isn't the
// thing causing the problem.
package self

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Report is CodexSSD's own footprint.
type Report struct {
	Mode         string `json:"mode"`
	StateDir     string `json:"state_dir"`
	HistoryBytes int64  `json:"history_bytes"`
}

// Measure reports CodexSSD's own footprint: the total size of its state
// directory (its only persistent storage). A missing dir reports 0, not an error.
func Measure(stateDir string) (Report, error) {
	r := Report{Mode: "low-write", StateDir: stateDir}
	size, err := dirSize(stateDir)
	if err != nil {
		return r, err
	}
	r.HistoryBytes = size
	return r, nil
}

// dirSize sums the sizes of all regular files under dir (0 if dir is absent).
func dirSize(dir string) (int64, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
