package codex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrUnsupportedPlatform is returned when process detection is not available on
// the current OS (currently: Windows). Callers must treat this as "cannot
// verify Codex is stopped" and refuse to act, rather than assuming it is safe.
var ErrUnsupportedPlatform = errors.New("process detection not supported on this platform")

// Process is a read-only snapshot of a running process.
type Process struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`    // executable base name
	Command string `json:"command"` // full command line
}

// codexExactNames are executable base names that identify Codex itself.
var codexExactNames = []string{"codex"}

// codexCommandHints are substrings within a full command line that identify a
// Codex sub-process.
var codexCommandHints = []string{"codex app-server", "codex desktop"}

// DetectProcesses returns running processes that look like Codex.
//
// SAFETY: observation only — it never signals or alters a process. It also
// excludes codexssd's own process.
func DetectProcesses() ([]Process, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrUnsupportedPlatform
	}
	out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var matched []Process
	for _, p := range parseProcesses(string(out)) {
		if p.PID == self {
			continue
		}
		if matchesCodex(p) {
			matched = append(matched, p)
		}
	}
	return matched, nil
}

// IsCodexRunning reports whether any Codex-like process is currently running.
func IsCodexRunning() (bool, error) {
	procs, err := DetectProcesses()
	if err != nil {
		return false, err
	}
	return len(procs) > 0, nil
}

// parseProcesses turns `ps -axo pid=,args=` output into Processes. Each line is
// "<pid> <full command line>". Lines without a numeric leading PID are skipped.
func parseProcesses(psOutput string) []Process {
	var procs []Process
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		name := filepath.Base(strings.Fields(command)[0])
		procs = append(procs, Process{PID: pid, Name: name, Command: command})
	}
	return procs
}

// matchesCodex reports whether a process looks like Codex (and is not codexssd).
func matchesCodex(p Process) bool {
	fields := strings.Fields(p.Command)
	if len(fields) == 0 {
		return false
	}
	base := filepath.Base(fields[0])
	if base == "codexssd" {
		return false
	}
	for _, n := range codexExactNames {
		if base == n {
			return true
		}
	}
	for _, h := range codexCommandHints {
		if strings.Contains(p.Command, h) {
			return true
		}
	}
	if base == "node" && strings.Contains(p.Command, "codex") {
		return true
	}
	return false
}
