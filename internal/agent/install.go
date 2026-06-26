// Package agent installs "please behave" rules (an AGENTS.md file) for AI coding
// agents, to reduce avoidable disk and token churn at the source.
package agent

import "errors"

// errNotImplemented marks scaffolding that has a defined shape but no logic yet.
var errNotImplemented = errors.New("not implemented")

// FileName is the file written into the target repository.
const FileName = "AGENTS.md"

// Install writes an AGENTS.md for the given profile into dir.
//
// SAFETY: writes a single new file into the user-chosen repo; it does not modify
// or delete existing project files (a later refinement may refuse to clobber an
// existing AGENTS.md without confirmation).
//
// STUB: not implemented yet.
func Install(dir string, p Profile) error {
	return errNotImplemented
}
