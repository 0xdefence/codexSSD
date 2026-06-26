package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/0xdefence/codexssd/internal/cleaner"
)

func TestRenderPlanEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderPlan(&buf, cleaner.Plan{CodexDir: "/x/.codex"}, false, true)
	out := buf.String()
	if !strings.Contains(out, "Nothing to move aside") {
		t.Errorf("empty plan output missing message:\n%s", out)
	}
}

func TestRenderPlanWithItemsAndSafety(t *testing.T) {
	var buf bytes.Buffer
	p := cleaner.Plan{
		CodexDir:   "/x/.codex",
		BackupRoot: "/x/.codex/codexssd-backups",
		Items: []cleaner.PlanItem{
			{Name: "logs_2.sqlite", Path: "/x/.codex/logs_2.sqlite", Size: 1024},
		},
		TotalBytes: 1024,
	}

	// Codex running -> output must warn it is not safe to clean.
	renderPlan(&buf, p, true, true)
	out := buf.String()
	if !strings.Contains(out, "logs_2.sqlite") || !strings.Contains(out, "1.0 KiB") {
		t.Errorf("plan output missing file/size:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "running") {
		t.Errorf("plan output should warn Codex is running:\n%s", out)
	}

	// Codex not running -> output must say it is safe.
	buf.Reset()
	renderPlan(&buf, p, false, true)
	if !strings.Contains(buf.String(), "--yes") {
		t.Errorf("plan output should tell the user to run --yes:\n%s", buf.String())
	}
}

func TestRenderBackupsEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderBackups(&buf, nil)
	if !strings.Contains(buf.String(), "No backups") {
		t.Errorf("empty backups output missing message:\n%s", buf.String())
	}
}

func TestRenderBackupsLists(t *testing.T) {
	var buf bytes.Buffer
	backups := []cleaner.Backup{
		{
			Dir: "/x/.codex/codexssd-backups/20260626-143000",
			Manifest: cleaner.Manifest{
				Items: []cleaner.ManifestItem{
					{Name: "logs_2.sqlite", OriginalPath: "/x/.codex/logs_2.sqlite", Size: 2048},
				},
			},
		},
	}
	renderBackups(&buf, backups)
	out := buf.String()
	if !strings.Contains(out, "20260626-143000") {
		t.Errorf("backups output missing id:\n%s", out)
	}
	if !strings.Contains(out, "2.0 KiB") {
		t.Errorf("backups output missing total size:\n%s", out)
	}
}
