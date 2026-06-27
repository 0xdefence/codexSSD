// Package monitor turns Codex's log-growth over time into a plain-language risk
// level (LOW/MEDIUM/HIGH/CRITICAL). It is a pure engine: it does no I/O and
// keeps no state of its own — callers (the interactive app) supply samples and
// render the result. Samples live in memory only; nothing is written to disk.
package monitor

import "time"

// Sample is a point-in-time reading of Codex's log sizes.
type Sample struct {
	At         time.Time
	TotalBytes int64 // total size of Codex's known log files
	WALBytes   int64 // size of logs_2.sqlite-wal
}

// AppendSample adds s to history, keeping at most max most-recent samples.
func AppendSample(history []Sample, s Sample, max int) []Sample {
	history = append(history, s)
	if max > 0 && len(history) > max {
		history = history[len(history)-max:]
	}
	return history
}
