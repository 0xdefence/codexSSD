// Package monitor watches what an AI coding agent is doing to disk and memory
// and turns raw activity into a simple risk level.
//
// Low-write by design: it polls process/file counters, keeps samples in memory,
// never continuously parses logs, and never uses a database for its own state.
package monitor

import (
	"errors"
	"time"
)

// errNotImplemented marks scaffolding that has a defined shape but no logic yet.
var errNotImplemented = errors.New("not implemented")

// Sample is a single in-memory observation of agent activity.
//
// DESIGN: samples are held in RAM and summarized once at session end — they are
// never streamed to disk (that would make the watchdog the thing hammering the
// disk it is meant to catch).
type Sample struct {
	At            time.Time
	WriteBytes    int64 // cumulative process write bytes
	WALSizeBytes  int64 // size of logs_2.sqlite-wal
	DiskFreeBytes int64
}

// Sampler takes periodic readings of a running agent's activity.
type Sampler struct {
	// unexported fields (process handle, last counters) added when implemented.
}

// NewSampler returns a ready-to-use Sampler.
func NewSampler() *Sampler {
	return &Sampler{}
}

// Sample takes one reading.
//
// STUB: not implemented yet.
func (s *Sampler) Sample() (Sample, error) {
	return Sample{}, errNotImplemented
}
