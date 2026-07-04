package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("missing file must not error, got %v", err)
	}
	if cfg != Default() {
		t.Errorf("got %+v, want defaults", cfg)
	}
}

func TestLoadPartialFileOverlaysDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"high_mb_per_min": 50, "notifications": false}`), 0o600)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HighMBPerMin != 50 {
		t.Errorf("HighMBPerMin = %v, want 50", cfg.HighMBPerMin)
	}
	if cfg.Notifications {
		t.Error("Notifications should be false when explicitly set")
	}
	if cfg.CriticalMBPerMin != Default().CriticalMBPerMin {
		t.Error("unset fields must keep defaults")
	}
}

func TestLoadMalformedReturnsDefaultsAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{not json`), 0o600)
	cfg, err := Load(path)
	if err == nil {
		t.Fatal("want an error for malformed config")
	}
	if cfg != Default() {
		t.Error("malformed config must fall back to full defaults, not a partial parse")
	}
}

func TestDurationHelpers(t *testing.T) {
	cfg := Default()
	if cfg.PollInterval() != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", cfg.PollInterval())
	}
	if cfg.BinHold() != 14*24*time.Hour {
		t.Errorf("BinHold = %v, want 336h", cfg.BinHold())
	}
	if cfg.StaleAfter() != 30*24*time.Hour {
		t.Errorf("StaleAfter = %v, want 720h", cfg.StaleAfter())
	}
	th := cfg.MonitorThresholds()
	if th.HighMemMB != 2048 || th.MediumMBPerMin != 25 {
		t.Errorf("MonitorThresholds mismatch: %+v", th)
	}
}

func TestPollIntervalClampsBelowMinimum(t *testing.T) {
	cfg := Default()
	cfg.PollIntervalSeconds = 0
	if got := cfg.PollInterval(); got != 5*time.Second {
		t.Errorf("PollInterval = %v, want clamped 5s", got)
	}
}

func TestBinHoldClampsBelowMinimum(t *testing.T) {
	cfg := Default()
	cfg.BinHoldDays = 0
	if got := cfg.BinHold(); got != 24*time.Hour {
		t.Errorf("BinHold = %v, want clamped 24h", got)
	}
}
