package tool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrUnsupportedPlatform: process detection unavailable (Windows). Callers must
// treat this as "cannot verify the tool is stopped" and refuse to act.
var ErrUnsupportedPlatform = errors.New("process detection not supported on this platform")

// Process is a read-only snapshot of a running process.
type Process struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// DetectProcesses returns running processes that look like the profiled tool.
// SAFETY: observation only — never signals or alters a process; excludes self.
func DetectProcesses(p Profile) ([]Process, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrUnsupportedPlatform
	}
	out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var matched []Process
	for _, proc := range parseProcesses(string(out)) {
		if proc.PID == self {
			continue
		}
		if matchesProfile(p, proc) {
			matched = append(matched, proc)
		}
	}
	return matched, nil
}

// IsRunning reports whether any process matching the profile is running.
func IsRunning(p Profile) (bool, error) {
	procs, err := DetectProcesses(p)
	if err != nil {
		return false, err
	}
	return len(procs) > 0, nil
}

// parseProcesses turns `ps -axo pid=,args=` output into Processes.
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

// matchesProfile: base-name match, hint substring match, or a node runner whose
// command line contains the tool name (Codex and Claude Code both ship as node
// apps). Never matches codexssd itself.
func matchesProfile(p Profile, proc Process) bool {
	fields := strings.Fields(proc.Command)
	if len(fields) == 0 {
		return false
	}
	base := filepath.Base(fields[0])
	if base == "codexssd" {
		return false
	}
	for _, n := range p.ProcessNames {
		if base == n {
			return true
		}
	}
	for _, h := range p.ProcessHints {
		if strings.Contains(proc.Command, h) {
			return true
		}
	}
	return base == "node" && strings.Contains(proc.Command, p.Name)
}
