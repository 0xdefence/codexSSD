package monitor

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
}

// DefaultThresholds returns the documented default risk thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MediumMBPerMin:    25,
		HighMBPerMin:      100,
		CriticalMBPerMin:  500,
		HighWALSizeMB:     1024,
		CriticalWALSizeMB: 8192,
	}
}

// Evaluate computes a Risk from a window of samples against thresholds.
//
// STUB: not implemented yet — always returns RiskLow until the risk engine
// lands.
func Evaluate(samples []Sample, t Thresholds) Risk {
	return RiskLow
}
