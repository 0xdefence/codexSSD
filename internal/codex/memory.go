package codex

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// execRSS asks ps for the RSS (in KiB) of the given pids. A package-level seam
// so tests can substitute canned output without a real process table.
var execRSS = func(pids []string) ([]byte, error) {
	return exec.Command("ps", "-o", "rss=", "-p", strings.Join(pids, ",")).Output()
}

// ProcessMemory returns the total resident memory, in BYTES, of running
// Codex-like processes. When no Codex process is running it returns (0, nil) —
// absence of Codex is not an error. Windows returns ErrUnsupportedPlatform;
// callers must omit memory rather than fail.
//
// SAFETY: observation only — it never signals or alters a process.
func ProcessMemory() (int64, error) {
	if runtime.GOOS == "windows" {
		return 0, ErrUnsupportedPlatform
	}
	procs, err := DetectProcesses()
	if err != nil {
		return 0, err
	}
	if len(procs) == 0 {
		return 0, nil
	}
	pids := make([]string, 0, len(procs))
	for _, p := range procs {
		pids = append(pids, strconv.Itoa(p.PID))
	}
	out, err := execRSS(pids)
	if err != nil {
		return 0, err
	}
	// ps reports RSS in KiB on both darwin and linux.
	return ParseRSSKiB(string(out)) * 1024, nil
}

// ParseRSSKiB sums the per-line RSS values (KiB) in `ps -o rss=` output.
// Non-numeric lines are skipped rather than treated as errors.
func ParseRSSKiB(psOut string) int64 {
	var total int64
	for _, line := range strings.Split(psOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kib, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		total += kib
	}
	return total
}
