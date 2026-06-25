package codex

import "errors"

// errNotImplemented marks scaffolding that has a defined shape but no logic yet.
var errNotImplemented = errors.New("not implemented")

// ProcessNameHints are substrings that identify a Codex-like process. A later
// phase uses these to find the process so the monitor can read its write
// counters rather than parsing logs.
var ProcessNameHints = []string{
	"codex",
	"codex app-server",
	"codex desktop",
}

// Process is a read-only snapshot of a running Codex-like process.
type Process struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// DetectProcesses returns the currently running processes that look like Codex
// (matched against ProcessNameHints).
//
// SAFETY: detection is observation only — it never signals or alters a process.
//
// STUB: not implemented yet.
func DetectProcesses() ([]Process, error) {
	return nil, errNotImplemented
}
