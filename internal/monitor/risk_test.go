package monitor

import (
	"strings"
	"testing"
	"time"
)

func mib(n int64) int64 { return n * 1024 * 1024 }

// two samples one minute apart with the given total-byte delta
func window(deltaBytes int64, wal int64) []Sample {
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	return []Sample{
		{At: base, TotalBytes: 0, WALBytes: wal},
		{At: base.Add(time.Minute), TotalBytes: deltaBytes, WALBytes: wal},
	}
}

func TestEvaluateRateLevels(t *testing.T) {
	th := DefaultThresholds()
	cases := []struct {
		name     string
		mbPerMin int64
		want     Risk
	}{
		{"calm", 5, RiskLow},
		{"medium", 30, RiskMedium},
		{"high", 150, RiskHigh},
		{"critical", 600, RiskCritical},
	}
	for _, c := range cases {
		a := Evaluate(window(mib(c.mbPerMin), 0), true, th)
		if a.Level != c.want {
			t.Errorf("%s: level = %v, want %v (rate %.0f)", c.name, a.Level, c.want, a.RateMBPerMin)
		}
	}
}

func TestEvaluateWALSizeEscalates(t *testing.T) {
	th := DefaultThresholds()
	// Low rate, but a huge WAL should still escalate to CRITICAL.
	a := Evaluate(window(mib(1), mib(9000)), true, th)
	if a.Level != RiskCritical {
		t.Errorf("level = %v, want RiskCritical for 9000 MiB WAL", a.Level)
	}
	if a.WALBytes != mib(9000) {
		t.Errorf("WALBytes = %d, want %d", a.WALBytes, mib(9000))
	}
}

func TestEvaluateIdleWriterEscalates(t *testing.T) {
	th := DefaultThresholds()
	// Medium rate while Codex is NOT running is more alarming → at least HIGH.
	a := Evaluate(window(mib(30), 0), false, th)
	if a.Level < RiskHigh {
		t.Errorf("level = %v, want >= RiskHigh (idle writer)", a.Level)
	}
	found := false
	for _, r := range a.Reasons {
		if strings.Contains(r, "idle") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an idle-writer reason, got %v", a.Reasons)
	}
}

func TestEvaluateEmptyAndSingle(t *testing.T) {
	th := DefaultThresholds()
	if a := Evaluate(nil, true, th); a.Level != RiskLow || a.RateMBPerMin != 0 {
		t.Errorf("empty: %+v, want calm/0", a)
	}
	one := []Sample{{TotalBytes: mib(500), WALBytes: 0}}
	if a := Evaluate(one, true, th); a.RateMBPerMin != 0 {
		t.Errorf("single sample should have 0 rate, got %.1f", a.RateMBPerMin)
	}
}

func TestEvaluateRateBoundaries(t *testing.T) {
	// Test exact boundary rates to catch mutations in >= thresholds.
	th := DefaultThresholds()
	cases := []struct {
		mbPerMin int64
		want     Risk
	}{
		{24, RiskLow},       // just below MediumMBPerMin (25)
		{25, RiskMedium},    // exactly MediumMBPerMin
		{99, RiskMedium},    // just below HighMBPerMin (100)
		{100, RiskHigh},     // exactly HighMBPerMin
		{499, RiskHigh},     // just below CriticalMBPerMin (500)
		{500, RiskCritical}, // exactly CriticalMBPerMin
	}
	for _, c := range cases {
		a := Evaluate(window(mib(c.mbPerMin), 0), true, th)
		if a.Level != c.want {
			t.Errorf("rate %d MB/min: level = %v, want %v", c.mbPerMin, a.Level, c.want)
		}
	}
}

func TestEvaluateWALBoundaries(t *testing.T) {
	// Test exact WAL size boundaries to catch mutations in >= thresholds.
	th := DefaultThresholds()
	cases := []struct {
		walMiB int64
		want   Risk
	}{
		{1023, RiskLow},      // just below HighWALSizeMB (1024)
		{1024, RiskHigh},     // exactly HighWALSizeMB
		{8191, RiskHigh},     // just below CriticalWALSizeMB (8192)
		{8192, RiskCritical}, // exactly CriticalWALSizeMB
	}
	for _, c := range cases {
		a := Evaluate(window(mib(1), mib(c.walMiB)), true, th)
		if a.Level != c.want {
			t.Errorf("WAL %d MiB: level = %v, want %v", c.walMiB, a.Level, c.want)
		}
	}
}

func TestEvaluateNoIdleEscalationWhenRunning(t *testing.T) {
	// Verify that idle-writer escalation does NOT happen when codexRunning=true.
	th := DefaultThresholds()
	a := Evaluate(window(mib(30), 0), true, th)
	if a.Level != RiskMedium {
		t.Errorf("level = %v, want RiskMedium (no escalation when running)", a.Level)
	}
	for _, r := range a.Reasons {
		if strings.Contains(r, "idle") {
			t.Errorf("unexpected idle reason when Codex is running: %q", r)
		}
	}
}

func TestEvaluateMemoryEscalation(t *testing.T) {
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	th := DefaultThresholds()

	t.Run("high memory escalates to HIGH", func(t *testing.T) {
		s := []Sample{{At: base, TotalBytes: 0, MemBytes: 3 * 1024 * 1024 * 1024}} // 3 GiB
		a := Evaluate(s, true, th)
		if a.Level != RiskHigh {
			t.Errorf("Level = %v, want HIGH", a.Level)
		}
	})

	t.Run("critical memory escalates to CRITICAL", func(t *testing.T) {
		s := []Sample{{At: base, MemBytes: 7 * 1024 * 1024 * 1024}} // 7 GiB
		a := Evaluate(s, true, th)
		if a.Level != RiskCritical {
			t.Errorf("Level = %v, want CRITICAL", a.Level)
		}
	})

	t.Run("modest memory stays LOW", func(t *testing.T) {
		s := []Sample{{At: base, MemBytes: 512 * 1024 * 1024}}
		if a := Evaluate(s, true, th); a.Level != RiskLow {
			t.Errorf("Level = %v, want LOW", a.Level)
		}
	})

	t.Run("zero mem thresholds disable the check", func(t *testing.T) {
		off := th
		off.HighMemMB, off.CriticalMemMB = 0, 0
		s := []Sample{{At: base, MemBytes: 64 * 1024 * 1024 * 1024}}
		if a := Evaluate(s, true, off); a.Level != RiskLow {
			t.Errorf("Level = %v, want LOW when disabled", a.Level)
		}
	})
}
