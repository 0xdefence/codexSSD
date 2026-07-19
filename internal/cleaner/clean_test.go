package cleaner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/tool"
)

// writeFile creates a file of exactly n bytes for testing.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}

func TestPlanCodexLogsEmpty(t *testing.T) {
	dir := t.TempDir() // no log files inside

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("Empty() = false, want true for %+v", plan)
	}
	if plan.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", plan.TotalBytes)
	}
}

func TestPlanCodexLogsListsPresentFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)
	writeFile(t, filepath.Join(dir, "logs_2.sqlite-wal"), 50)
	// -shm intentionally absent

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	if plan.Empty() {
		t.Fatal("Empty() = true, want false")
	}
	if len(plan.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 (%+v)", len(plan.Items), plan.Items)
	}
	if plan.TotalBytes != 150 {
		t.Errorf("TotalBytes = %d, want 150", plan.TotalBytes)
	}
	wantRoot := filepath.Join(dir, BackupDirName)
	if plan.BackupRoot != wantRoot {
		t.Errorf("BackupRoot = %q, want %q", plan.BackupRoot, wantRoot)
	}
}

// TestPlanCodexLogsJSONOmitsToolField pins the legacy (pre-multi-tool) JSON
// shape of a codex Plan: it must NOT contain a "tool" key at all — the base
// branch never emitted one — while still carrying the documented legacy keys.
// "" already means codex everywhere this field is read (profileFor); this
// test guards the write side so a default `clean --json` (and anything else
// that marshals a codex Plan, e.g. the MCP clean_plan tool and manifests
// written for codex backups) stays byte-identical to pre-branch output.
func TestPlanCodexLogsJSONOmitsToolField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logs_2.sqlite"), 100)

	plan, err := PlanCodexLogs(dir)
	if err != nil {
		t.Fatalf("PlanCodexLogs: %v", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"tool"`) {
		t.Errorf("codex plan JSON leaked a \"tool\" key: %s", data)
	}
	for _, key := range []string{`"codex_dir"`, `"backup_root"`, `"items"`, `"total_bytes"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("codex plan JSON missing legacy key %s: %s", key, data)
		}
	}
}

// TestPlanToolClaudeJSONKeepsToolField is the mirror of the above: a claude
// (non-default) Plan MUST carry "tool":"claude" in its JSON.
func TestPlanToolClaudeJSONKeepsToolField(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	transcript := filepath.Join(dir, "projects", "-Users-jo-app", "s1.jsonl")
	writeAged(t, transcript, 40*24*time.Hour, now)

	plan, err := PlanTool(tool.Claude(), dir, now, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PlanTool: %v", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"tool":"claude"`) {
		t.Errorf("claude plan JSON missing \"tool\":\"claude\": %s", data)
	}
}
