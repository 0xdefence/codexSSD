package monitor

import "context"

// Watcher ties the Sampler and risk Evaluate together: it samples a running
// agent, updates the risk level, and emits plain-language warnings.
//
// Low-write cadence (see docs/stack.md): sleep minutes when no agent is running,
// seconds while one is active, and tighten further when risk is high.
type Watcher struct {
	thresholds Thresholds
}

// NewWatcher returns a Watcher using the default thresholds.
func NewWatcher() *Watcher {
	return &Watcher{thresholds: DefaultThresholds()}
}

// Run watches until ctx is cancelled.
//
// STUB: not implemented yet.
func (w *Watcher) Run(ctx context.Context) error {
	return errNotImplemented
}
