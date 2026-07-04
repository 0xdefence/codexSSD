// Package config loads CodexSSD's optional user configuration. The contract:
// config can NEVER brick the tool. A missing file means defaults; a malformed
// file means defaults plus an error the caller should surface as a warning and
// then carry on.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/recorder"
)

// FileName is the config file inside CodexSSD's state dir (~/.codexssd).
const FileName = "config.json"

// Config is the user-editable configuration. All fields are optional in the
// file; absent fields keep their defaults (we unmarshal onto Default()).
type Config struct {
	MediumMBPerMin      float64 `json:"medium_mb_per_min"`
	HighMBPerMin        float64 `json:"high_mb_per_min"`
	CriticalMBPerMin    float64 `json:"critical_mb_per_min"`
	HighWALSizeMB       int64   `json:"high_wal_size_mb"`
	CriticalWALSizeMB   int64   `json:"critical_wal_size_mb"`
	HighMemMB           int64   `json:"high_mem_mb"`
	CriticalMemMB       int64   `json:"critical_mem_mb"`
	PollIntervalSeconds int     `json:"poll_interval_seconds"`
	BinHoldDays         int     `json:"bin_hold_days"`
	Notifications       bool    `json:"notifications"`
	StaleAfterDays      int     `json:"stale_after_days"`
}

// Default returns the documented defaults (mirrors monitor.DefaultThresholds).
func Default() Config {
	t := monitor.DefaultThresholds()
	return Config{
		MediumMBPerMin:      t.MediumMBPerMin,
		HighMBPerMin:        t.HighMBPerMin,
		CriticalMBPerMin:    t.CriticalMBPerMin,
		HighWALSizeMB:       t.HighWALSizeMB,
		CriticalWALSizeMB:   t.CriticalWALSizeMB,
		HighMemMB:           t.HighMemMB,
		CriticalMemMB:       t.CriticalMemMB,
		PollIntervalSeconds: 30,
		BinHoldDays:         cleaner.RetentionDays,
		Notifications:       true,
		StaleAfterDays:      30,
	}
}

// Load reads the config at path. Missing file → (Default(), nil). Malformed
// file → (Default(), error): the caller warns and proceeds with defaults —
// a broken config file must never make the tool unusable.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Default(), err
	}
	// Unmarshal onto the defaults: absent keys keep their default values,
	// including bools that default to true.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("config file %s is not valid JSON: %w", path, err)
	}
	return cfg, nil
}

// DefaultPath is <state-dir>/config.json — one home for everything CodexSSD owns.
func DefaultPath() (string, error) {
	dir, err := recorder.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// LoadDefault loads the config from its default path.
func LoadDefault() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Default(), err
	}
	return Load(path)
}

// MonitorThresholds maps the config onto the risk engine's thresholds.
func (c Config) MonitorThresholds() monitor.Thresholds {
	return monitor.Thresholds{
		MediumMBPerMin:    c.MediumMBPerMin,
		HighMBPerMin:      c.HighMBPerMin,
		CriticalMBPerMin:  c.CriticalMBPerMin,
		HighWALSizeMB:     c.HighWALSizeMB,
		CriticalWALSizeMB: c.CriticalWALSizeMB,
		HighMemMB:         c.HighMemMB,
		CriticalMemMB:     c.CriticalMemMB,
	}
}

// PollInterval is how often watchers re-check ~/.codex (minimum 5s).
func (c Config) PollInterval() time.Duration {
	if c.PollIntervalSeconds < 5 {
		return 5 * time.Second
	}
	return time.Duration(c.PollIntervalSeconds) * time.Second
}

// BinHold is how long moved-aside backups are held before release (minimum 1 day).
func (c Config) BinHold() time.Duration {
	if c.BinHoldDays < 1 {
		return 24 * time.Hour
	}
	return time.Duration(c.BinHoldDays) * 24 * time.Hour
}

// StaleAfter is the age past which the disk report flags an entry as stale.
func (c Config) StaleAfter() time.Duration {
	if c.StaleAfterDays < 1 {
		return 24 * time.Hour
	}
	return time.Duration(c.StaleAfterDays) * 24 * time.Hour
}
