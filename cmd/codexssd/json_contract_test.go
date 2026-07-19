// json_contract_test.go pins the exact JSON shapes several commands emit, so
// a future change to shared plumbing (e.g. cleaner.Plan, the tool profile
// registry) can't silently leak new fields into default (codex) output again.
// See internal/cleaner/clean_test.go for the equivalent pin at the cleaner
// package level (TestPlanCodexLogsJSONOmitsToolField).
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to it, plus fn's own return value.
func captureStdout(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), code
}

// TestCleanJSONDefaultOmitsToolField pins the default (codex) `clean --json`
// dry-run shape: it must carry the legacy keys clean --json has always had,
// and must NOT contain a "tool" key anywhere — that field only exists on
// non-default (--tool claude) plans. This is the CLI-level companion to
// TestPlanCodexLogsJSONOmitsToolField in internal/cleaner.
func TestCleanJSONDefaultOmitsToolField(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".codex", "logs_2.sqlite"), time.Hour)

	out, code := captureStdout(t, func() int { return run([]string{"clean", "--json"}) })
	if code != 0 {
		t.Fatalf("clean --json exit = %d, want 0; output:\n%s", code, out)
	}
	if strings.Contains(out, `"tool"`) {
		t.Errorf("default clean --json leaked a \"tool\" key:\n%s", out)
	}
	for _, key := range []string{`"codex_dir"`, `"items"`, `"codex_running"`, `"platform_supported"`} {
		if !strings.Contains(out, key) {
			t.Errorf("default clean --json missing legacy key %s:\n%s", key, out)
		}
	}
}

// TestCleanJSONToolClaudeKeepsToolField is the mirror: `--tool claude` must
// carry "tool":"claude" in the plan.
func TestCleanJSONToolClaudeKeepsToolField(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl"), 40*24*time.Hour)

	out, code := captureStdout(t, func() int { return run([]string{"clean", "--json", "--tool", "claude"}) })
	if code != 0 {
		t.Fatalf("clean --json --tool claude exit = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"tool": "claude"`) && !strings.Contains(out, `"tool":"claude"`) {
		t.Errorf("clean --json --tool claude missing tool=claude:\n%s", out)
	}
}

// TestReportJSONDefaultOmitsConnectionsAndProvenance pins the default (no
// --connections, no seeded provenance) `report --json` shape: both fields are
// additive (omitempty) and must not appear at all when unused, keeping output
// byte-identical to before either feature existed.
func TestReportJSONDefaultOmitsConnectionsAndProvenance(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".codex", "cache-v2", "f.bin"), time.Hour)

	out, code := captureStdout(t, func() int { return run([]string{"report", "--json"}) })
	if code != 0 {
		t.Fatalf("report --json exit = %d, want 0; output:\n%s", code, out)
	}
	if strings.Contains(out, `"connections"`) {
		t.Errorf("default report --json leaked a \"connections\" key:\n%s", out)
	}
	if strings.Contains(out, `"provenance"`) {
		t.Errorf("default report --json leaked a \"provenance\" key:\n%s", out)
	}
}

// TestStatusClaudeJSONShape pins the toolStatusReport shape emitted by
// `status --tool claude --json` (glob-profile tools take a different JSON
// path than codex's fixed-file codex.LogReport — see cmdStatus).
func TestStatusClaudeJSONShape(t *testing.T) {
	home := withTempHome(t)
	writeAgedFile(t, filepath.Join(home, ".claude", "projects", "-Users-jo-app", "s1.jsonl"), 40*24*time.Hour)

	out, code := captureStdout(t, func() int { return run([]string{"status", "--tool", "claude", "--json"}) })
	if code != 0 {
		t.Fatalf("status --tool claude --json exit = %d, want 0; output:\n%s", code, out)
	}
	for _, key := range []string{`"tool"`, `"display_name"`, `"dir"`, `"cleanable"`, `"cleanable_bytes"`} {
		if !strings.Contains(out, key) {
			t.Errorf("status --tool claude --json missing key %s:\n%s", key, out)
		}
	}
	if !strings.Contains(out, `"tool": "claude"`) && !strings.Contains(out, `"tool":"claude"`) {
		t.Errorf("status --tool claude --json missing tool=claude:\n%s", out)
	}
}
