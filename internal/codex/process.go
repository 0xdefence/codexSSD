package codex

import "github.com/0xdefence/codexssd/internal/tool"

// ErrUnsupportedPlatform mirrors tool.ErrUnsupportedPlatform (same sentinel, so
// errors.Is works across both packages).
var ErrUnsupportedPlatform = tool.ErrUnsupportedPlatform

// Process is a read-only snapshot of a running process.
type Process = tool.Process

// DetectProcesses returns running processes that look like Codex.
func DetectProcesses() ([]Process, error) { return tool.DetectProcesses(tool.Codex()) }

// IsCodexRunning reports whether any Codex-like process is currently running.
func IsCodexRunning() (bool, error) { return tool.IsRunning(tool.Codex()) }
