package monitor

import "fmt"

// Risk is the simple, plain-language risk level surfaced to the user.
type Risk int

const (
	RiskLow Risk = iota
	RiskMedium
	RiskHigh
	RiskCritical
)

// String renders the risk level in the words the user sees.
func (r Risk) String() string {
	switch r {
	case RiskLow:
		return "LOW"
	case RiskMedium:
		return "MEDIUM"
	case RiskHigh:
		return "HIGH"
	case RiskCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// Thresholds define where one risk level tips into the next. Defaults mirror the
// config documented in docs/stack.md.
type Thresholds struct {
	MediumMBPerMin    float64
	HighMBPerMin      float64
	CriticalMBPerMin  float64
	HighWALSizeMB     int64
	CriticalWALSizeMB int64
	HighMemMB         int64 // Codex RSS at/above this is HIGH (0 disables)
	CriticalMemMB     int64 // Codex RSS at/above this is CRITICAL (0 disables)
}

// DefaultThresholds returns the documented default risk thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MediumMBPerMin:    25,
		HighMBPerMin:      100,
		CriticalMBPerMin:  500,
		HighWALSizeMB:     1024,
		CriticalWALSizeMB: 8192,
		HighMemMB:         2048,
		CriticalMemMB:     6144,
	}
}

// Assessment is the monitor's read on current Codex log activity.
type Assessment struct {
	Level        Risk
	RateMBPerMin float64
	WALBytes     int64
	Reasons      []string
}

// Evaluate computes a risk Assessment from a window of samples. Rate is the
// total-log growth between the oldest and newest sample, in MB/min. WAL size and
// an idle-writer rule (growth while Codex is not running) can escalate the level.
// Pure: no I/O, no clock — everything comes from the samples.
func Evaluate(samples []Sample, codexRunning bool, t Thresholds) Assessment {
	a := Assessment{Level: RiskLow}
	if len(samples) == 0 {
		return a
	}
	newest := samples[len(samples)-1]
	a.WALBytes = newest.WALBytes

	if len(samples) >= 2 {
		oldest := samples[0]
		mins := newest.At.Sub(oldest.At).Minutes()
		if mins > 0 {
			delta := newest.TotalBytes - oldest.TotalBytes
			if delta < 0 {
				delta = 0
			}
			a.RateMBPerMin = float64(delta) / (1024 * 1024) / mins
		}
	}

	// Write-rate thresholds.
	switch {
	case a.RateMBPerMin >= t.CriticalMBPerMin:
		a.Level = RiskCritical
		a.Reasons = append(a.Reasons, fmt.Sprintf("writing %.0f MB/min", a.RateMBPerMin))
	case a.RateMBPerMin >= t.HighMBPerMin:
		a.Level = RiskHigh
		a.Reasons = append(a.Reasons, fmt.Sprintf("writing %.0f MB/min", a.RateMBPerMin))
	case a.RateMBPerMin >= t.MediumMBPerMin:
		a.Level = RiskMedium
		a.Reasons = append(a.Reasons, fmt.Sprintf("writing %.0f MB/min", a.RateMBPerMin))
	}

	// WAL size can escalate.
	walMB := newest.WALBytes / (1024 * 1024)
	if walMB >= t.CriticalWALSizeMB {
		a.Level = maxRisk(a.Level, RiskCritical)
		a.Reasons = append(a.Reasons, fmt.Sprintf("WAL file is %d MiB", walMB))
	} else if walMB >= t.HighWALSizeMB {
		a.Level = maxRisk(a.Level, RiskHigh)
		a.Reasons = append(a.Reasons, fmt.Sprintf("WAL file is %d MiB", walMB))
	}

	// Memory can escalate too: a Codex eating RAM is the same "quietly hurting
	// your machine" problem as a bloating WAL. Zero thresholds disable the check.
	memMB := newest.MemBytes / (1024 * 1024)
	if t.CriticalMemMB > 0 && memMB >= t.CriticalMemMB {
		a.Level = maxRisk(a.Level, RiskCritical)
		a.Reasons = append(a.Reasons, fmt.Sprintf("Codex is using %d MiB of memory", memMB))
	} else if t.HighMemMB > 0 && memMB >= t.HighMemMB {
		a.Level = maxRisk(a.Level, RiskHigh)
		a.Reasons = append(a.Reasons, fmt.Sprintf("Codex is using %d MiB of memory", memMB))
	}

	// An idle writer (logs growing while Codex isn't running) is extra alarming.
	if !codexRunning && a.RateMBPerMin >= t.MediumMBPerMin {
		a.Level = maxRisk(a.Level, RiskHigh)
		a.Reasons = append(a.Reasons, "growing while Codex is idle")
	}

	return a
}

func maxRisk(a, b Risk) Risk {
	if a > b {
		return a
	}
	return b
}
