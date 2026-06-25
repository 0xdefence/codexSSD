// Package self reports CodexSSD's OWN footprint (disk, memory, processor) so the
// tool holds itself to the same standard it holds the agents it watches — and
// can prove it isn't the thing causing the problem.
package self

import "errors"

// errNotImplemented marks scaffolding that has a defined shape but no logic yet.
var errNotImplemented = errors.New("not implemented")

// Report is CodexSSD's own footprint for a session.
type Report struct {
	OwnWriteBytes int64  `json:"own_write_bytes"`
	Mode          string `json:"mode"` // e.g. "low-write"
	HistoryBytes  int64  `json:"history_bytes"`
}

// Measure gathers CodexSSD's own footprint.
//
// STUB: not implemented yet.
func Measure() (Report, error) {
	return Report{}, errNotImplemented
}
