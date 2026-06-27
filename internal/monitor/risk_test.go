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
