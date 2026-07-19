// Package shallowmap implements Phase 3's deliberately shallow connection
// probe: "does anything obvious point at this entry?".
//
// GOLDEN RULE (from the roadmap): finding a connection is trustworthy — the
// entry is in use, hands off, extra caution. Finding NOTHING is not proof of
// safety; an Unknown entry may only ever be REPORTED, never acted on. This
// package therefore has no verdict meaning "safe to remove" — by design there
// is nowhere for such a verdict to exist.
package shallowmap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdefence/codexssd/internal/visibility"
)

// Connection is the probe's two-value verdict. There is deliberately no third
// "safe" value.
type Connection string

const (
	Connected Connection = "connected" // evidence found: hands off
	Unknown   Connection = "unknown"   // nothing found: still report-only
)

// Result is one probe outcome. Evidence is plain language and only ever set
// when Connected — "we found nothing" is not a finding.
type Result struct {
	Connection Connection `json:"connection"`
	Evidence   string     `json:"evidence,omitempty"`
}

// DecodePath turns a Claude Code projects dir name into its best-guess source
// path: "-Users-jo-code-app" → "/Users/jo/code/app". The encoding is lossy
// (dashes inside real folder names are indistinguishable from separators), so
// the result is only ever a PROBE input, never an action target. Names without
// the leading dash aren't the encoding we know; refuse to guess.
func DecodePath(entryName string) string {
	if !strings.HasPrefix(entryName, "-") {
		return ""
	}
	return filepath.FromSlash(strings.ReplaceAll(entryName, "-", "/"))
}

// ProbeClaudeProject checks whether the project a transcripts folder belongs to
// still exists on disk. statFn is injected for tests (production: os.Stat).
func ProbeClaudeProject(entryName string, statFn func(string) (os.FileInfo, error)) Result {
	decoded := DecodePath(entryName)
	if decoded == "" {
		return Result{Connection: Unknown}
	}
	if _, err := statFn(decoded); err == nil {
		return Result{
			Connection: Connected,
			Evidence:   fmt.Sprintf("its project folder still exists on disk (%s)", decoded),
		}
	}
	return Result{Connection: Unknown}
}

// ProjectEntry is one per-project row for the connections section of `report`.
type ProjectEntry struct {
	visibility.Entry
	DecodedPath string     `json:"decoded_path,omitempty"`
	Connection  Connection `json:"connection"`
	Evidence    string     `json:"evidence,omitempty"`
}

// ScanClaudeProjects sizes each project's transcript folder (read-only, via
// visibility.Scan on the projects dir) and probes its connection.
func ScanClaudeProjects(claudeDir string, now time.Time, staleAfter time.Duration) []ProjectEntry {
	report := visibility.Scan(filepath.Join(claudeDir, "projects"), now, staleAfter)
	var out []ProjectEntry
	for _, e := range report.Entries {
		probe := ProbeClaudeProject(e.Name, os.Stat)
		out = append(out, ProjectEntry{
			Entry:       e,
			DecodedPath: DecodePath(e.Name),
			Connection:  probe.Connection,
			Evidence:    probe.Evidence,
		})
	}
	return out
}
