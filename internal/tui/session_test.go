package tui

import (
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
)

// loadedAt builds a loadedMsg for a point in time with a given total log size,
// so a sequence of them drives the session's rate/risk/growth tracking.
func loadedAt(at time.Time, totalBytes int64) loadedMsg {
	return loadedMsg{
		at: at,
		report: codex.LogReport{
			DirExists:  true,
			Files:      []codex.LogFile{{Name: "logs_2.sqlite", Exists: true, Size: totalBytes}},
			TotalBytes: totalBytes,
		},
		running:   false,
		supported: true,
	}
}

func TestSessionReceiptTracksDurationGrowthAndPeaks(t *testing.T) {
	start := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	m := New(config.Default())

	// First sample sets the session start; second, one minute later and 200 MiB
	// larger, drives a ~200 MB/min rate → HIGH risk.
	m, _ = step(m, loadedAt(start, 100*1024*1024))
	m, _ = step(m, loadedAt(start.Add(time.Minute), 300*1024*1024))

	rec := m.sessionReceipt(start.Add(2 * time.Minute))

	if rec.Action != "session" {
		t.Errorf("Action = %q, want %q", rec.Action, "session")
	}
	if rec.DurationSec != 120 {
		t.Errorf("DurationSec = %v, want 120", rec.DurationSec)
	}
	if want := int64(200 * 1024 * 1024); rec.DiskWritten != want {
		t.Errorf("DiskWritten = %d, want %d", rec.DiskWritten, want)
	}
	if rec.Risk != "HIGH" {
		t.Errorf("Risk = %q, want HIGH (peak)", rec.Risk)
	}
	if rec.PeakMBPerMin < 190 || rec.PeakMBPerMin > 210 {
		t.Errorf("PeakMBPerMin = %v, want ~200", rec.PeakMBPerMin)
	}
	if !rec.At.Equal(start.Add(2 * time.Minute)) {
		t.Errorf("At = %v, want the end time", rec.At)
	}
}

func TestSessionReceiptPeakSurvivesDeEscalation(t *testing.T) {
	start := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	m := New(config.Default())

	// Spike to HIGH, then a quiet sample. The peak risk must persist even though
	// current risk has dropped back to LOW.
	m, _ = step(m, loadedAt(start, 100*1024*1024))
	m, _ = step(m, loadedAt(start.Add(time.Minute), 300*1024*1024))    // HIGH
	m, _ = step(m, loadedAt(start.Add(10*time.Minute), 300*1024*1024)) // flat → LOW now

	rec := m.sessionReceipt(start.Add(11 * time.Minute))
	if rec.Risk != "HIGH" {
		t.Errorf("Risk = %q, want HIGH — the session peak must survive de-escalation", rec.Risk)
	}
}

func TestSessionReceiptEmptySessionIsSafe(t *testing.T) {
	// A dashboard opened and quit with no successful load: no start, no samples.
	m := New(config.Default())
	rec := m.sessionReceipt(time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC))
	if rec.Action != "session" {
		t.Errorf("Action = %q, want session", rec.Action)
	}
	if rec.DurationSec != 0 {
		t.Errorf("DurationSec = %v, want 0 for a session that never loaded", rec.DurationSec)
	}
	if rec.DiskWritten != 0 {
		t.Errorf("DiskWritten = %d, want 0", rec.DiskWritten)
	}
	if rec.Risk != "LOW" {
		t.Errorf("Risk = %q, want LOW", rec.Risk)
	}
}
